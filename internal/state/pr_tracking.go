package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// UpsertPRTracking ensures a branch_states row exists for repo/branch and sets
// the pr_number. If the row already exists the PR number is updated in place.
// This makes the operation idempotent — calling twice with the same PR produces
// one row; calling with a different PR updates the existing row.
func UpsertPRTracking(ctx context.Context, db *DB, repo, branch string, prNumber int) error {
	if repo == "" || branch == "" {
		return fmt.Errorf("pr tracking: repo and branch are required")
	}
	if prNumber <= 0 {
		return fmt.Errorf("pr tracking: pr number must be positive, got %d", prNumber)
	}

	// Try UPDATE first — this is the common path when the branch already exists
	// (e.g. from a session heartbeat or a prior submit).
	res, err := db.sql.ExecContext(ctx,
		`UPDATE branch_states SET pr_number = ?, updated_at = datetime('now') WHERE repo = ? AND branch = ?`,
		prNumber, repo, branch,
	)
	if err != nil {
		return fmt.Errorf("pr tracking: update: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("pr tracking: rows affected: %w", err)
	}
	if affected > 0 {
		return nil
	}

	// No existing row — insert a new branch_states entry in the "active" state
	// so the pipeline and repos endpoints can surface this branch immediately.
	id := uuid.New().String()
	_, err = db.sql.ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, pr_number, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, datetime('now'), datetime('now'))`,
		id, repo, branch, prNumber,
	)
	if err != nil {
		return fmt.Errorf("pr tracking: insert: %w", err)
	}
	return nil
}

// GetPRNumber returns the PR number for a repo/branch pair, or 0 if not tracked.
func GetPRNumber(ctx context.Context, db *DB, repo, branch string) (int, error) {
	var prNumber int
	err := db.sql.QueryRowContext(ctx,
		`SELECT pr_number FROM branch_states WHERE repo = ? AND branch = ?`,
		repo, branch,
	).Scan(&prNumber)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get pr number: %w", err)
	}
	return prNumber, nil
}
