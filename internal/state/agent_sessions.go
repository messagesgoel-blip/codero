package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	sqlite3 "github.com/mattn/go-sqlite3"
)

// ErrAgentSessionNotFound is returned when an agent session record is missing.
var ErrAgentSessionNotFound = errors.New("agent session not found")

// ErrAgentSessionAlreadyEnded is returned when a single-use session ID is
// re-registered after its historical row already ended.
var ErrAgentSessionAlreadyEnded = errors.New("agent session already ended")
var ErrAgentSessionAgentMismatch = errors.New("agent session agent mismatch")

// ErrInvalidHeartbeatSecret is returned when the heartbeat_secret does not
// match the stored value. EL-23: launcher-only enforcement.
var ErrInvalidHeartbeatSecret = errors.New("invalid heartbeat secret")

// ErrAgentAssignmentNotFound is returned when an agent assignment record is missing.
var ErrAgentAssignmentNotFound = errors.New("agent assignment not found")

// ErrTaskAlreadyClaimed is returned when a live assignment for the requested
// task_id already belongs to a different session.
var ErrTaskAlreadyClaimed = errors.New("task already claimed by another live session")

// ErrVersionConflict is returned when an emit provides a stale
// assignment_version that does not match the durable row.
var ErrVersionConflict = errors.New("assignment version conflict")

// ErrHandoffRestricted is returned when a task has a handoff nomination for
// a specific successor session and the requesting session is not the nominee.
var ErrHandoffRestricted = errors.New("task handoff restricted to nominated session")

// ErrAssignmentEnded is returned when an emit targets an assignment whose
// ended_at is already set (terminal/superseded/lost).
var ErrAssignmentEnded = errors.New("assignment already ended")

// AgentSession is a row from agent_sessions.
type AgentSession struct {
	SessionID       string
	AgentID         string
	Mode            string
	TmuxSessionName string
	StartedAt       time.Time
	LastSeenAt      time.Time
	LastProgressAt  *time.Time
	LastIOAt        *time.Time
	EndedAt         *time.Time
	EndReason       string
}

