package state

import (
	"context"
	"database/sql"
	"fmt"
	"math"
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
// The UNIQUE(session_id) constraint enforces SL-1 (exactly one archive per session).
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

	_, err := tx.ExecContext(ctx, `
		INSERT INTO session_archives
			(archive_id, session_id, agent_id, task_id, repo, branch, result,
			 started_at, ended_at, duration_seconds, commit_count, merge_sha,
			 task_source, archived_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		archive.ArchiveID,
		archive.SessionID,
		archive.AgentID,
		archive.TaskID,
		archive.Repo,
		archive.Branch,
		archive.Result,
		archive.StartedAt.UTC().Format(time.RFC3339),
		archive.EndedAt.UTC().Format(time.RFC3339),
		archive.DurationSeconds,
		archive.CommitCount,
		archive.MergeSHA,
		archive.TaskSource,
		archive.ArchivedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("write session archive: %w", err)
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
		WHERE session_id = ?`,
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
	if err := row.Scan(
		&a.ArchiveID, &a.SessionID, &a.AgentID, &a.TaskID, &a.Repo, &a.Branch,
		&a.Result, &startedAt, &endedAt, &a.DurationSeconds, &a.CommitCount,
		&a.MergeSHA, &a.TaskSource, &archivedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAgentSessionNotFound
		}
		return nil, fmt.Errorf("scan session archive: %w", err)
	}
	a.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	a.EndedAt, _ = time.Parse(time.RFC3339, endedAt)
	a.ArchivedAt, _ = time.Parse(time.RFC3339, archivedAt)
	return &a, nil
}

func scanSessionArchiveRow(rows *sql.Rows) (*SessionArchive, error) {
	var a SessionArchive
	var startedAt, endedAt, archivedAt string
	if err := rows.Scan(
		&a.ArchiveID, &a.SessionID, &a.AgentID, &a.TaskID, &a.Repo, &a.Branch,
		&a.Result, &startedAt, &endedAt, &a.DurationSeconds, &a.CommitCount,
		&a.MergeSHA, &a.TaskSource, &archivedAt,
	); err != nil {
		return nil, fmt.Errorf("scan session archive row: %w", err)
	}
	a.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	a.EndedAt, _ = time.Parse(time.RFC3339, endedAt)
	a.ArchivedAt, _ = time.Parse(time.RFC3339, archivedAt)
	return &a, nil
}
