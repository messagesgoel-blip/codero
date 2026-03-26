package state

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SessionArchive is a row from session_archives.
// Spec reference: Session Lifecycle v1 §4.1.
type SessionArchive struct {
	ArchiveID       string
	SessionID       string
	AgentID         string
	TaskID          string
	Repo            string
	Branch          string
	Result          string
	StartedAt       time.Time
	EndedAt         time.Time
	DurationSeconds int
	CommitCount     int
	MergeSHA        string
	TaskSource      string
	ArchivedAt      time.Time
}

// writeSessionArchiveTx writes one session_archives row inside an existing transaction.
// It is called atomically within the terminal-state transition (SL-3).
func writeSessionArchiveTx(ctx context.Context, tx *sql.Tx, archive *SessionArchive) error {
	if archive.ArchiveID == "" {
		archive.ArchiveID = uuid.New().String()
	}
	if archive.ArchivedAt.IsZero() {
		archive.ArchivedAt = time.Now().UTC()
	}

	duration := int(math.Round(archive.EndedAt.Sub(archive.StartedAt).Seconds()))
	if duration < 0 {
		duration = 0
	}
	archive.DurationSeconds = duration

	taskID := nullIfEmpty(archive.TaskID)
	repo := nullIfEmpty(archive.Repo)
	branch := nullIfEmpty(archive.Branch)
	mergeSHA := nullIfEmpty(archive.MergeSHA)
	taskSource := nullIfEmpty(archive.TaskSource)

	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_archives
			(archive_id, session_id, agent_id, task_id, repo, branch, result,
			 started_at, ended_at, duration_seconds, commit_count, merge_sha,
			 task_source, archived_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		archive.ArchiveID,
		archive.SessionID,
		archive.AgentID,
		taskID,
		repo,
		branch,
		archive.Result,
		archive.StartedAt.UTC().Format(time.RFC3339),
		archive.EndedAt.UTC().Format(time.RFC3339),
		archive.DurationSeconds,
		archive.CommitCount,
		mergeSHA,
		taskSource,
		archive.ArchivedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("write session archive: %w", err)
	}
	return nil
}

// ArchiveSession writes an archive row for the given session.
// Intended for use by external orchestrators (e.g., merge completion).
func ArchiveSession(ctx context.Context, db *DB, sessionID, result, mergeSHA string) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("archive session: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := archiveSessionTx(ctx, tx, sessionID, result, mergeSHA, time.Now().UTC()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("archive session: commit: %w", err)
	}
	return nil
}

func archiveSessionTx(ctx context.Context, tx *sql.Tx, sessionID, result, mergeSHA string, now time.Time) error {
	var session AgentSession
	var endedAt sql.NullTime

	if err := tx.QueryRowContext(ctx, `
		SELECT session_id, agent_id, started_at, ended_at, end_reason
		FROM agent_sessions
		WHERE session_id = ?`,
		sessionID,
	).Scan(&session.SessionID, &session.AgentID, &session.StartedAt, &endedAt, &session.EndReason); err != nil {
		if err == sql.ErrNoRows {
			return ErrAgentSessionNotFound
		}
		return fmt.Errorf("archive session: load session: %w", err)
	}

	endTime := now
	if endedAt.Valid {
		endTime = endedAt.Time
	}
	finalResult := result
	if strings.TrimSpace(finalResult) == "" {
		finalResult = session.EndReason
	}
	if strings.TrimSpace(finalResult) == "" {
		finalResult = "ended"
	}

	var assignment AgentAssignment
	assignErr := tx.QueryRowContext(ctx, `
		SELECT assignment_id, session_id, agent_id, repo, branch, task_id
		FROM agent_assignments
		WHERE session_id = ?
		ORDER BY started_at DESC
		LIMIT 1`,
		sessionID,
	).Scan(&assignment.ID, &assignment.SessionID, &assignment.AgentID, &assignment.Repo, &assignment.Branch, &assignment.TaskID)
	if assignErr != nil && assignErr != sql.ErrNoRows {
		return fmt.Errorf("archive session: load assignment: %w", assignErr)
	}

	archive := &SessionArchive{
		SessionID: sessionID,
		AgentID:   session.AgentID,
		Result:    finalResult,
		StartedAt: session.StartedAt,
		EndedAt:   endTime,
		MergeSHA:  mergeSHA,
	}
	if assignErr == nil {
		archive.TaskID = assignment.TaskID
		archive.Repo = assignment.Repo
		archive.Branch = assignment.Branch
	}

	if err := writeSessionArchiveTx(ctx, tx, archive); err != nil {
		return fmt.Errorf("archive session: write archive: %w", err)
	}
	return nil
}

