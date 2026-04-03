package state

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RecordPrecommitResult inserts a precommit gate run into both the precommit_reviews
// and review_runs tables so the scorecard and gate health dashboard both reflect real data.
//
// result must be "pass" or "fail". durationMS is the wall-clock run duration in milliseconds.
// checks is a comma-separated list of checks that ran (e.g. "gitleaks,ruff,govet").
// headHash is the current git HEAD SHA; empty string is accepted.
//
// Both inserts happen in a single transaction to avoid partial writes.
// The call is best-effort — callers should not rely on the returned error to block commits.
func RecordPrecommitResult(ctx context.Context, db *DB, repo, branch, headHash, result string, durationMS int64, checks string) error {
	now := time.Now().UTC()
	id := uuid.New().String()

	// Map result to status strings for each table.
	var precommitStatus string // precommit_reviews: "passed" | "failed"
	var runStatus string       // review_runs: "completed" | "failed"
	switch result {
	case "pass":
		precommitStatus = "passed"
		runStatus = "completed"
	case "fail":
		precommitStatus = "failed"
		runStatus = "failed"
	default:
		return fmt.Errorf("record precommit: invalid result %q (expected pass or fail)", result)
	}

	// Populate error field: on pass store the checks list as metadata;
	// on fail prefix with "failed checks:" for visibility.
	errMsg := ""
	if result == "fail" && checks != "" {
		errMsg = "failed checks: " + checks
	} else if result == "fail" {
		errMsg = "gate failed"
	} else if checks != "" {
		errMsg = checks
	}

	// Compute started_at from duration if provided.
	startedAt := now
	if durationMS > 0 {
		startedAt = now.Add(-time.Duration(durationMS) * time.Millisecond)
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record precommit: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	// Write to precommit_reviews (feeds proving scorecard PrecommitReviews7Days).
	_, err = tx.ExecContext(ctx, `
		INSERT INTO precommit_reviews (id, repo, branch, provider, status, error, created_at)
		VALUES (?, ?, ?, 'precommit', ?, ?, ?)`,
		id, repo, branch, precommitStatus, errMsg, now)
	if err != nil {
		return fmt.Errorf("record precommit: insert precommit_reviews: %w", err)
	}

	// Write to review_runs (feeds queryGateHealth provider breakdown).
	_, err = tx.ExecContext(ctx, `
		INSERT INTO review_runs (id, repo, branch, head_hash, provider, status, started_at, finished_at, error, created_at)
		VALUES (?, ?, ?, ?, 'precommit', ?, ?, ?, ?, ?)`,
		uuid.New().String(), repo, branch, headHash, runStatus, startedAt, now, errMsg, now)
	if err != nil {
		return fmt.Errorf("record precommit: insert review_runs: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record precommit: commit: %w", err)
	}
	return nil
}
