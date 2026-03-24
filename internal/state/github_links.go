package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrGitHubLinkNotFound is returned when a github link record is missing.
var ErrGitHubLinkNotFound = errors.New("github link not found")

// GitHubLink is a row from codero_github_links.
type GitHubLink struct {
	LinkID       string
	TaskID       string
	RepoFullName string
	PRNumber     int
	IssueNumber  int
	BranchName   string
	HeadSHA      string
	PRState      string
	LastCIRunID  string
	LastSyncedAt *time.Time
}

// UpsertGitHubLink inserts or updates a github link row, keyed on task_id.
// If link.LinkID is empty, a new UUID is generated. LastSyncedAt is always
// set to the current time.
func UpsertGitHubLink(ctx context.Context, db *DB, link *GitHubLink) error {
	if link.LinkID == "" {
		link.LinkID = uuid.NewString()
	}
	now := time.Now().UTC().Truncate(time.Second)
	link.LastSyncedAt = &now

	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO codero_github_links (
			link_id, task_id, repo_full_name, pr_number, issue_number,
			branch_name, head_sha, pr_state, last_ci_run_id, last_synced_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			repo_full_name = excluded.repo_full_name,
			pr_number      = excluded.pr_number,
			issue_number   = excluded.issue_number,
			branch_name    = excluded.branch_name,
			head_sha       = excluded.head_sha,
			pr_state       = excluded.pr_state,
			last_ci_run_id = excluded.last_ci_run_id,
			last_synced_at = excluded.last_synced_at`,
		link.LinkID, link.TaskID, link.RepoFullName,
		nullInt(link.PRNumber), nullInt(link.IssueNumber),
		nullStr(link.BranchName), nullStr(link.HeadSHA),
		nullStr(link.PRState), nullStr(link.LastCIRunID),
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert github link: %w", err)
	}
	return nil
}

// GetLinkByTaskID retrieves a github link by task_id.
func GetLinkByTaskID(ctx context.Context, db *DB, taskID string) (*GitHubLink, error) {
	const q = `
		SELECT link_id, task_id, repo_full_name, pr_number, issue_number,
		       branch_name, head_sha, pr_state, last_ci_run_id, last_synced_at
		FROM codero_github_links
		WHERE task_id = ?`

	row := db.sql.QueryRowContext(ctx, q, taskID)
	return scanGitHubLink(row)
}

// GetLinkByRepoPR retrieves a github link by repo_full_name and pr_number.
func GetLinkByRepoPR(ctx context.Context, db *DB, repoFullName string, prNumber int) (*GitHubLink, error) {
	const q = `
		SELECT link_id, task_id, repo_full_name, pr_number, issue_number,
		       branch_name, head_sha, pr_state, last_ci_run_id, last_synced_at
		FROM codero_github_links
		WHERE repo_full_name = ? AND pr_number = ?`

	row := db.sql.QueryRowContext(ctx, q, repoFullName, prNumber)
	return scanGitHubLink(row)
}

// GetLinkByBranch retrieves a github link by repo_full_name and branch_name.
func GetLinkByBranch(ctx context.Context, db *DB, repoFullName, branchName string) (*GitHubLink, error) {
	const q = `
		SELECT link_id, task_id, repo_full_name, pr_number, issue_number,
		       branch_name, head_sha, pr_state, last_ci_run_id, last_synced_at
		FROM codero_github_links
		WHERE repo_full_name = ? AND branch_name = ?`

	row := db.sql.QueryRowContext(ctx, q, repoFullName, branchName)
	return scanGitHubLink(row)
}

// UpdateLinkPRState updates the pr_state and last_synced_at for a link.
func UpdateLinkPRState(ctx context.Context, db *DB, linkID, prState string) error {
	now := time.Now().UTC().Truncate(time.Second)
	res, err := db.sql.ExecContext(ctx, `
		UPDATE codero_github_links
		SET pr_state = ?, last_synced_at = ?
		WHERE link_id = ?`,
		prState, now, linkID,
	)
	if err != nil {
		return fmt.Errorf("update link pr state: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update link pr state rows affected: %w", err)
	}
	if affected == 0 {
		return ErrGitHubLinkNotFound
	}
	return nil
}

// UpdateLinkHeadSHA updates the head_sha and last_synced_at for a link.
func UpdateLinkHeadSHA(ctx context.Context, db *DB, linkID, headSHA string) error {
	now := time.Now().UTC().Truncate(time.Second)
	res, err := db.sql.ExecContext(ctx, `
		UPDATE codero_github_links
		SET head_sha = ?, last_synced_at = ?
		WHERE link_id = ?`,
		headSHA, now, linkID,
	)
	if err != nil {
		return fmt.Errorf("update link head sha: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update link head sha rows affected: %w", err)
	}
	if affected == 0 {
		return ErrGitHubLinkNotFound
	}
	return nil
}

// UpdateLinkCIRunID updates the last_ci_run_id and last_synced_at for a link.
func UpdateLinkCIRunID(ctx context.Context, db *DB, linkID, ciRunID string) error {
	now := time.Now().UTC().Truncate(time.Second)
	res, err := db.sql.ExecContext(ctx, `
		UPDATE codero_github_links
		SET last_ci_run_id = ?, last_synced_at = ?
		WHERE link_id = ?`,
		ciRunID, now, linkID,
	)
	if err != nil {
		return fmt.Errorf("update link ci run id: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update link ci run id rows affected: %w", err)
	}
	if affected == 0 {
		return ErrGitHubLinkNotFound
	}
	return nil
}

func scanGitHubLink(row *sql.Row) (*GitHubLink, error) {
	var link GitHubLink
	var prNumber sql.NullInt64
	var issueNumber sql.NullInt64
	var branchName sql.NullString
	var headSHA sql.NullString
	var prState sql.NullString
	var lastCIRunID sql.NullString
	var lastSyncedAt sql.NullTime

	err := row.Scan(
		&link.LinkID, &link.TaskID, &link.RepoFullName,
		&prNumber, &issueNumber,
		&branchName, &headSHA, &prState, &lastCIRunID, &lastSyncedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGitHubLinkNotFound
		}
		return nil, fmt.Errorf("scan github link: %w", err)
	}
	if prNumber.Valid {
		link.PRNumber = int(prNumber.Int64)
	}
	if issueNumber.Valid {
		link.IssueNumber = int(issueNumber.Int64)
	}
	if branchName.Valid {
		link.BranchName = branchName.String
	}
	if headSHA.Valid {
		link.HeadSHA = headSHA.String
	}
	if prState.Valid {
		link.PRState = prState.String
	}
	if lastCIRunID.Valid {
		link.LastCIRunID = lastCIRunID.String
	}
	if lastSyncedAt.Valid {
		t := lastSyncedAt.Time
		link.LastSyncedAt = &t
	}
	return &link, nil
}

// nullInt returns a sql.NullInt64 that is NULL when v == 0.
func nullInt(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(v), Valid: true}
}

// nullStr returns a sql.NullString that is NULL when v == "".
func nullStr(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
