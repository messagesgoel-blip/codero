package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrFeedbackCacheNotFound is returned when a feedback cache record is missing.
var ErrFeedbackCacheNotFound = errors.New("feedback cache not found")

// FeedbackCache is a row from task_feedback_cache.
type FeedbackCache struct {
	CacheID             string
	AssignmentID        string
	SessionID           string
	TaskID              string
	CISnapshot          string
	CoderabbitSnapshot  string
	HumanReviewSnapshot string
	ComplianceSnapshot  string
	ContextBlock        string
	SnapshotAt          time.Time
	CacheHash           string
	SourceStatus        string
}

// UpsertFeedbackCache inserts or updates a feedback cache row, keyed on
// assignment_id. If fc.CacheID is empty, a new UUID is generated.
// snapshot_at is always set to the current time via SQL datetime('now').
func UpsertFeedbackCache(ctx context.Context, db *DB, fc *FeedbackCache) error {
	if fc.CacheID == "" {
		fc.CacheID = uuid.NewString()
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO task_feedback_cache (
			cache_id, assignment_id, session_id, task_id,
			ci_snapshot, coderabbit_snapshot, human_review_snapshot,
			compliance_snapshot, context_block, snapshot_at,
			cache_hash, source_status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), ?, ?)
		ON CONFLICT(assignment_id) DO UPDATE SET
			session_id              = excluded.session_id,
			task_id                 = excluded.task_id,
			ci_snapshot             = excluded.ci_snapshot,
			coderabbit_snapshot     = excluded.coderabbit_snapshot,
			human_review_snapshot   = excluded.human_review_snapshot,
			compliance_snapshot     = excluded.compliance_snapshot,
			context_block           = excluded.context_block,
			snapshot_at             = datetime('now'),
			cache_hash              = excluded.cache_hash,
			source_status           = excluded.source_status`,
		fc.CacheID, fc.AssignmentID, fc.SessionID, fc.TaskID,
		nullStr(fc.CISnapshot), nullStr(fc.CoderabbitSnapshot),
		nullStr(fc.HumanReviewSnapshot), nullStr(fc.ComplianceSnapshot),
		nullStr(fc.ContextBlock),
		fc.CacheHash, fc.SourceStatus,
	)
	if err != nil {
		return fmt.Errorf("upsert feedback cache: %w", err)
	}
	return nil
}

// GetFeedbackCacheByAssignment retrieves a feedback cache by assignment_id.
func GetFeedbackCacheByAssignment(ctx context.Context, db *DB, assignmentID string) (*FeedbackCache, error) {
	const q = `
		SELECT cache_id, assignment_id, session_id, task_id,
		       ci_snapshot, coderabbit_snapshot, human_review_snapshot,
		       compliance_snapshot, context_block, snapshot_at,
		       cache_hash, source_status
		FROM task_feedback_cache
		WHERE assignment_id = ?`

	row := db.sql.QueryRowContext(ctx, q, assignmentID)
	return scanFeedbackCache(row)
}

// GetFeedbackCacheByTaskID retrieves the most recent feedback cache for a task_id.
func GetFeedbackCacheByTaskID(ctx context.Context, db *DB, taskID string) (*FeedbackCache, error) {
	const q = `
		SELECT cache_id, assignment_id, session_id, task_id,
		       ci_snapshot, coderabbit_snapshot, human_review_snapshot,
		       compliance_snapshot, context_block, snapshot_at,
		       cache_hash, source_status
		FROM task_feedback_cache
		WHERE task_id = ?
		ORDER BY snapshot_at DESC
		LIMIT 1`

	row := db.sql.QueryRowContext(ctx, q, taskID)
	return scanFeedbackCache(row)
}

// InvalidateFeedbackCache deletes a feedback cache row by assignment_id.
// It is a no-op if the row does not exist.
func InvalidateFeedbackCache(ctx context.Context, db *DB, assignmentID string) error {
	_, err := db.sql.ExecContext(ctx, `
		DELETE FROM task_feedback_cache
		WHERE assignment_id = ?`,
		assignmentID,
	)
	if err != nil {
		return fmt.Errorf("invalidate feedback cache: %w", err)
	}
	return nil
}

// InvalidateFeedbackCacheByTaskID deletes all feedback cache rows for a given
// task_id. It is a no-op if no rows exist.
func InvalidateFeedbackCacheByTaskID(ctx context.Context, db *DB, taskID string) error {
	_, err := db.sql.ExecContext(ctx,
		`DELETE FROM task_feedback_cache WHERE task_id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("invalidate feedback cache by task: %w", err)
	}
	return nil
}

func scanFeedbackCache(row *sql.Row) (*FeedbackCache, error) {
	var fc FeedbackCache
	var ciSnapshot sql.NullString
	var coderabbitSnapshot sql.NullString
	var humanReviewSnapshot sql.NullString
	var complianceSnapshot sql.NullString
	var contextBlock sql.NullString

	err := row.Scan(
		&fc.CacheID, &fc.AssignmentID, &fc.SessionID, &fc.TaskID,
		&ciSnapshot, &coderabbitSnapshot, &humanReviewSnapshot,
		&complianceSnapshot, &contextBlock, &fc.SnapshotAt,
		&fc.CacheHash, &fc.SourceStatus,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFeedbackCacheNotFound
		}
		return nil, fmt.Errorf("scan feedback cache: %w", err)
	}
	if ciSnapshot.Valid {
		fc.CISnapshot = ciSnapshot.String
	}
	if coderabbitSnapshot.Valid {
		fc.CoderabbitSnapshot = coderabbitSnapshot.String
	}
	if humanReviewSnapshot.Valid {
		fc.HumanReviewSnapshot = humanReviewSnapshot.String
	}
	if complianceSnapshot.Valid {
		fc.ComplianceSnapshot = complianceSnapshot.String
	}
	if contextBlock.Valid {
		fc.ContextBlock = contextBlock.String
	}
	return &fc, nil
}