// GetSessionArchive returns the archive row for a session, if one exists.
func GetSessionArchive(ctx context.Context, db *DB, sessionID string) (*SessionArchive, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT archive_id, session_id, agent_id, task_id, repo, branch, result,
		       started_at, ended_at, duration_seconds, commit_count, merge_sha,
		       task_source, archived_at
		FROM session_archives
		WHERE session_id = ?
		ORDER BY archived_at DESC
		LIMIT 1`,
		sessionID,
	)
	return scanSessionArchive(row)
}

// ListSessionArchives returns all archive rows ordered by archived_at DESC.
func ListSessionArchives(ctx context.Context, db *DB, limit int) ([]SessionArchive, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.sql.QueryContext(ctx, `
		SELECT archive_id, session_id, agent_id, task_id, repo, branch, result,
		       started_at, ended_at, duration_seconds, commit_count, merge_sha,
		       task_source, archived_at
		FROM session_archives
		ORDER BY archived_at DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list session archives: %w", err)
	}
	defer rows.Close()

	var archives []SessionArchive
	for rows.Next() {
		a, err := scanSessionArchiveRow(rows)
		if err != nil {
			return nil, err
		}
		archives = append(archives, *a)
	}
	return archives, rows.Err()
}

func scanSessionArchive(row *sql.Row) (*SessionArchive, error) {
	var a SessionArchive
	var startedAt, endedAt, archivedAt string
	var taskID, repo, branch, mergeSHA, taskSource sql.NullString
	if err := row.Scan(
		&a.ArchiveID, &a.SessionID, &a.AgentID, &taskID, &repo, &branch,
		&a.Result, &startedAt, &endedAt, &a.DurationSeconds, &a.CommitCount,
		&mergeSHA, &taskSource, &archivedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAgentSessionNotFound
		}
		return nil, fmt.Errorf("scan session archive: %w", err)
	}
	a.TaskID = taskID.String
	a.Repo = repo.String
	a.Branch = branch.String
	a.MergeSHA = mergeSHA.String
	a.TaskSource = taskSource.String
	a.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	a.EndedAt, _ = time.Parse(time.RFC3339, endedAt)
	a.ArchivedAt, _ = time.Parse(time.RFC3339, archivedAt)
	return &a, nil
}

func scanSessionArchiveRow(rows *sql.Rows) (*SessionArchive, error) {
	var a SessionArchive
	var startedAt, endedAt, archivedAt string
	var taskID, repo, branch, mergeSHA, taskSource sql.NullString
	if err := rows.Scan(
		&a.ArchiveID, &a.SessionID, &a.AgentID, &taskID, &repo, &branch,
		&a.Result, &startedAt, &endedAt, &a.DurationSeconds, &a.CommitCount,
		&mergeSHA, &taskSource, &archivedAt,
	); err != nil {
		return nil, fmt.Errorf("scan session archive row: %w", err)
	}
	a.TaskID = taskID.String
	a.Repo = repo.String
	a.Branch = branch.String
	a.MergeSHA = mergeSHA.String
	a.TaskSource = taskSource.String
	a.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	a.EndedAt, _ = time.Parse(time.RFC3339, endedAt)
	a.ArchivedAt, _ = time.Parse(time.RFC3339, archivedAt)
	return &a, nil
}

func nullIfEmpty(value string) sql.NullString {
	if strings.TrimSpace(value) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
