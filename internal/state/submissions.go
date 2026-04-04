package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrDuplicateSubmission is returned when a duplicate (assignment_id, diff_hash, head_sha) is detected.
var ErrDuplicateSubmission = errors.New("duplicate submission: same diff already submitted for this assignment")

// SubmissionRecord is a row from the submissions table.
type SubmissionRecord struct {
	SubmissionID  string
	AssignmentID  string
	SessionID     string
	Repo          string
	Branch        string
	HeadSHA       string
	DiffHash      string
	AttemptLocal  int
	AttemptRemote int
	State         string
	Result        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateSubmission inserts a new submission record.
// Returns ErrDuplicateSubmission if same (assignment_id, diff_hash, head_sha) already exists
// and assignment_id is non-empty.
func CreateSubmission(ctx context.Context, db *DB, rec SubmissionRecord) error {
	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO submissions (
			submission_id, assignment_id, session_id, repo, branch,
			head_sha, diff_hash, attempt_local, attempt_remote, state, result
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rec.SubmissionID, rec.AssignmentID, rec.SessionID, rec.Repo, rec.Branch,
		rec.HeadSHA, rec.DiffHash, rec.AttemptLocal, rec.AttemptRemote, rec.State, rec.Result,
	)
	if err != nil {
		// Map only the dedup-key constraint violation to ErrDuplicateSubmission.
		// Primary key violations (submission_id) are a different UNIQUE failure.
		if strings.Contains(err.Error(), "UNIQUE constraint failed: submissions.assignment_id") {
			return ErrDuplicateSubmission
		}
		return fmt.Errorf("insert submission: %w", err)
	}
	return nil
}

// GetSubmissionsByBranch returns all submissions for a repo/branch, newest first.
func GetSubmissionsByBranch(ctx context.Context, db *DB, repo, branch string) ([]SubmissionRecord, error) {
	rows, err := db.Unwrap().QueryContext(ctx, `
		SELECT submission_id, assignment_id, session_id, repo, branch,
		       head_sha, diff_hash, attempt_local, attempt_remote, state, result,
		       created_at, updated_at
		FROM submissions
		WHERE repo = ? AND branch = ?
		ORDER BY created_at DESC
	`, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("query submissions by branch: %w", err)
	}
	defer rows.Close()

	var out []SubmissionRecord
	for rows.Next() {
		var rec SubmissionRecord
		if err := rows.Scan(
			&rec.SubmissionID, &rec.AssignmentID, &rec.SessionID, &rec.Repo, &rec.Branch,
			&rec.HeadSHA, &rec.DiffHash, &rec.AttemptLocal, &rec.AttemptRemote, &rec.State, &rec.Result,
			&rec.CreatedAt, &rec.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan submission row: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate submissions: %w", err)
	}
	return out, nil
}

// GetSubmissionByID retrieves a single submission by its ID.
func GetSubmissionByID(ctx context.Context, db *DB, submissionID string) (*SubmissionRecord, error) {
	row := db.Unwrap().QueryRowContext(ctx, `
		SELECT submission_id, assignment_id, session_id, repo, branch,
		       head_sha, diff_hash, attempt_local, attempt_remote, state, result,
		       created_at, updated_at
		FROM submissions
		WHERE submission_id = ?
	`, submissionID)

	var rec SubmissionRecord
	if err := row.Scan(
		&rec.SubmissionID, &rec.AssignmentID, &rec.SessionID, &rec.Repo, &rec.Branch,
		&rec.HeadSHA, &rec.DiffHash, &rec.AttemptLocal, &rec.AttemptRemote, &rec.State, &rec.Result,
		&rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("GetSubmissionByID: scan submission: %w", err)
	}
	return &rec, nil
}

// UpdateSubmissionState updates the state and result fields.
// Returns an error if the submission is not found.
func UpdateSubmissionState(ctx context.Context, db *DB, submissionID, state, result string) error {
	res, err := db.Unwrap().ExecContext(ctx, `
		UPDATE submissions
		SET state = ?, result = ?, updated_at = datetime('now')
		WHERE submission_id = ?
	`, state, result, submissionID)
	if err != nil {
		return fmt.Errorf("update submission state: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("submission %s not found", submissionID)
	}
	return nil
}

// SubmissionCountForBranch returns the number of submissions for a repo/branch.
func SubmissionCountForBranch(ctx context.Context, db *DB, repo, branch string) (int, error) {
	var count int
	if err := db.Unwrap().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM submissions WHERE repo = ? AND branch = ?
	`, repo, branch).Scan(&count); err != nil {
		return 0, fmt.Errorf("submission count for repo %s branch %s: %w", repo, branch, err)
	}
	return count, nil
}

// LatestSubmissionForBranch returns the most recent submission_id for a repo/branch, or "" if none.
func LatestSubmissionForBranch(ctx context.Context, db *DB, repo, branch string) (string, error) {
	var submissionID sql.NullString
	err := db.Unwrap().QueryRowContext(ctx, `
		SELECT submission_id FROM submissions
		WHERE repo = ? AND branch = ?
		ORDER BY created_at DESC, submission_id DESC LIMIT 1
	`, repo, branch).Scan(&submissionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("latest submission for repo %s branch %s: %w", repo, branch, err)
	}
	if submissionID.Valid {
		return submissionID.String, nil
	}
	return "", nil
}
