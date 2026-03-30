// Package sessmetrics polls LiteLLM's Postgres spend log and imports per-session
// token usage into codero's local SQLite, then evaluates context pressure for
// every active session.
package sessmetrics

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq" // register the postgres driver

	"github.com/codero/codero/internal/state"
)

// LiteLLMSyncer polls LiteLLM's Postgres spend log and writes token metric rows
// into codero's local SQLite. It uses the MAX(request_time) in codero as a
// cursor so each sync only fetches new rows.
type LiteLLMSyncer struct {
	pgDSN string
	db    *state.DB
}

// NewLiteLLMSyncer returns a syncer backed by the given Postgres DSN.
func NewLiteLLMSyncer(pgDSN string, db *state.DB) *LiteLLMSyncer {
	return &LiteLLMSyncer{pgDSN: pgDSN, db: db}
}

// Sync fetches new rows from LiteLLM_SpendLogs and upserts them into
// session_token_metrics. Returns the number of rows imported.
func (s *LiteLLMSyncer) Sync(ctx context.Context) (int, error) {
	cursor, err := state.GetLatestSyncedRequestTime(ctx, s.db)
	if err != nil {
		return 0, fmt.Errorf("litellm sync: get cursor: %w", err)
	}

	pg, err := sql.Open("postgres", s.pgDSN)
	if err != nil {
		return 0, fmt.Errorf("litellm sync: open pg: %w", err)
	}
	defer pg.Close()

	// Fetch rows newer than the cursor. We filter to rows that have a
	// session_id so we can correlate with codero sessions.
	rows, err := pg.QueryContext(ctx, `
		SELECT request_id, session_id, model, prompt_tokens, completion_tokens, "startTime"
		FROM "LiteLLM_SpendLogs"
		WHERE session_id IS NOT NULL
		  AND session_id != ''
		  AND "startTime" > $1
		ORDER BY "startTime" ASC
		LIMIT 2000`,
		cursor,
	)
	if err != nil {
		return 0, fmt.Errorf("litellm sync: query: %w", err)
	}
	defer rows.Close()

	type rawRow struct {
		requestID    string
		sessionID    string
		model        string
		promptTokens int64
		compTokens   int64
		startTime    time.Time
	}

	var batch []rawRow
	for rows.Next() {
		var r rawRow
		if err := rows.Scan(&r.requestID, &r.sessionID, &r.model,
			&r.promptTokens, &r.compTokens, &r.startTime); err != nil {
			return 0, fmt.Errorf("litellm sync: scan: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("litellm sync: rows: %w", err)
	}

	sqlDB := s.db.Unwrap()

	// Accumulate running totals per LiteLLM session_id.
	type sessionAcc struct {
		cumPrompt int64
		cumComp   int64
	}
	acc := map[string]*sessionAcc{}
	for _, r := range batch {
		if _, ok := acc[r.sessionID]; !ok {
			acc[r.sessionID] = &sessionAcc{}
		}
	}
	// Seed accumulators from existing maximums so mid-session syncs stay correct.
	for sid := range acc {
		var maxCumPrompt, maxCumComp sql.NullInt64
		_ = sqlDB.QueryRowContext(ctx, `
			SELECT MAX(cumulative_prompt_tokens), MAX(cumulative_completion_tokens)
			FROM session_token_metrics
			WHERE session_id IN (
				SELECT session_id FROM agent_sessions WHERE litellm_session_id = ?
			)`, sid).Scan(&maxCumPrompt, &maxCumComp)
		if maxCumPrompt.Valid {
			acc[sid].cumPrompt = maxCumPrompt.Int64
		}
		if maxCumComp.Valid {
			acc[sid].cumComp = maxCumComp.Int64
		}
	}

	imported := 0
	for _, r := range batch {
		// Resolve the codero session_id from litellm_session_id.
		var coderoSessionID sql.NullString
		_ = sqlDB.QueryRowContext(ctx,
			`SELECT session_id FROM agent_sessions WHERE litellm_session_id = ? LIMIT 1`,
			r.sessionID).Scan(&coderoSessionID)
		if !coderoSessionID.Valid {
			// LiteLLM session not linked to any codero session; skip.
			continue
		}

		a := acc[r.sessionID]
		a.cumPrompt += r.promptTokens
		a.cumComp += r.compTokens

		reqID := r.requestID
		row := state.TokenMetricRow{
			SessionID:                  coderoSessionID.String,
			LiteLLMRequestID:           &reqID,
			Model:                      r.model,
			PromptTokens:               r.promptTokens,
			CompletionTokens:           r.compTokens,
			CumulativePromptTokens:     a.cumPrompt,
			CumulativeCompletionTokens: a.cumComp,
			RequestTime:                r.startTime,
		}
		if err := state.UpsertTokenMetric(ctx, s.db, row); err != nil {
			return imported, fmt.Errorf("upsert token metric (session=%s request=%s): %w",
				coderoSessionID.String, r.requestID, err)
		}
		imported++
	}
	return imported, nil
}
