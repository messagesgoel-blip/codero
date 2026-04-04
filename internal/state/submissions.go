package state

import (
	"context"
	"database/sql"
	"errors"
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
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrDuplicateSubmission
		}
		return err
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
		return nil, err
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
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
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
		return nil, err
	}
	return &rec, nil
}

// UpdateSubmissionState updates the state and result fields.
func UpdateSubmissionState(ctx context.Context, db *DB, submissionID, state, result string) error {
	_, err := db.Unwrap().ExecContext(ctx, `
		UPDATE submissions
		SET state = ?, result = ?, updated_at = datetime('now')
		WHERE submission_id = ?
	`, state, result, submissionID)
	return err
}

// SubmissionCountForBranch returns the number of submissions for a repo/branch.
func SubmissionCountForBranch(ctx context.Context, db *DB, repo, branch string) (int, error) {
	var count int
	err := db.Unwrap().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM submissions WHERE repo = ? AND branch = ?
	`, repo, branch).Scan(&count)
	return count, err
}

// LatestSubmissionForBranch returns the most recent submission_id for a repo/branch, or "" if none.
func LatestSubmissionForBranch(ctx context.Context, db *DB, repo, branch string) (string, error) {
	var submissionID sql.NullString
	err := db.Unwrap().QueryRowContext(ctx, `
		SELECT submission_id FROM submissions
		WHERE repo = ? AND branch = ?
		ORDER BY created_at DESC LIMIT 1
	`, repo, branch).Scan(&submissionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if submissionID.Valid {
		return submissionID.String, nil
	}
	return "", nil
}
