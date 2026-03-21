package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrAgentSessionNotFound is returned when an agent session record is missing.
var ErrAgentSessionNotFound = errors.New("agent session not found")

// ErrAgentSessionAlreadyEnded is returned when a single-use session ID is
// re-registered after its historical row already ended.
var ErrAgentSessionAlreadyEnded = errors.New("agent session already ended")
var ErrAgentSessionAgentMismatch = errors.New("agent session agent mismatch")

// ErrAgentAssignmentNotFound is returned when an agent assignment record is missing.
var ErrAgentAssignmentNotFound = errors.New("agent assignment not found")

// AgentSession is a row from agent_sessions.
type AgentSession struct {
	SessionID      string
	AgentID        string
	Mode           string
	StartedAt      time.Time
	LastSeenAt     time.Time
	LastProgressAt *time.Time
	EndedAt        *time.Time
	EndReason      string
}

// AgentAssignment is a row from agent_assignments.
type AgentAssignment struct {
	ID           string
	SessionID    string
	AgentID      string
	Repo         string
	Branch       string
	Worktree     string
	TaskID       string
	StartedAt    time.Time
	EndedAt      *time.Time
	EndReason    string
	SupersededBy *string
}

// AgentEvent is a row from agent_events.
type AgentEvent struct {
	ID        int64
	SessionID string
	AgentID   string
	EventType string
	Payload   string
	CreatedAt time.Time
}

// RegisterAgentSession inserts or updates a session registration.
// Registration starts with session_id + agent_id; mode is optional and
// only updates when provided.
func RegisterAgentSession(ctx context.Context, db *DB, sessionID, agentID, mode string) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("register agent session: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var endedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT ended_at
		FROM agent_sessions
		WHERE session_id = ?`,
		sessionID,
	).Scan(&endedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("register agent session: check existing row: %w", err)
	}
	if err == nil && endedAt.Valid && isSingleUseSessionID(sessionID) {
		return ErrAgentSessionAlreadyEnded
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_sessions (session_id, agent_id, mode, started_at, last_seen_at)
		VALUES (?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(session_id) DO UPDATE SET
			agent_id = excluded.agent_id,
			mode = COALESCE(NULLIF(excluded.mode, ''), agent_sessions.mode),
			ended_at = NULL,
			end_reason = '',
			last_seen_at = datetime('now')`,
		sessionID, agentID, mode,
	)
	if err != nil {
		return fmt.Errorf("register agent session: %w", err)
	}
	payload, err := json.Marshal(map[string]string{
		"mode": mode,
	})
	if err != nil {
		return fmt.Errorf("register agent session: marshal event payload: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		VALUES (?, ?, ?, ?)`,
		sessionID, agentID, "session_registered", string(payload),
	); err != nil {
		return fmt.Errorf("register agent session: append event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("register agent session: commit: %w", err)
	}
	return nil
}

func isSingleUseSessionID(sessionID string) bool {
	_, err := uuid.Parse(sessionID)
	return err == nil
}

// UpdateAgentSessionHeartbeat updates the last_seen_at timestamp for a session.
// When markProgress is true, it also refreshes last_progress_at.
func UpdateAgentSessionHeartbeat(ctx context.Context, db *DB, sessionID string, markProgress bool) error {
	query := `
		UPDATE agent_sessions
		SET last_seen_at = datetime('now')`
	if markProgress {
		query += `,
			last_progress_at = datetime('now')`
	}
	query += `
		WHERE session_id = ? AND ended_at IS NULL`

	res, err := db.sql.ExecContext(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("update agent session heartbeat: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update agent session heartbeat rows affected: %w", err)
	}
	if affected == 0 {
		return ErrAgentSessionNotFound
	}
	return nil
}

// GetAgentSession retrieves a session by ID.
func GetAgentSession(ctx context.Context, db *DB, sessionID string) (*AgentSession, error) {
	const q = `
		SELECT session_id, agent_id, mode, started_at, last_seen_at, last_progress_at, ended_at, end_reason
		FROM agent_sessions
		WHERE session_id = ?`

	row := db.sql.QueryRowContext(ctx, q, sessionID)
	s, err := scanAgentSession(row)
	if err != nil && !errors.Is(err, ErrAgentSessionNotFound) {
		return nil, fmt.Errorf("get agent session %q: %w", sessionID, err)
	}
	return s, err
}

// ConfirmAgentSession verifies that a live session exists and belongs to the
// expected agent ID.
func ConfirmAgentSession(ctx context.Context, db *DB, sessionID, agentID string) error {
	s, err := GetAgentSession(ctx, db, sessionID)
	if err != nil {
		return err
	}
	if s.EndedAt != nil {
		return ErrAgentSessionAlreadyEnded
	}
	if agentID != "" && s.AgentID != agentID {
		return ErrAgentSessionAgentMismatch
	}
	return nil
}

// ListActiveAgentSessions returns all sessions without ended_at set.
func ListActiveAgentSessions(ctx context.Context, db *DB) ([]AgentSession, error) {
	const q = `
		SELECT session_id, agent_id, mode, started_at, last_seen_at, last_progress_at, ended_at, end_reason
		FROM agent_sessions
		WHERE ended_at IS NULL
		ORDER BY last_seen_at DESC`

	rows, err := db.sql.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list active agent sessions: %w", err)
	}
	defer rows.Close()

	return scanAgentSessions(rows)
}

