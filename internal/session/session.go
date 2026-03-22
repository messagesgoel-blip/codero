package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/codero/codero/internal/state"
)

var (
	ErrMissingSessionID  = errors.New("session_id is required")
	ErrMissingAgentID    = errors.New("agent_id is required")
	ErrMissingAssignment = errors.New("repo and branch are required to attach assignment")
	ErrMissingStatus     = errors.New("status is required")
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionMismatch   = errors.New("session agent mismatch")
)

type Completion struct {
	TaskID     string
	Status     string
	Substatus  string
	Summary    string
	Tests      []string
	FinishedAt time.Time
}

// Store persists agent session registration and assignment metadata.
// It uses the durable state DB owned by internal/state.
type Store struct {
	db *state.DB
}

// NewStore constructs a session Store for the given state DB.
func NewStore(db *state.DB) *Store {
	return &Store{db: db}
}

// Register records a session with session_id + agent_id only.
// If the session already exists, it refreshes agent_id, mode, and last_seen.
func (s *Store) Register(ctx context.Context, sessionID, agentID, mode string) error {
	if sessionID == "" {
		return ErrMissingSessionID
	}
	if agentID == "" {
		return ErrMissingAgentID
	}
	if err := state.RegisterAgentSession(ctx, s.db, sessionID, agentID, mode); err != nil {
		return err
	}
	return nil
}

// Confirm verifies that Codero has the same live session identity the agent was
// injected with at startup.
func (s *Store) Confirm(ctx context.Context, sessionID, agentID string) error {
	if sessionID == "" {
		return ErrMissingSessionID
	}
	if agentID == "" {
		return ErrMissingAgentID
	}
	if err := state.ConfirmAgentSession(ctx, s.db, sessionID, agentID); err != nil {
		if errors.Is(err, state.ErrAgentSessionNotFound) || errors.Is(err, state.ErrAgentSessionAlreadyEnded) {
			return ErrSessionNotFound
		}
		if errors.Is(err, state.ErrAgentSessionAgentMismatch) {
			return ErrSessionMismatch
		}
		return fmt.Errorf("Store.Confirm: confirm agent session %s for agent %s: %w", sessionID, agentID, err)
	}
	return nil
}

// Heartbeat updates last_seen for the session and any attached branch assignment.
// When markProgress is true, it also refreshes the durable progress timestamp.
func (s *Store) Heartbeat(ctx context.Context, sessionID string, markProgress bool) error {
	if sessionID == "" {
		return ErrMissingSessionID
	}
	if err := state.UpdateAgentSessionHeartbeat(ctx, s.db, sessionID, markProgress); err != nil {
		return err
	}

	active, err := state.GetActiveAgentAssignment(ctx, s.db, sessionID)
	if err != nil {
		if errors.Is(err, state.ErrAgentAssignmentNotFound) {
			return nil
		}
		return err
	}
	if active.Repo != "" && active.Branch != "" {
		if err := state.UpdateSessionHeartbeat(s.db, active.Repo, active.Branch); err != nil {
			return err
		}
	}
	return nil
}

// AttachAssignment fills in repo/branch/worktree/task_id when a task is claimed or assigned.
// It also updates the branch session tracking fields for dashboard visibility.
func (s *Store) AttachAssignment(
	ctx context.Context,
	sessionID, agentID, repo, branch, worktree, mode, taskID, substatus string,
) error {
	if sessionID == "" {
		return ErrMissingSessionID
	}
	if agentID == "" {
		return ErrMissingAgentID
	}
	if repo == "" || branch == "" {
		return ErrMissingAssignment
	}
	if err := state.RegisterAgentSession(ctx, s.db, sessionID, agentID, mode); err != nil {
		return err
	}

	assignment := &state.AgentAssignment{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		AgentID:   agentID,
		Repo:      repo,
		Branch:    branch,
		Worktree:  worktree,
		TaskID:    taskID,
		Substatus: substatus,
	}
	if err := state.AttachAgentAssignment(ctx, s.db, assignment); err != nil {
		return err
	}
	res, err := s.db.Unwrap().ExecContext(ctx, `
		UPDATE branch_states
		SET owner_session_id = ?, owner_session_last_seen = datetime('now'),
		    owner_agent = ?, updated_at = datetime('now')
		WHERE repo = ? AND branch = ?`,
		sessionID, agentID, repo, branch)
	if err != nil {
		return fmt.Errorf("attach assignment: sync branch state: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return state.ErrBranchNotFound
	}
	return nil
}

// Finalize marks a session complete using a machine-readable completion record.
func (s *Store) Finalize(ctx context.Context, sessionID, agentID string, completion Completion) error {
	if sessionID == "" {
		return ErrMissingSessionID
	}
	if agentID == "" {
		return ErrMissingAgentID
	}
	if completion.Status == "" {
		return ErrMissingStatus
	}
	return state.FinalizeAgentSession(ctx, s.db, sessionID, agentID, state.AgentSessionCompletion{
		TaskID:     completion.TaskID,
		Status:     completion.Status,
		Substatus:  completion.Substatus,
		Summary:    completion.Summary,
		Tests:      completion.Tests,
		FinishedAt: completion.FinishedAt,
	})
}
