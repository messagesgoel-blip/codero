package dashboard

import (
	"context"
	"database/sql"
	"time"
)

// queryOverview returns today's aggregate run stats.
func queryOverview(ctx context.Context, db *sql.DB) (runsToday, passedToday int, blockedCount int, avgGateSec float64, err error) {
	// runs today + passed today
	row := db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END)
		FROM review_runs
		WHERE DATE(created_at) = DATE('now')`)
	var passed sql.NullInt64
	if err = row.Scan(&runsToday, &passed); err != nil {
		return
	}
	passedToday = int(passed.Int64)

	// blocked branches
	if err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM branch_states WHERE state = 'blocked'`).Scan(&blockedCount); err != nil {
		return
	}

	// avg gate time for completed runs today
	var avg sql.NullFloat64
	if err = db.QueryRowContext(ctx, `
		SELECT AVG(
			CAST((julianday(finished_at) - julianday(started_at)) * 86400 AS REAL)
		)
		FROM review_runs
		WHERE status = 'completed'
		  AND DATE(created_at) = DATE('now')
		  AND started_at IS NOT NULL
		  AND finished_at IS NOT NULL`).Scan(&avg); err != nil {
		return
	}
	if avg.Valid {
		avgGateSec = avg.Float64
	} else {
		avgGateSec = -1
	}
	return
}

// querySparkline7d returns the last 7 days of daily run stats.
func querySparkline7d(ctx context.Context, db *sql.DB) ([]DayStats, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			DATE(created_at)                                        AS day,
			COUNT(*)                                                AS total,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END)  AS passed
		FROM review_runs
		WHERE created_at >= DATE('now', '-6 days')
		GROUP BY DATE(created_at)
		ORDER BY day ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DayStats
	for rows.Next() {
		var d DayStats
		if err := rows.Scan(&d.Date, &d.Total, &d.Passed); err != nil {
			return nil, err
		}
		d.Failed = d.Total - d.Passed
		out = append(out, d)
	}
	return out, rows.Err()
}