// ListExpiredAgentSessions returns sessions whose last_seen_at has passed the TTL.
func ListExpiredAgentSessions(ctx context.Context, db *DB, ttl time.Duration) ([]AgentSession, error) {
	threshold := time.Now().UTC().Add(-ttl).Truncate(time.Second)
	const q = `
		SELECT session_id, agent_id, mode, started_at, last_seen_at, last_progress_at, ended_at, end_reason
		FROM agent_sessions
		WHERE ended_at IS NULL AND last_seen_at < ?
		ORDER BY last_seen_at ASC`

	rows, err := db.sql.QueryContext(ctx, q, threshold)
	if err != nil {
		return nil, fmt.Errorf("list expired agent sessions: %w", err)
	}
	defer rows.Close()

	return scanAgentSessions(rows)
}

// ExpireAgentSession marks a session and any active assignment as expired.
// It is idempotent at the database level: if the session has already ended,
// the call returns ErrAgentSessionNotFound.
func ExpireAgentSession(ctx context.Context, db *DB, sessionID, reason string) error {
	if reason == "" {
		reason = "expired"
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("expire agent session: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE agent_sessions
		SET ended_at = datetime('now'), end_reason = ?
		WHERE session_id = ? AND ended_at IS NULL`,
		reason, sessionID,
	)
	if err != nil {
		return fmt.Errorf("expire agent session: update session: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("expire agent session: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrAgentSessionNotFound
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_assignments
		SET ended_at = datetime('now'), end_reason = ?
		WHERE session_id = ? AND ended_at IS NULL`,
		reason, sessionID,
	); err != nil {
		return fmt.Errorf("expire agent session: end assignments: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"reason": reason,
	})
	if err != nil {
		return fmt.Errorf("expire agent session: marshal event payload: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		SELECT session_id, agent_id, ?, ?
		FROM agent_sessions
		WHERE session_id = ?`,
		"session_expired",
		string(payload),
		sessionID,
	); err != nil {
		return fmt.Errorf("expire agent session: append event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("expire agent session: commit: %w", err)
	}
	return nil
}

// AttachAgentAssignment inserts a new assignment and supersedes any active
// assignments for the same session.
func AttachAgentAssignment(ctx context.Context, db *DB, assignment *AgentAssignment) error {
	if assignment == nil {
		return fmt.Errorf("attach agent assignment: nil assignment")
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("attach agent assignment: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE agent_sessions
		SET last_seen_at = datetime('now'),
		    last_progress_at = datetime('now')
		WHERE session_id = ? AND ended_at IS NULL`,
		assignment.SessionID,
	)
	if err != nil {
		return fmt.Errorf("attach agent assignment: touch session: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("attach agent assignment: touch session rows affected: %w", err)
	}
	if affected == 0 {
		return ErrAgentSessionNotFound
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE agent_assignments
		SET ended_at = datetime('now'), end_reason = 'superseded', superseded_by = ?
		WHERE session_id = ? AND ended_at IS NULL`,
		assignment.ID, assignment.SessionID,
	)
	if err != nil {
		return fmt.Errorf("attach agent assignment: supersede: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_assignments (
			assignment_id, session_id, agent_id, repo, branch, worktree, task_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		assignment.ID, assignment.SessionID, assignment.AgentID,
		assignment.Repo, assignment.Branch, assignment.Worktree, assignment.TaskID,
	)
	if err != nil {
		return fmt.Errorf("attach agent assignment: insert: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"assignment_id": assignment.ID,
		"repo":          assignment.Repo,
		"branch":        assignment.Branch,
		"worktree":      assignment.Worktree,
		"task_id":       assignment.TaskID,
	})
	if err != nil {
		return fmt.Errorf("attach agent assignment: marshal event payload: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		VALUES (?, ?, ?, ?)`,
		assignment.SessionID,
		assignment.AgentID,
		"assignment_attached",
		string(payload),
	); err != nil {
		return fmt.Errorf("attach agent assignment: append event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("attach agent assignment: commit: %w", err)
	}
	return nil
}

// GetActiveAgentAssignment returns the most recent active assignment for a session.
func GetActiveAgentAssignment(ctx context.Context, db *DB, sessionID string) (*AgentAssignment, error) {
	const q = `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
		       started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE session_id = ? AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1`

	row := db.sql.QueryRowContext(ctx, q, sessionID)
	return scanAgentAssignment(row)
}

// ListAgentAssignments returns all assignments for a session.
func ListAgentAssignments(ctx context.Context, db *DB, sessionID string) ([]AgentAssignment, error) {
	const q = `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
		       started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE session_id = ?
		ORDER BY started_at ASC`

	rows, err := db.sql.QueryContext(ctx, q, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list agent assignments: %w", err)
	}
	defer rows.Close()

	var assignments []AgentAssignment
	for rows.Next() {
		a, err := scanAgentAssignmentRow(rows)
		if err != nil {
			return nil, fmt.Errorf("list agent assignments: %w", err)
		}
		assignments = append(assignments, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agent assignments: %w", err)
	}
	return assignments, nil
}

// AppendAgentEvent appends an event for the given session.
func AppendAgentEvent(ctx context.Context, db *DB, ev *AgentEvent) (int64, error) {
	if ev == nil {
		return 0, fmt.Errorf("append agent event: nil event")
	}
	res, err := db.sql.ExecContext(ctx,
		`INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		 VALUES (?, ?, ?, ?)`,
		ev.SessionID, ev.AgentID, ev.EventType, ev.Payload,
	)
	if err != nil {
		return 0, fmt.Errorf("append agent event: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("append agent event: read id: %w", err)
	}
	return id, nil
}

// ListAgentEvents returns events for a session with id > sinceID.
func ListAgentEvents(ctx context.Context, db *DB, sessionID string, sinceID int64) ([]AgentEvent, error) {
	const q = `
		SELECT id, session_id, agent_id, event_type, payload, created_at
		FROM agent_events
		WHERE session_id = ? AND id > ?
		ORDER BY id ASC`

	rows, err := db.sql.QueryContext(ctx, q, sessionID, sinceID)
	if err != nil {
		return nil, fmt.Errorf("list agent events: %w", err)
	}
	defer rows.Close()

	var events []AgentEvent
	for rows.Next() {
		var ev AgentEvent
		if err := rows.Scan(
			&ev.ID, &ev.SessionID, &ev.AgentID, &ev.EventType, &ev.Payload, &ev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agent events: %w", err)
	}
	return events, nil
}

// ListRecentAgentEvents returns recent agent events across all sessions.
func ListRecentAgentEvents(ctx context.Context, db *DB, limit int) ([]AgentEvent, error) {
	const q = `
		SELECT id, session_id, agent_id, event_type, payload, created_at
		FROM agent_events
		ORDER BY created_at DESC, id DESC
		LIMIT ?`

	rows, err := db.sql.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent agent events: %w", err)
	}
	defer rows.Close()

	var events []AgentEvent
	for rows.Next() {
		var ev AgentEvent
		if err := rows.Scan(
			&ev.ID, &ev.SessionID, &ev.AgentID, &ev.EventType, &ev.Payload, &ev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recent agent event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list recent agent events: %w", err)
	}
	return events, nil
}

func scanAgentSession(row *sql.Row) (*AgentSession, error) {
	var s AgentSession
	var lastProgressAt sql.NullTime
	var endedAt sql.NullTime

	err := row.Scan(
		&s.SessionID, &s.AgentID, &s.Mode, &s.StartedAt, &s.LastSeenAt, &lastProgressAt, &endedAt, &s.EndReason,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentSessionNotFound
		}
		return nil, fmt.Errorf("scan agent session: %w", err)
	}
	if lastProgressAt.Valid {
		s.LastProgressAt = &lastProgressAt.Time
	}
	if endedAt.Valid {
		s.EndedAt = &endedAt.Time
	}
	return &s, nil
}

func scanAgentSessions(rows *sql.Rows) ([]AgentSession, error) {
	var sessions []AgentSession
	for rows.Next() {
		var s AgentSession
		var lastProgressAt sql.NullTime
		var endedAt sql.NullTime
		if err := rows.Scan(
			&s.SessionID, &s.AgentID, &s.Mode, &s.StartedAt, &s.LastSeenAt, &lastProgressAt, &endedAt, &s.EndReason,
		); err != nil {
			return nil, fmt.Errorf("scan agent session row: %w", err)
		}
		if lastProgressAt.Valid {
			s.LastProgressAt = &lastProgressAt.Time
		}
		if endedAt.Valid {
			s.EndedAt = &endedAt.Time
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan agent sessions: %w", err)
	}
	return sessions, nil
}

func scanAgentAssignment(row *sql.Row) (*AgentAssignment, error) {
	var a AgentAssignment
	var endedAt sql.NullTime
	var supersededBy sql.NullString

	err := row.Scan(
		&a.ID, &a.SessionID, &a.AgentID, &a.Repo, &a.Branch, &a.Worktree, &a.TaskID,
		&a.StartedAt, &endedAt, &a.EndReason, &supersededBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentAssignmentNotFound
		}
		return nil, fmt.Errorf("scan agent assignment: %w", err)
	}
	if endedAt.Valid {
		a.EndedAt = &endedAt.Time
	}
	if supersededBy.Valid {
		a.SupersededBy = &supersededBy.String
	}
	return &a, nil
}

func scanAgentAssignmentRow(rows *sql.Rows) (*AgentAssignment, error) {
	var a AgentAssignment
	var endedAt sql.NullTime
	var supersededBy sql.NullString

	if err := rows.Scan(
		&a.ID, &a.SessionID, &a.AgentID, &a.Repo, &a.Branch, &a.Worktree, &a.TaskID,
		&a.StartedAt, &endedAt, &a.EndReason, &supersededBy,
	); err != nil {
		return nil, fmt.Errorf("scan agent assignment row: %w", err)
	}
	if endedAt.Valid {
		a.EndedAt = &endedAt.Time
	}
	if supersededBy.Valid {
		a.SupersededBy = &supersededBy.String
	}
	return &a, nil
}