// AgentAssignment is a row from agent_assignments.
type AgentAssignment struct {
	ID            string
	SessionID     string
	AgentID       string
	Repo          string
	Branch        string
	Worktree      string
	TaskID        string
	State         string
	BlockedReason string
	Substatus     string
	Version       int
	DeliveryState string
	StartedAt     time.Time
	EndedAt       *time.Time
	EndReason     string
	SupersededBy  *string
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

type AgentSessionCompletion struct {
	TaskID     string    `json:"task_id"`
	Status     string    `json:"status"`
	Substatus  string    `json:"substatus"`
	Summary    string    `json:"summary"`
	Tests      []string  `json:"tests"`
	FinishedAt time.Time `json:"finished_at"`
}

type agentAssignmentContext struct {
	Session    AgentSession
	Assignment AgentAssignment
}

// RegisterAgentSession inserts or updates a session registration.
// Registration starts with session_id + agent_id; mode and tmuxSessionName
// are optional and only update when provided.
func RegisterAgentSession(ctx context.Context, db *DB, sessionID, agentID, mode, tmuxSessionName string) error {
	_, err := RegisterAgentSessionWithSecret(ctx, db, sessionID, agentID, mode, tmuxSessionName)
	return err
}

// RegisterAgentSessionWithSecret inserts or updates a session registration and
// returns the launcher-owned heartbeat secret used for EL-23 enforcement.
func RegisterAgentSessionWithSecret(ctx context.Context, db *DB, sessionID, agentID, mode, tmuxSessionName string) (string, error) {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("register agent session: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var endedAt sql.NullTime
	var existingSecret sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT ended_at, heartbeat_secret
		FROM agent_sessions
		WHERE session_id = ?`,
		sessionID,
	).Scan(&endedAt, &existingSecret)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("register agent session: check existing row: %w", err)
	}
	if err == nil && endedAt.Valid && isSingleUseSessionID(sessionID) {
		return "", ErrAgentSessionAlreadyEnded
	}

	heartbeatSecret := strings.TrimSpace(existingSecret.String)
	if heartbeatSecret == "" {
		heartbeatSecret = uuid.New().String()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_sessions (session_id, agent_id, mode, tmux_session_name, started_at, last_seen_at, heartbeat_secret)
		VALUES (?, ?, ?, ?, datetime('now'), datetime('now'), ?)
		ON CONFLICT(session_id) DO UPDATE SET
			agent_id = excluded.agent_id,
			mode = COALESCE(NULLIF(excluded.mode, ''), agent_sessions.mode),
			tmux_session_name = COALESCE(NULLIF(excluded.tmux_session_name, ''), agent_sessions.tmux_session_name),
			ended_at = NULL,
			end_reason = '',
			last_seen_at = datetime('now'),
			heartbeat_secret = COALESCE(NULLIF(agent_sessions.heartbeat_secret, ''), excluded.heartbeat_secret)`,
		sessionID, agentID, mode, tmuxSessionName, heartbeatSecret,
	)
	if err != nil {
		return "", fmt.Errorf("register agent session: %w", err)
	}
	payload, err := json.Marshal(map[string]string{
		"mode": mode,
	})
	if err != nil {
		return "", fmt.Errorf("register agent session: marshal event payload: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		VALUES (?, ?, ?, ?)`,
		sessionID, agentID, "session_registered", string(payload),
	); err != nil {
		return "", fmt.Errorf("register agent session: append event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("register agent session: commit: %w", err)
	}
	return heartbeatSecret, nil
}

func isSingleUseSessionID(sessionID string) bool {
	_, err := uuid.Parse(sessionID)
	return err == nil
}

// ValidateHeartbeatSecret checks that the provided secret matches the stored
// heartbeat_secret for the session. EL-23: only the launcher (which received
// the secret from RegisterSession) can heartbeat.
func ValidateHeartbeatSecret(ctx context.Context, db *DB, sessionID, secret string) error {
	var stored string
	err := db.sql.QueryRowContext(ctx, `
		SELECT heartbeat_secret FROM agent_sessions
		WHERE session_id = ? AND ended_at IS NULL`,
		sessionID,
	).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("validate heartbeat secret: %w", err)
	}
	if stored == "" || stored != secret {
		return ErrInvalidHeartbeatSecret
	}
	return nil
}

// UpdateAgentSessionHeartbeat updates the last_seen_at timestamp for a session.
// When markProgress is true, it also refreshes last_io_at (raw I/O activity for
// stall detection) and last_progress_at (60-minute compliance rule).
func UpdateAgentSessionHeartbeat(ctx context.Context, db *DB, sessionID string, markProgress bool) error {
	now := time.Now().UTC().Truncate(time.Second)

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update agent session heartbeat: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := `
		UPDATE agent_sessions
		SET last_seen_at = ?`
	args := []any{now}
	if markProgress {
		query += `,
			last_io_at = ?,
			last_progress_at = ?`
		args = append(args, now, now)
	}
	query += `
		WHERE session_id = ? AND ended_at IS NULL`
	args = append(args, sessionID)

	res, err := tx.ExecContext(ctx, query, args...)
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

	contextRow, err := loadActiveAssignmentContextTx(ctx, tx, sessionID)
	if err != nil && !errors.Is(err, ErrAgentAssignmentNotFound) {
		return fmt.Errorf("update agent session heartbeat: load active assignment context: %w", err)
	}
	if err == nil {
		contextRow.Session.LastSeenAt = now
		if markProgress {
			contextRow.Session.LastProgressAt = &now
			contextRow.Session.LastIOAt = &now
		}
		rule004 := evaluateRule004HeartbeatProgress(now, &contextRow.Session, &contextRow.Assignment)
		result := "pass"
		violationRaised := false
		resolvedBy := "codero"
		if !rule004.Pass {
			result = "fail"
			violationRaised = true
			resolvedBy = ""
		}
		detail := rule004.Detail
		if !rule004.Pass {
			detail["failure_path"] = rule004.FailurePath
			detail["classification"] = rule004.Classification
		}
		if err := recordAssignmentRuleCheckTx(ctx, tx, contextRow.Assignment.ID, sessionID, RuleIDHeartbeatProgress, result, violationRaised, detail, resolvedBy); err != nil {
			return fmt.Errorf("update agent session heartbeat: record RULE-004: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update agent session heartbeat: commit: %w", err)
	}
	return nil
}

// GetAgentSession retrieves a session by ID.
func GetAgentSession(ctx context.Context, db *DB, sessionID string) (*AgentSession, error) {
	const q = `
		SELECT session_id, agent_id, mode, tmux_session_name, started_at, last_seen_at, last_progress_at, last_io_at, ended_at, end_reason
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
		return fmt.Errorf("confirm agent session %q: %w", sessionID, err)
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
		SELECT session_id, agent_id, mode, tmux_session_name, started_at, last_seen_at, last_progress_at, last_io_at, ended_at, end_reason
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
		SELECT session_id, agent_id, mode, tmux_session_name, started_at, last_seen_at, last_progress_at, last_io_at, ended_at, end_reason
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
	now := time.Now().UTC().Truncate(time.Second)

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("expire agent session: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	contextRow, err := loadActiveAssignmentContextTx(ctx, tx, sessionID)
	if err != nil && !errors.Is(err, ErrAgentAssignmentNotFound) {
		return fmt.Errorf("expire agent session: load active assignment context: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE agent_sessions
		SET ended_at = ?, end_reason = ?
		WHERE session_id = ? AND ended_at IS NULL`,
		now, reason, sessionID,
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

	terminalSubstatus := AssignmentSubstatusTerminalLost
	if reason == "stuck_abandoned" {
		terminalSubstatus = AssignmentSubstatusTerminalStuckAbandoned
	}
	if contextRow != nil {
		rule004 := evaluateRule004HeartbeatProgress(now, &contextRow.Session, &contextRow.Assignment)
		detail := rule004.Detail
		if reason == "expired" && detail["reason"] == "protocol_ok" {
			detail["reason"] = "heartbeat_missing"
			detail["failure_path"] = "lost"
			detail["classification"] = "infrastructure_failure"
		}
		if reason == "stuck_abandoned" {
			detail["reason"] = "progress_stale"
			detail["failure_path"] = "stuck"
			detail["classification"] = "stuck_assignment"
		}
		if err := recordAssignmentRuleCheckTx(ctx, tx, contextRow.Assignment.ID, sessionID, RuleIDHeartbeatProgress, "fail", true, detail, ""); err != nil {
			return fmt.Errorf("expire agent session: record RULE-004: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_assignments
		SET ended_at = ?,
		    end_reason = ?,
		    state = ?,
		    blocked_reason = '',
		    assignment_substatus = ?,
		    assignment_version = assignment_version + 1
		WHERE session_id = ? AND ended_at IS NULL`,
		now, reason, string(assignmentStateLost), terminalSubstatus, sessionID,
	); err != nil {
		return fmt.Errorf("expire agent session: end assignments: %w", err)
	}

	if contextRow != nil {
		if err := clearAssignmentBranchOwnershipTx(ctx, tx, &contextRow.Assignment); err != nil {
			return fmt.Errorf("expire agent session: clear branch ownership: %w", err)
		}
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

	// SL-1/SL-3: Write session_archives row atomically in the same transaction.
	if err := archiveSessionTx(ctx, tx, sessionID, reason, "", now); err != nil {
		return fmt.Errorf("expire agent session: write archive: %w", err)
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
	substatus, err := validateAttachAssignmentSubstatus(assignment.Substatus)
	if err != nil {
		return fmt.Errorf("attach agent assignment: %w", err)
	}
	assignment.Substatus = substatus
	assignment.State = string(assignmentStateActive)
	assignment.BlockedReason = ""
	now := time.Now().UTC().Truncate(time.Second)
	if assignment.StartedAt.IsZero() {
		assignment.StartedAt = now
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("attach agent assignment: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE agent_sessions
		SET last_seen_at = ?,
		    last_progress_at = ?
		WHERE session_id = ? AND ended_at IS NULL`,
		now, now, assignment.SessionID,
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
		SET ended_at = ?,
		    end_reason = 'superseded',
		    state = ?,
		    blocked_reason = '',
		    assignment_substatus = ?,
		    superseded_by = ?,
		    assignment_version = assignment_version + 1
		WHERE session_id = ? AND ended_at IS NULL`,
		now, string(assignmentStateSuperseded), AssignmentSubstatusTerminalWaitingNextTask, assignment.ID, assignment.SessionID,
	)
	if err != nil {
		return fmt.Errorf("attach agent assignment: supersede: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_assignments (
			assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
			state, blocked_reason, assignment_substatus, started_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		assignment.ID, assignment.SessionID, assignment.AgentID,
		assignment.Repo, assignment.Branch, assignment.Worktree, assignment.TaskID,
		assignment.State, assignment.BlockedReason, assignment.Substatus, assignment.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("attach agent assignment: insert: %w", err)
	}
	if err := seedPendingAssignmentRuleChecksTx(ctx, tx, assignment.ID, assignment.SessionID); err != nil {
		return fmt.Errorf("attach agent assignment: seed rule checks: %w", err)
	}
	if err := recordAssignmentRuleCheckTx(ctx, tx, assignment.ID, assignment.SessionID, RuleIDNoSilentFailure, "pass", false, map[string]any{
		"state":     string(assignmentStateActive),
		"substatus": assignment.Substatus,
	}, "codero"); err != nil {
		return fmt.Errorf("attach agent assignment: record RULE-002: %w", err)
	}
	rule003Pass, _, rule003Detail := evaluateRule003BranchHoldTTL(now, assignment)
	rule003Result := "pass"
	rule003Violation := false
	rule003ResolvedBy := "codero"
	if !rule003Pass {
		rule003Result = "fail"
		rule003Violation = true
		rule003ResolvedBy = ""
	}
	if err := recordAssignmentRuleCheckTx(ctx, tx, assignment.ID, assignment.SessionID, RuleIDBranchHoldTTL, rule003Result, rule003Violation, rule003Detail, rule003ResolvedBy); err != nil {
		return fmt.Errorf("attach agent assignment: record RULE-003: %w", err)
	}
	session := AgentSession{
		SessionID:      assignment.SessionID,
		AgentID:        assignment.AgentID,
		StartedAt:      now,
		LastSeenAt:     now,
		LastProgressAt: &now,
	}
	rule004 := evaluateRule004HeartbeatProgress(now, &session, assignment)
	if err := recordAssignmentRuleCheckTx(ctx, tx, assignment.ID, assignment.SessionID, RuleIDHeartbeatProgress, "pass", false, rule004.Detail, "codero"); err != nil {
		return fmt.Errorf("attach agent assignment: record RULE-004: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"assignment_id": assignment.ID,
		"repo":          assignment.Repo,
		"branch":        assignment.Branch,
		"worktree":      assignment.Worktree,
		"task_id":       assignment.TaskID,
		"substatus":     assignment.Substatus,
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
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus,
		       assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE session_id = ? AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1`

	row := db.sql.QueryRowContext(ctx, q, sessionID)
	return scanAgentAssignment(row)
}

// SetSessionTaskID updates the task_id on the active (non-ended) assignment for
// a session. Returns ErrAgentAssignmentNotFound if there is no active
// assignment. The repo field is only updated when non-empty.
func SetSessionTaskID(ctx context.Context, db *DB, sessionID, taskID, repo string) error {
	if sessionID == "" {
		return fmt.Errorf("set session task id: session_id is required")
	}
	if taskID == "" {
		return fmt.Errorf("set session task id: task_id is required")
	}
	var res sql.Result
	var err error
	if repo != "" {
		res, err = db.sql.ExecContext(ctx, `
			UPDATE agent_assignments
			SET task_id = ?, repo = ?, assignment_version = assignment_version + 1
			WHERE session_id = ? AND ended_at IS NULL`,
			taskID, repo, sessionID,
		)
	} else {
		res, err = db.sql.ExecContext(ctx, `
			UPDATE agent_assignments
			SET task_id = ?, assignment_version = assignment_version + 1
			WHERE session_id = ? AND ended_at IS NULL`,
			taskID, sessionID,
		)
	}
	if err != nil {
		return fmt.Errorf("set session task id: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set session task id: rows affected: %w", err)
	}
	if affected == 0 {
		return ErrAgentAssignmentNotFound
	}
	return nil
}

// GetAgentAssignmentByID returns a single assignment looked up by its unique ID.
func GetAgentAssignmentByID(ctx context.Context, db *DB, assignmentID string) (*AgentAssignment, error) {
	const q = `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus,
		       assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE assignment_id = ?`

	row := db.sql.QueryRowContext(ctx, q, assignmentID)
	return scanAgentAssignment(row)
}

// GetActiveAssignmentByTaskID returns the active assignment for a task_id.
func GetActiveAssignmentByTaskID(ctx context.Context, db *DB, taskID string) (*AgentAssignment, error) {
	const q = `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus,
		       assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE task_id = ? AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1`

	row := db.sql.QueryRowContext(ctx, q, taskID)
	return scanAgentAssignment(row)
}

// ListAgentAssignments returns all assignments for a session.
func ListAgentAssignments(ctx context.Context, db *DB, sessionID string) ([]AgentAssignment, error) {
	const q = `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus,
		       assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
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

// FinalizeAgentSession ends a live session, closes its active assignment if any,
// clears branch ownership, and records a completion event payload.
func FinalizeAgentSession(ctx context.Context, db *DB, sessionID, agentID string, completion AgentSessionCompletion) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("finalize agent session: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		session             AgentSession
		sessionLastProgress sql.NullTime
		endedAt             sql.NullTime
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT session_id, agent_id, mode, tmux_session_name, started_at, last_seen_at, last_progress_at, ended_at, end_reason
		FROM agent_sessions
		WHERE session_id = ?`,
		sessionID,
	).Scan(&session.SessionID, &session.AgentID, &session.Mode, &session.TmuxSessionName, &session.StartedAt, &session.LastSeenAt, &sessionLastProgress, &endedAt, &session.EndReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAgentSessionNotFound
		}
		return fmt.Errorf("finalize agent session: load session %q: %w", sessionID, err)
	}
	if endedAt.Valid {
		return ErrAgentSessionAlreadyEnded
	}
	session.EndedAt = nil
	if sessionLastProgress.Valid {
		session.LastProgressAt = &sessionLastProgress.Time
	}
	if agentID != "" && session.AgentID != agentID {
		return ErrAgentSessionAgentMismatch
	}

	finishedAt := completion.FinishedAt.UTC()
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}

	var active *AgentAssignment
	row := tx.QueryRowContext(ctx, `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus,
		       assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE session_id = ? AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1`,
		sessionID,
	)
	if a, err := scanAgentAssignment(row); err == nil {
		active = a
	} else if !errors.Is(err, ErrAgentAssignmentNotFound) {
		return fmt.Errorf("finalize agent session: load active assignment %q: %w", sessionID, err)
	}
	targetState, substatus, err := validateTerminalAssignmentSubstatus(completion.Status, completion.Substatus)
	if err != nil {
		if active != nil {
			if recordErr := recordAssignmentRuleCheckTx(ctx, tx, active.ID, sessionID, RuleIDNoSilentFailure, "fail", true, map[string]any{
				"status":    completion.Status,
				"substatus": completion.Substatus,
				"reason":    err.Error(),
			}, ""); recordErr != nil {
				return fmt.Errorf("finalize agent session: record RULE-002 failure: %w", recordErr)
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return fmt.Errorf("finalize agent session: persist RULE-002 failure: %w", commitErr)
			}
		}
		return err
	}
	if active != nil {
		if err := recordAssignmentRuleCheckTx(ctx, tx, active.ID, sessionID, RuleIDNoSilentFailure, "pass", false, map[string]any{
			"state":     string(targetState),
			"status":    completion.Status,
			"substatus": substatus,
		}, "codero"); err != nil {
			return fmt.Errorf("finalize agent session: record RULE-002: %w", err)
		}

		rule003Pass, _, rule003Detail := evaluateRule003BranchHoldTTL(finishedAt, active)
		rule003Result := "pass"
		rule003Violation := false
		rule003ResolvedBy := "codero"
		if !rule003Pass {
			rule003Result = "fail"
			rule003Violation = true
			rule003ResolvedBy = ""
		}
		if err := recordAssignmentRuleCheckTx(ctx, tx, active.ID, sessionID, RuleIDBranchHoldTTL, rule003Result, rule003Violation, rule003Detail, rule003ResolvedBy); err != nil {
			return fmt.Errorf("finalize agent session: record RULE-003: %w", err)
		}

		rule004 := evaluateRule004HeartbeatProgress(finishedAt, &session, active)
		rule004Result := "pass"
		rule004Violation := false
		rule004ResolvedBy := "codero"
		rule004Detail := rule004.Detail
		if !rule004.Pass {
			rule004Result = "fail"
			rule004Violation = true
			rule004ResolvedBy = ""
			rule004Detail["failure_path"] = rule004.FailurePath
			rule004Detail["classification"] = rule004.Classification
		}
		if err := recordAssignmentRuleCheckTx(ctx, tx, active.ID, sessionID, RuleIDHeartbeatProgress, rule004Result, rule004Violation, rule004Detail, rule004ResolvedBy); err != nil {
			return fmt.Errorf("finalize agent session: record RULE-004: %w", err)
		}

		rule001Pass := true
		rule001Detail := map[string]any{
			"reason": "not_applicable",
		}
		if targetState == assignmentStateCompleted {
			rule001Pass, rule001Detail, err = evaluateRule001CompletionTx(ctx, tx, active)
			if err != nil {
				return fmt.Errorf("finalize agent session: evaluate RULE-001: %w", err)
			}
		}
		rule001Result := "pass"
		rule001Violation := false
		resolvedBy := "codero"
		if !rule001Pass {
			rule001Result = "fail"
			rule001Violation = true
			resolvedBy = ""
		}
		if err := recordAssignmentRuleCheckTx(ctx, tx, active.ID, sessionID, RuleIDGateMustPassBeforeMerge, rule001Result, rule001Violation, rule001Detail, resolvedBy); err != nil {
			return fmt.Errorf("finalize agent session: record RULE-001: %w", err)
		}

		if targetState == assignmentStateCompleted {
			blockers, err := activeRuleBlockersTx(ctx, tx, active.ID)
			if err != nil {
				return fmt.Errorf("finalize agent session: list rule blockers: %w", err)
			}
			if blockErr := assignmentCompletionError(blockers); blockErr != nil {
				if err := tx.Commit(); err != nil {
					return fmt.Errorf("finalize agent session: persist rule blockers: %w", err)
				}
				return blockErr
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_sessions
		SET ended_at = ?, end_reason = ?
		WHERE session_id = ? AND ended_at IS NULL`,
		finishedAt, completion.Status, sessionID,
	); err != nil {
		return fmt.Errorf("finalize agent session: end session: %w", err)
	}

	if active != nil {
		if completion.TaskID == "" {
			completion.TaskID = active.TaskID
		}
		blockedReason := blockedReasonFromSubstatus(substatus)
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_assignments
			SET ended_at = ?, end_reason = ?, state = ?, blocked_reason = ?, assignment_substatus = ?, assignment_version = assignment_version + 1
			WHERE assignment_id = ? AND ended_at IS NULL`,
			finishedAt, completion.Status, string(targetState), blockedReason, substatus, active.ID,
		); err != nil {
			return fmt.Errorf("finalize agent session: end assignment: %w", err)
		}
		if err := clearAssignmentBranchOwnershipTx(ctx, tx, active); err != nil {
			return fmt.Errorf("finalize agent session: clear branch ownership: %w", err)
		}
	}

	payload, err := json.Marshal(completion)
	if err != nil {
		return fmt.Errorf("finalize agent session: marshal payload: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		VALUES (?, ?, ?, ?)`,
		sessionID, session.AgentID, "session_finalized", string(payload),
	); err != nil {
		return fmt.Errorf("finalize agent session: append event: %w", err)
	}

	// SL-1/SL-3: Write session_archives row atomically in the same transaction.
	if err := archiveSessionTx(ctx, tx, sessionID, completion.Status, "", finishedAt); err != nil {
		return fmt.Errorf("finalize agent session: write archive: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("finalize agent session: commit: %w", err)
	}
	return nil
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

// MonitorAgentAssignmentRules evaluates active assignment compliance rows and
// applies hard enforcement for protocol and hold violations.
func MonitorAgentAssignmentRules(ctx context.Context, db *DB, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC().Truncate(time.Second)
	}

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("monitor agent assignment rules: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	contexts, err := listActiveAssignmentContextsTx(ctx, tx)
	if err != nil {
		return fmt.Errorf("monitor agent assignment rules: list active assignment contexts: %w", err)
	}

	for _, contextRow := range contexts {
		rule004 := evaluateRule004HeartbeatProgress(now, &contextRow.Session, &contextRow.Assignment)
		rule004Result := "pass"
		rule004Violation := false
		rule004ResolvedBy := "codero"
		rule004Detail := rule004.Detail
		if !rule004.Pass {
			rule004Result = "fail"
			rule004Violation = true
			rule004ResolvedBy = ""
			rule004Detail["failure_path"] = rule004.FailurePath
			rule004Detail["classification"] = rule004.Classification
		}
		if err := recordAssignmentRuleCheckTx(ctx, tx, contextRow.Assignment.ID, contextRow.Session.SessionID, RuleIDHeartbeatProgress, rule004Result, rule004Violation, rule004Detail, rule004ResolvedBy); err != nil {
			return fmt.Errorf("monitor agent assignment rules: record RULE-004 for %s: %w", contextRow.Assignment.ID, err)
		}
		if !rule004.Pass {
			reason := "lost"
			substatus := AssignmentSubstatusTerminalLost
			eventType := "session_protocol_lost"
			if rule004.FailurePath == "stuck" {
				reason = "stuck_abandoned"
				substatus = AssignmentSubstatusTerminalStuckAbandoned
				eventType = "session_protocol_stuck"
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE agent_sessions
				SET ended_at = ?, end_reason = ?
				WHERE session_id = ? AND ended_at IS NULL`,
				now, reason, contextRow.Session.SessionID,
			); err != nil {
				return fmt.Errorf("monitor agent assignment rules: end session %s: %w", contextRow.Session.SessionID, err)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE agent_assignments
				SET ended_at = ?, end_reason = ?, state = ?, blocked_reason = '', assignment_substatus = ?, assignment_version = assignment_version + 1
				WHERE assignment_id = ? AND ended_at IS NULL`,
				now, reason, string(assignmentStateLost), substatus, contextRow.Assignment.ID,
			); err != nil {
				return fmt.Errorf("monitor agent assignment rules: end assignment %s: %w", contextRow.Assignment.ID, err)
			}
			if err := clearAssignmentBranchOwnershipTx(ctx, tx, &contextRow.Assignment); err != nil {
				return fmt.Errorf("monitor agent assignment rules: clear branch ownership for %s: %w", contextRow.Assignment.ID, err)
			}
			if err := appendAgentEventTx(ctx, tx, contextRow.Session.SessionID, contextRow.Session.AgentID, eventType, map[string]any{
				"assignment_id":  contextRow.Assignment.ID,
				"rule_id":        RuleIDHeartbeatProgress,
				"failure_path":   rule004.FailurePath,
				"classification": rule004.Classification,
				"substatus":      substatus,
			}); err != nil {
				return fmt.Errorf("monitor agent assignment rules: append %s: %w", eventType, err)
			}
			continue
		}

		rule003Pass, forceCancel, rule003Detail := evaluateRule003BranchHoldTTL(now, &contextRow.Assignment)
		rule003Result := "pass"
		rule003Violation := false
		rule003ResolvedBy := "codero"
		if !rule003Pass {
			rule003Result = "fail"
			rule003Violation = true
			rule003ResolvedBy = ""
		}
		if err := recordAssignmentRuleCheckTx(ctx, tx, contextRow.Assignment.ID, contextRow.Session.SessionID, RuleIDBranchHoldTTL, rule003Result, rule003Violation, rule003Detail, rule003ResolvedBy); err != nil {
			return fmt.Errorf("monitor agent assignment rules: record RULE-003 for %s: %w", contextRow.Assignment.ID, err)
		}
		if forceCancel {
			if _, err := tx.ExecContext(ctx, `
				UPDATE agent_assignments
				SET ended_at = ?, end_reason = 'cancelled', state = ?, blocked_reason = '', assignment_substatus = ?, assignment_version = assignment_version + 1
				WHERE assignment_id = ? AND ended_at IS NULL`,
				now, string(assignmentStateCancelled), AssignmentSubstatusTerminalCancelled, contextRow.Assignment.ID,
			); err != nil {
				return fmt.Errorf("monitor agent assignment rules: cancel assignment %s: %w", contextRow.Assignment.ID, err)
			}
			if err := clearAssignmentBranchOwnershipTx(ctx, tx, &contextRow.Assignment); err != nil {
				return fmt.Errorf("monitor agent assignment rules: clear cancelled branch ownership for %s: %w", contextRow.Assignment.ID, err)
			}
			if err := appendAgentEventTx(ctx, tx, contextRow.Session.SessionID, contextRow.Session.AgentID, "assignment_auto_cancelled", map[string]any{
				"assignment_id": contextRow.Assignment.ID,
				"rule_id":       RuleIDBranchHoldTTL,
				"substatus":     AssignmentSubstatusTerminalCancelled,
				"reason":        rule003Detail["reason"],
			}); err != nil {
				return fmt.Errorf("monitor agent assignment rules: append assignment_auto_cancelled: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("monitor agent assignment rules: commit: %w", err)
	}
	return nil
}

// ReconcileAgentAssignmentWaitingState updates the active assignment substatus
// for a branch when Codero polling has enough information to advance or regress
// the waiting state.
func ReconcileAgentAssignmentWaitingState(ctx context.Context, db *DB, repo, branch string) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reconcile assignment waiting state: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		branchState       string
		approvedInt       int
		ciGreenInt        int
		pendingEvents     int
		unresolvedThreads int
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT state, approved, ci_green, pending_events, unresolved_threads
		FROM branch_states
		WHERE repo = ? AND branch = ?`,
		repo, branch,
	).Scan(&branchState, &approvedInt, &ciGreenInt, &pendingEvents, &unresolvedThreads); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("reconcile assignment waiting state: load branch %s/%s: %w", repo, branch, err)
	}

	row := tx.QueryRowContext(ctx, `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus,
		       assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE repo = ? AND branch = ? AND ended_at IS NULL
		ORDER BY started_at DESC
		LIMIT 1`,
		repo, branch,
	)
	assignment, err := scanAgentAssignment(row)
	if err != nil {
		if errors.Is(err, ErrAgentAssignmentNotFound) {
			return nil
		}
		return fmt.Errorf("reconcile assignment waiting state: load active assignment %s/%s: %w", repo, branch, err)
	}

	current := normalizeAssignmentSubstatus(assignment.Substatus)
	if current != AssignmentSubstatusWaitingForCI && current != AssignmentSubstatusWaitingForMergeApproval {
		return nil
	}

	nextSubstatus := nextWaitingAssignmentSubstatus(branchState, approvedInt != 0, ciGreenInt != 0, pendingEvents, unresolvedThreads)
	if nextSubstatus == current {
		return nil
	}

	nextState := assignmentStateFromSubstatus(nextSubstatus)
	if nextState == "" {
		nextState = string(assignmentStateActive)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_assignments
		SET state = ?, blocked_reason = '', assignment_substatus = ?, assignment_version = assignment_version + 1
		WHERE assignment_id = ? AND ended_at IS NULL`,
		nextState, nextSubstatus, assignment.ID,
	); err != nil {
		return fmt.Errorf("reconcile assignment waiting state: update assignment %s: %w", assignment.ID, err)
	}
	if err := appendAgentEventTx(ctx, tx, assignment.SessionID, assignment.AgentID, "assignment_substatus_updated", map[string]any{
		"assignment_id":      assignment.ID,
		"repo":               repo,
		"branch":             branch,
		"from_substatus":     current,
		"to_substatus":       nextSubstatus,
		"branch_state":       branchState,
		"approved":           approvedInt != 0,
		"ci_green":           ciGreenInt != 0,
		"pending_events":     pendingEvents,
		"unresolved_threads": unresolvedThreads,
	}); err != nil {
		return fmt.Errorf("reconcile assignment waiting state: append event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reconcile assignment waiting state: commit: %w", err)
	}
	return nil
}

func nextWaitingAssignmentSubstatus(branchState string, approved, ciGreen bool, pendingEvents, unresolvedThreads int) string {
	if !ciGreen {
		return AssignmentSubstatusWaitingForCI
	}
	if branchState == string(StateMergeReady) || branchState == string(StateMerged) {
		if approved && pendingEvents == 0 && unresolvedThreads == 0 {
			return AssignmentSubstatusInProgress
		}
	}
	if approved && pendingEvents == 0 && unresolvedThreads == 0 {
		return AssignmentSubstatusInProgress
	}
	return AssignmentSubstatusWaitingForMergeApproval
}

func loadActiveAssignmentContextTx(ctx context.Context, tx *sql.Tx, sessionID string) (*agentAssignmentContext, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT
			s.session_id, s.agent_id, s.mode, s.started_at, s.last_seen_at, s.last_progress_at, s.ended_at, s.end_reason,
			a.assignment_id, a.session_id, a.agent_id, a.repo, a.branch, a.worktree, a.task_id,
			a.state, a.blocked_reason, a.assignment_substatus, a.assignment_version, a.started_at, a.ended_at, a.end_reason, a.superseded_by
		FROM agent_sessions s
		JOIN agent_assignments a ON a.session_id = s.session_id
		WHERE s.session_id = ? AND s.ended_at IS NULL AND a.ended_at IS NULL
		ORDER BY a.started_at DESC
		LIMIT 1`,
		sessionID,
	)
	return scanAgentAssignmentContextRow(row)
}

func listActiveAssignmentContextsTx(ctx context.Context, tx *sql.Tx) ([]agentAssignmentContext, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			s.session_id, s.agent_id, s.mode, s.started_at, s.last_seen_at, s.last_progress_at, s.ended_at, s.end_reason,
			a.assignment_id, a.session_id, a.agent_id, a.repo, a.branch, a.worktree, a.task_id,
			a.state, a.blocked_reason, a.assignment_substatus, a.assignment_version, a.started_at, a.ended_at, a.end_reason, a.superseded_by
		FROM agent_sessions s
		JOIN agent_assignments a ON a.session_id = s.session_id
		WHERE s.ended_at IS NULL AND a.ended_at IS NULL
		ORDER BY a.started_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list active assignment contexts: %w", err)
	}
	defer rows.Close()

	var contexts []agentAssignmentContext
	for rows.Next() {
		contextRow, err := scanAgentAssignmentContextRows(rows)
		if err != nil {
			return nil, err
		}
		contexts = append(contexts, *contextRow)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active assignment contexts: rows: %w", err)
	}
	return contexts, nil
}

func clearAssignmentBranchOwnershipTx(ctx context.Context, tx *sql.Tx, assignment *AgentAssignment) error {
	if assignment == nil || assignment.Repo == "" || assignment.Branch == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE branch_states
		SET owner_session_id = '', owner_session_last_seen = NULL,
		    owner_agent = '', updated_at = datetime('now')
		WHERE repo = ? AND branch = ? AND owner_session_id = ?`,
		assignment.Repo, assignment.Branch, assignment.SessionID,
	); err != nil {
		return err
	}
	return nil
}

func appendAgentEventTx(ctx context.Context, tx *sql.Tx, sessionID, agentID, eventType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		VALUES (?, ?, ?, ?)`,
		sessionID, agentID, eventType, string(body),
	); err != nil {
		return err
	}
	return nil
}

// AcceptTask atomically claims task_id for sessionID.
//
// Idempotency: repeated calls with the same session_id+task_id return the
// existing live assignment without modifying any state.
//
// Conflict: a different session that already holds a live (ended_at IS NULL)
// assignment for the same task_id causes ErrTaskAlreadyClaimed.
//
// Terminal reuse: assignments whose ended_at IS NOT NULL are considered
// complete; they do not block a new claim from any session.
//
// The inserted row inherits assignment_version=1 from the schema default and
// starts in state=active / substatus=in_progress.  The caller must ensure the
// session is already registered via RegisterAgentSession before calling Accept.
func AcceptTask(ctx context.Context, db *DB, sessionID, taskID string) (*AgentAssignment, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("accept task: session_id is required")
	}
	if taskID == "" {
		return nil, fmt.Errorf("accept task: task_id is required")
	}

	now := time.Now().UTC().Truncate(time.Second)

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("accept task: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Verify the session exists and is still live.
	var agentID string
	var sessionEndedAt sql.NullTime
	err = tx.QueryRowContext(ctx,
		`SELECT agent_id, ended_at FROM agent_sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&agentID, &sessionEndedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("accept task: load session: %w", err)
	}
	if sessionEndedAt.Valid {
		return nil, ErrAgentSessionAlreadyEnded
	}

	// Check for a live (ended_at IS NULL) assignment for this task.
	var existingID, existingSession string
	err = tx.QueryRowContext(ctx,
		`SELECT assignment_id, session_id
		 FROM agent_assignments
		 WHERE task_id = ? AND ended_at IS NULL
		 LIMIT 1`,
		taskID,
	).Scan(&existingID, &existingSession)

	if err == nil {
		// A live assignment exists.
		if existingSession != sessionID {
			return nil, fmt.Errorf("%w: assignment %s held by session %s",
				ErrTaskAlreadyClaimed, existingID, existingSession)
		}
		// Same session — idempotent: return the existing row without committing.
		row := tx.QueryRowContext(ctx,
			`SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
			        state, blocked_reason, assignment_substatus,
			        assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
			 FROM agent_assignments WHERE assignment_id = ?`,
			existingID,
		)
		a, scanErr := scanAgentAssignment(row)
		if scanErr != nil {
			return nil, fmt.Errorf("accept task: reload idempotent row: %w", scanErr)
		}
		return a, nil // tx rolled back by defer
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("accept task: check existing claim: %w", err)
	}

	assignmentID := uuid.New().String()

	// A session may hold only one live assignment at a time. Supersede any
	// other live row for this session before inserting the new claim.
	var priorAssignmentID string
	err = tx.QueryRowContext(ctx,
		`SELECT assignment_id
		 FROM agent_assignments
		 WHERE session_id = ? AND ended_at IS NULL AND task_id <> ?
		 LIMIT 1`,
		sessionID, taskID,
	).Scan(&priorAssignmentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("accept task: check existing live session assignment: %w", err)
	}
	if err == nil {
		if _, err = tx.ExecContext(ctx, `
			UPDATE agent_assignments
			SET ended_at = ?,
			    end_reason = 'superseded',
			    state = ?,
			    blocked_reason = '',
			    assignment_substatus = ?,
			    superseded_by = ?,
			    assignment_version = assignment_version + 1
			WHERE assignment_id = ? AND ended_at IS NULL`,
			now, string(assignmentStateSuperseded), AssignmentSubstatusTerminalWaitingNextTask, assignmentID, priorAssignmentID,
		); err != nil {
			return nil, fmt.Errorf("accept task: supersede prior assignment: %w", err)
		}
	}

	// No live claim — insert a new assignment.
	// I-41: Check for handoff nomination. If the most recently ended
	// assignment for this task has a successor_session_id set, only
	// the nominated session may accept.
	var nominatedSession sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT successor_session_id
		 FROM agent_assignments
		 WHERE task_id = ? AND ended_at IS NOT NULL
		   AND successor_session_id IS NOT NULL AND successor_session_id != ''
		 ORDER BY ended_at DESC
		 LIMIT 1`,
		taskID,
	).Scan(&nominatedSession)
	if err == nil && nominatedSession.Valid && nominatedSession.String != "" && nominatedSession.String != sessionID {
		return nil, fmt.Errorf("%w: task %s nominated session %s",
			ErrHandoffRestricted, taskID, nominatedSession.String)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("accept task: check handoff nomination: %w", err)
	}

	// assignment_version defaults to 1 via the schema CHECK (assignment_version >= 1)
	// and DEFAULT 1 added by migration 000011.
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO agent_assignments (
			assignment_id, session_id, agent_id, task_id,
			state, blocked_reason, assignment_substatus, started_at,
			repo, branch, worktree
		) VALUES (?, ?, ?, ?, ?, '', ?, ?, '', '', '')`,
		assignmentID, sessionID, agentID, taskID,
		string(assignmentStateActive), AssignmentSubstatusInProgress, now,
	); err != nil {
		if isLiveTaskConstraintError(err) {
			conflictAssignment, conflictSession, lookupErr := loadLiveTaskClaimTx(ctx, tx, taskID)
			if lookupErr != nil {
				return nil, fmt.Errorf("accept task: reload unique-constraint conflict: %w", lookupErr)
			}
			if conflictSession == sessionID {
				row := tx.QueryRowContext(ctx, `
					SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
					       state, blocked_reason, assignment_substatus,
					       assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
					FROM agent_assignments
					WHERE assignment_id = ?`,
					conflictAssignment,
				)
				a, scanErr := scanAgentAssignment(row)
				if scanErr != nil {
					return nil, fmt.Errorf("accept task: reload same-session unique-constraint row: %w", scanErr)
				}
				return a, nil
			}
			return nil, fmt.Errorf("%w: assignment %s held by session %s",
				ErrTaskAlreadyClaimed, conflictAssignment, conflictSession)
		}
		return nil, fmt.Errorf("accept task: insert: %w", err)
	}

	// §3.2: seed pending rule checks atomically with the new assignment.
	if err := seedPendingAssignmentRuleChecksTx(ctx, tx, assignmentID, sessionID); err != nil {
		return nil, fmt.Errorf("accept task: seed rule checks: %w", err)
	}

	// Touch session heartbeat so last_seen reflects the claim instant.
	if _, err = tx.ExecContext(ctx, `
		UPDATE agent_sessions
		SET last_seen_at = ?, last_progress_at = ?
		WHERE session_id = ?`,
		now, now, sessionID,
	); err != nil {
		return nil, fmt.Errorf("accept task: touch session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("accept task: commit: %w", err)
	}

	return &AgentAssignment{
		ID:        assignmentID,
		SessionID: sessionID,
		AgentID:   agentID,
		TaskID:    taskID,
		State:     string(assignmentStateActive),
		Substatus: AssignmentSubstatusInProgress,
		Version:   1,
		StartedAt: now,
	}, nil
}

// ErrInvalidEmitSubstatus is returned when the emitted substatus is not
// recognized or not valid for an emit transition.
var ErrInvalidEmitSubstatus = errors.New("invalid emit substatus")

var systemOwnedTerminalSubstatuses = map[string]struct{}{
	AssignmentSubstatusTerminalWaitingNextTask: {},
	AssignmentSubstatusTerminalLost:            {},
	AssignmentSubstatusTerminalStuckAbandoned:  {},
}

func isSystemOwnedTerminalSubstatus(substatus string) bool {
	_, ok := systemOwnedTerminalSubstatuses[substatus]
	return ok
}

func validateEmitSubstatusNormalized(substatus string) error {
	if substatus == "" {
		return fmt.Errorf("%w: substatus must not be empty", ErrInvalidEmitSubstatus)
	}
	if _, ok := activeAssignmentSubstatusSet[substatus]; ok {
		return nil
	}
	if _, ok := blockedAssignmentSubstatusSet[substatus]; ok {
		return nil
	}
	if _, ok := terminalAssignmentSubstatusSet[substatus]; ok {
		if isSystemOwnedTerminalSubstatus(substatus) {
			return fmt.Errorf("%w: %q is system-owned and cannot be set via emit", ErrInvalidEmitSubstatus, substatus)
		}
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidEmitSubstatus, substatus)
}

// EmitAssignmentUpdate atomically applies a state/substatus transition to an
// assignment, guarded by optimistic concurrency on assignment_version.
//
// Contract:
//   - assignmentID identifies the target row.
//   - currentVersion is the version the caller believes the row currently has.
//   - newSubstatus is the desired substatus after the emit.
//
// On success the row's state, substatus, blocked_reason, assignment_version,
// and last_emit_at are updated atomically. The returned AgentAssignment
// reflects the post-update row with Version = currentVersion + 1.
//
// Errors:
//   - ErrAgentAssignmentNotFound: no row with that assignment_id.
//   - ErrAssignmentEnded: the assignment already has a non-NULL ended_at.
//   - ErrVersionConflict: the row's current version != currentVersion.
//   - ErrInvalidEmitSubstatus: the substatus is unrecognized.
func EmitAssignmentUpdate(ctx context.Context, db *DB, assignmentID string, currentVersion int, newSubstatus string) (*AgentAssignment, error) {
	normalized := normalizeAssignmentSubstatus(newSubstatus)
	if err := validateEmitSubstatusNormalized(normalized); err != nil {
		return nil, err
	}

	newState := assignmentStateFromSubstatus(normalized)
	newBlockedReason := blockedReasonFromSubstatus(normalized)
	now := time.Now().UTC().Truncate(time.Second)

	// Determine whether this substatus is terminal.
	_, isTerminal := terminalAssignmentSubstatusSet[normalized]

	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("emit assignment update: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Load the full row inside the transaction so we can return an in-memory
	// post-update view without rereading after commit.
	currentRow := tx.QueryRowContext(ctx, `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
		       state, blocked_reason, assignment_substatus,
		       assignment_version, delivery_state, started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE assignment_id = ?`,
		assignmentID,
	)
	assignment, err := scanAgentAssignment(currentRow)
	if err != nil {
		if errors.Is(err, ErrAgentAssignmentNotFound) {
			return nil, ErrAgentAssignmentNotFound
		}
		return nil, fmt.Errorf("emit assignment update: load row: %w", err)
	}

	if assignment.EndedAt != nil {
		return nil, fmt.Errorf("%w: assignment %s ended at %s",
			ErrAssignmentEnded, assignmentID, assignment.EndedAt.Format(time.RFC3339))
	}

	if assignment.Version != currentVersion {
		return nil, fmt.Errorf("%w: expected version %d but row has %d",
			ErrVersionConflict, currentVersion, assignment.Version)
	}

	nextVersion := currentVersion + 1

	// I-43: Load deviation tracking columns for this assignment.
	var suggestedLast sql.NullString
	var deviationCount int
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(suggested_substatus_last, ''), substatus_deviation_count
		 FROM agent_assignments WHERE assignment_id = ?`,
		assignmentID,
	).Scan(&suggestedLast, &deviationCount)
	if err != nil {
		return nil, fmt.Errorf("emit assignment update: load deviation state: %w", err)
	}
	// Increment deviation count if the emitted substatus differs from
	// the last suggested substatus (and a suggestion was recorded).
	if suggestedLast.String != "" && suggestedLast.String != normalized {
		deviationCount++
	}

	// Build the UPDATE. If the emit is terminal, also set ended_at and end_reason.
	var endReason string
	if isTerminal {
		endReason = normalized
	}

	updateSQL := `
		UPDATE agent_assignments
		SET state = ?, assignment_substatus = ?, blocked_reason = ?,
		    assignment_version = ?, last_emit_at = ?,
		    actual_substatus_last = ?, substatus_deviation_count = ?`
	args := []any{
		newState, normalized, newBlockedReason,
		nextVersion, now,
		normalized, deviationCount,
	}
	if isTerminal {
		updateSQL += `,
		    ended_at = ?, end_reason = ?`
		args = append(args, now, endReason)
	}
	updateSQL += `
		WHERE assignment_id = ? AND assignment_version = ?`
	args = append(args, assignmentID, currentVersion)

	res, execErr := tx.ExecContext(ctx, updateSQL, args...)
	if execErr != nil {
		return nil, fmt.Errorf("emit assignment update: update row: %w", execErr)
	}
	affected, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return nil, fmt.Errorf("emit assignment update: rows affected: %w", rowsErr)
	}
	if affected == 0 {
		return nil, fmt.Errorf("%w: assignment %s changed during update", ErrVersionConflict, assignmentID)
	}
	if affected != 1 {
		return nil, fmt.Errorf("emit assignment update: unexpected rows affected: %d", affected)
	}

	if isTerminal {
		if err := appendAgentEventTx(ctx, tx, assignment.SessionID, assignment.AgentID, "assignment_substatus_updated", map[string]any{
			"assignment_id":  assignment.ID,
			"repo":           assignment.Repo,
			"branch":         assignment.Branch,
			"from_substatus": assignment.Substatus,
			"to_substatus":   normalized,
			"end_reason":     endReason,
		}); err != nil {
			return nil, fmt.Errorf("emit assignment update: append event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("emit assignment update: commit: %w", err)
	}

	assignment.State = newState
	assignment.Substatus = normalized
	assignment.BlockedReason = newBlockedReason
	assignment.Version = nextVersion
	if isTerminal {
		endedAt := now
		assignment.EndedAt = &endedAt
		assignment.EndReason = endReason
	}
	return assignment, nil
}

func loadLiveTaskClaimTx(ctx context.Context, tx *sql.Tx, taskID string) (assignmentID, sessionID string, err error) {
	err = tx.QueryRowContext(ctx, `
		SELECT assignment_id, session_id
		FROM agent_assignments
		WHERE task_id = ? AND ended_at IS NULL
		LIMIT 1`,
		taskID,
	).Scan(&assignmentID, &sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrAgentAssignmentNotFound
	}
	if err != nil {
		return "", "", err
	}
	return assignmentID, sessionID, nil
}

func isLiveTaskConstraintError(err error) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique || sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey
	}
	return strings.Contains(err.Error(), "idx_agent_assignments_live_task_id")
}

func scanAgentSession(row *sql.Row) (*AgentSession, error) {
	var s AgentSession
	var lastProgressAt sql.NullTime
	var lastIOAt sql.NullTime
	var endedAt sql.NullTime

	err := row.Scan(
		&s.SessionID, &s.AgentID, &s.Mode, &s.TmuxSessionName, &s.StartedAt, &s.LastSeenAt, &lastProgressAt, &lastIOAt, &endedAt, &s.EndReason,
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
	if lastIOAt.Valid {
		s.LastIOAt = &lastIOAt.Time
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
		var lastIOAt sql.NullTime
		var endedAt sql.NullTime
		if err := rows.Scan(
			&s.SessionID, &s.AgentID, &s.Mode, &s.TmuxSessionName, &s.StartedAt, &s.LastSeenAt, &lastProgressAt, &lastIOAt, &endedAt, &s.EndReason,
		); err != nil {
			return nil, fmt.Errorf("scan agent session row: %w", err)
		}
		if lastProgressAt.Valid {
			s.LastProgressAt = &lastProgressAt.Time
		}
		if lastIOAt.Valid {
			s.LastIOAt = &lastIOAt.Time
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
	var substatus sql.NullString
	var state sql.NullString
	var blockedReason sql.NullString
	var deliveryState sql.NullString

	err := row.Scan(
		&a.ID, &a.SessionID, &a.AgentID, &a.Repo, &a.Branch, &a.Worktree, &a.TaskID, &state, &blockedReason, &substatus,
		&a.Version, &deliveryState, &a.StartedAt, &endedAt, &a.EndReason, &supersededBy,
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
	if substatus.Valid {
		a.Substatus = substatus.String
	}
	if state.Valid {
		a.State = state.String
	}
	if blockedReason.Valid {
		a.BlockedReason = blockedReason.String
	}
	if deliveryState.Valid {
		a.DeliveryState = deliveryState.String
	}
	return &a, nil
}

func scanAgentAssignmentRow(rows *sql.Rows) (*AgentAssignment, error) {
	var a AgentAssignment
	var endedAt sql.NullTime
	var supersededBy sql.NullString
	var substatus sql.NullString
	var state sql.NullString
	var blockedReason sql.NullString
	var deliveryState sql.NullString

	if err := rows.Scan(
		&a.ID, &a.SessionID, &a.AgentID, &a.Repo, &a.Branch, &a.Worktree, &a.TaskID, &state, &blockedReason, &substatus,
		&a.Version, &deliveryState, &a.StartedAt, &endedAt, &a.EndReason, &supersededBy,
	); err != nil {
		return nil, fmt.Errorf("scan agent assignment row: %w", err)
	}
	if endedAt.Valid {
		a.EndedAt = &endedAt.Time
	}
	if supersededBy.Valid {
		a.SupersededBy = &supersededBy.String
	}
	if substatus.Valid {
		a.Substatus = substatus.String
	}
	if state.Valid {
		a.State = state.String
	}
	if blockedReason.Valid {
		a.BlockedReason = blockedReason.String
	}
	if deliveryState.Valid {
		a.DeliveryState = deliveryState.String
	}
	return &a, nil
}

func scanAgentAssignmentContextRow(row *sql.Row) (*agentAssignmentContext, error) {
	var (
		contextRow              agentAssignmentContext
		sessionLastProgressAt   sql.NullTime
		sessionEndedAt          sql.NullTime
		assignmentEndedAt       sql.NullTime
		assignmentSupersededBy  sql.NullString
		assignmentSubstatus     sql.NullString
		assignmentState         sql.NullString
		assignmentBlockedReason sql.NullString
	)
	err := row.Scan(
		&contextRow.Session.SessionID,
		&contextRow.Session.AgentID,
		&contextRow.Session.Mode,
		&contextRow.Session.StartedAt,
		&contextRow.Session.LastSeenAt,
		&sessionLastProgressAt,
		&sessionEndedAt,
		&contextRow.Session.EndReason,
		&contextRow.Assignment.ID,
		&contextRow.Assignment.SessionID,
		&contextRow.Assignment.AgentID,
		&contextRow.Assignment.Repo,
		&contextRow.Assignment.Branch,
		&contextRow.Assignment.Worktree,
		&contextRow.Assignment.TaskID,
		&assignmentState,
		&assignmentBlockedReason,
		&assignmentSubstatus,
		&contextRow.Assignment.Version,
		&contextRow.Assignment.StartedAt,
		&assignmentEndedAt,
		&contextRow.Assignment.EndReason,
		&assignmentSupersededBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAgentAssignmentNotFound
		}
		return nil, fmt.Errorf("scan agent assignment context: %w", err)
	}
	if sessionLastProgressAt.Valid {
		contextRow.Session.LastProgressAt = &sessionLastProgressAt.Time
	}
	if sessionEndedAt.Valid {
		contextRow.Session.EndedAt = &sessionEndedAt.Time
	}
	if assignmentEndedAt.Valid {
		contextRow.Assignment.EndedAt = &assignmentEndedAt.Time
	}
	if assignmentSupersededBy.Valid {
		contextRow.Assignment.SupersededBy = &assignmentSupersededBy.String
	}
	if assignmentSubstatus.Valid {
		contextRow.Assignment.Substatus = assignmentSubstatus.String
	}
	if assignmentState.Valid {
		contextRow.Assignment.State = assignmentState.String
	}
	if assignmentBlockedReason.Valid {
		contextRow.Assignment.BlockedReason = assignmentBlockedReason.String
	}
	return &contextRow, nil
}

func scanAgentAssignmentContextRows(rows *sql.Rows) (*agentAssignmentContext, error) {
	var (
		contextRow              agentAssignmentContext
		sessionLastProgressAt   sql.NullTime
		sessionEndedAt          sql.NullTime
		assignmentEndedAt       sql.NullTime
		assignmentSupersededBy  sql.NullString
		assignmentSubstatus     sql.NullString
		assignmentState         sql.NullString
		assignmentBlockedReason sql.NullString
	)
	if err := rows.Scan(
		&contextRow.Session.SessionID,
		&contextRow.Session.AgentID,
		&contextRow.Session.Mode,
		&contextRow.Session.StartedAt,
		&contextRow.Session.LastSeenAt,
		&sessionLastProgressAt,
		&sessionEndedAt,
		&contextRow.Session.EndReason,
		&contextRow.Assignment.ID,
		&contextRow.Assignment.SessionID,
		&contextRow.Assignment.AgentID,
		&contextRow.Assignment.Repo,
		&contextRow.Assignment.Branch,
		&contextRow.Assignment.Worktree,
		&contextRow.Assignment.TaskID,
		&assignmentState,
		&assignmentBlockedReason,
		&assignmentSubstatus,
		&contextRow.Assignment.Version,
		&contextRow.Assignment.StartedAt,
		&assignmentEndedAt,
		&contextRow.Assignment.EndReason,
		&assignmentSupersededBy,
	); err != nil {
		return nil, fmt.Errorf("scan agent assignment context row: %w", err)
	}
	if sessionLastProgressAt.Valid {
		contextRow.Session.LastProgressAt = &sessionLastProgressAt.Time
	}
	if sessionEndedAt.Valid {
		contextRow.Session.EndedAt = &sessionEndedAt.Time
	}
	if assignmentEndedAt.Valid {
		contextRow.Assignment.EndedAt = &assignmentEndedAt.Time
	}
	if assignmentSupersededBy.Valid {
		contextRow.Assignment.SupersededBy = &assignmentSupersededBy.String
	}
	if assignmentSubstatus.Valid {
		contextRow.Assignment.Substatus = assignmentSubstatus.String
	}
	if assignmentState.Valid {
		contextRow.Assignment.State = assignmentState.String
	}
	if assignmentBlockedReason.Valid {
		contextRow.Assignment.BlockedReason = assignmentBlockedReason.String
	}
	return &contextRow, nil
}