// queryRepos returns the latest branch-state summary per repo.
func queryRepos(ctx context.Context, db *sql.DB) ([]RepoSummary, error) {
	// Latest branch record per repo (by updated_at).
	rows, err := db.QueryContext(ctx, `
		SELECT b.repo, b.branch, b.state, b.head_hash, b.updated_at
		FROM branch_states b
		INNER JOIN (
			SELECT repo, MAX(updated_at) AS max_upd
			FROM branch_states
			GROUP BY repo
		) latest ON b.repo = latest.repo AND b.updated_at = latest.max_upd
		ORDER BY b.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RepoSummary
	for rows.Next() {
		var s RepoSummary
		if err := rows.Scan(&s.Repo, &s.Branch, &s.State, &s.HeadHash, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Enrich with last run info and gate summary.
	for i := range out {
		enrichRepoSummary(ctx, db, &out[i])
	}
	return out, nil
}

// enrichRepoSummary adds last_run_status and gate_summary to a RepoSummary.
func enrichRepoSummary(ctx context.Context, db *sql.DB, s *RepoSummary) {
	// Last run status + time.
	row := db.QueryRowContext(ctx, `
		SELECT status, finished_at
		FROM review_runs
		WHERE repo = ?
		ORDER BY created_at DESC
		LIMIT 1`, s.Repo)
	var status string
	var finAt sql.NullTime
	if err := row.Scan(&status, &finAt); err == nil {
		s.LastRunStatus = status
		if finAt.Valid {
			t := finAt.Time
			s.LastRunAt = &t
		}
	}

	// Gate pills: aggregate provider outcomes for this repo's last few runs.
	rows, err := db.QueryContext(ctx, `
		SELECT provider, status
		FROM review_runs
		WHERE repo = ?
		ORDER BY created_at DESC
		LIMIT 6`, s.Repo)
	if err != nil {
		return
	}
	defer rows.Close()

	seen := map[string]string{}
	for rows.Next() {
		var prov, st string
		if rows.Scan(&prov, &st) == nil {
			if _, exists := seen[prov]; !exists {
				seen[prov] = statusToPillState(st)
			}
		}
	}
	for name, pillState := range seen {
		s.GateSummary = append(s.GateSummary, GatePill{Name: name, Status: pillState})
	}
}

func statusToPillState(status string) string {
	switch status {
	case "completed":
		return "pass"
	case "failed":
		return "fail"
	case "running":
		return "run"
	default:
		return "idle"
	}
}

// queryActivity returns the most recent delivery events.
func queryActivity(ctx context.Context, db *sql.DB, limit int) ([]ActivityEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, repo, branch, event_type, payload, created_at
		FROM delivery_events
		ORDER BY created_at DESC, seq DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ActivityEvent
	for rows.Next() {
		var e ActivityEvent
		if err := rows.Scan(&e.Seq, &e.Repo, &e.Branch, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// queryBlockReasons returns ranked error sources from findings.
func queryBlockReasons(ctx context.Context, db *sql.DB) ([]BlockReason, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT source, COUNT(*) AS cnt
		FROM findings
		WHERE severity = 'error'
		GROUP BY source
		ORDER BY cnt DESC
		LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BlockReason
	for rows.Next() {
		var b BlockReason
		if err := rows.Scan(&b.Source, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// queryGateHealth returns per-provider pass rates across all runs.
func queryGateHealth(ctx context.Context, db *sql.DB) ([]GateHealth, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			provider,
			COUNT(*)                                               AS total,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS passed
		FROM review_runs
		GROUP BY provider
		ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GateHealth
	for rows.Next() {
		var g GateHealth
		if err := rows.Scan(&g.Provider, &g.Total, &g.Passed); err != nil {
			return nil, err
		}
		if g.Total > 0 {
			g.PassRate = float64(g.Passed) / float64(g.Total) * 100
		} else {
			g.PassRate = -1
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// queryRuns returns the most recent review runs.
func queryRuns(ctx context.Context, db *sql.DB, limit int) ([]RunRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, repo, branch, head_hash, provider, status,
		       started_at, finished_at, error, created_at
		FROM review_runs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RunRow
	for rows.Next() {
		var r RunRow
		var startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Repo, &r.Branch, &r.HeadHash,
			&r.Provider, &r.Status, &startedAt, &finishedAt,
			&r.Error, &r.CreatedAt); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			t := startedAt.Time
			r.StartedAt = &t
		}
		if finishedAt.Valid {
			t := finishedAt.Time
			r.FinishedAt = &t
		}
		// Manual runs are identified by provider "manual".
		r.Manual = r.Provider == "manual"
		out = append(out, r)
	}
	return out, rows.Err()
}

// queryLatestActivitySeq returns the highest delivery_events seq across all repos.
// Returns 0 if the table is empty.
func queryLatestActivitySeq(ctx context.Context, db *sql.DB) (int64, error) {
	var seq sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT MAX(seq) FROM delivery_events`).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if seq.Valid {
		return seq.Int64, nil
	}
	return 0, nil
}

// queryActivitySince returns delivery_events newer than sinceSeq.
func queryActivitySince(ctx context.Context, db *sql.DB, sinceSeq int64, limit int) ([]ActivityEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, repo, branch, event_type, payload, created_at
		FROM delivery_events
		WHERE seq > ?
		ORDER BY seq ASC
		LIMIT ?`, sinceSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ActivityEvent
	for rows.Next() {
		var e ActivityEvent
		if err := rows.Scan(&e.Seq, &e.Repo, &e.Branch, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// insertManualReviewRun creates a pending manual review run and returns its ID.
func insertManualReviewRun(ctx context.Context, db *sql.DB, id, repo, branch, headHash string) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
		INSERT INTO review_runs (id, repo, branch, head_hash, provider, status, started_at, error, created_at)
		VALUES (?, ?, ?, ?, 'manual', 'pending', ?, '', ?)`,
		id, repo, branch, headHash, now, now)
	return err
}
