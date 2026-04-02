package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// TailDir returns the directory used for per-session output tail files.
// Override via CODERO_TAIL_DIR env var; defaults to codero-tails under os.TempDir().
func TailDir() string {
	if d := os.Getenv("CODERO_TAIL_DIR"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "codero-tails")
}

// TailPath returns the tail file path for a session.
// Returns an error if sessionID contains path traversal sequences.
func TailPath(sessionID string) (string, error) {
	if strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, "/\\") {
		return "", fmt.Errorf("invalid session ID: path traversal detected")
	}
	base := TailDir()
	p := filepath.Join(base, sessionID+".log")
	// Use filepath.Rel to properly validate path doesn't escape base
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return "", fmt.Errorf("invalid session ID: %w", err)
	}
	// Check if the relative path tries to escape (starts with ..)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid session ID: path escapes tail directory")
	}
	return p, nil
}

var (
	ErrMissingSessionID  = errors.New("session_id is required")
	ErrInvalidSessionID  = errors.New("session_id contains invalid characters")
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
func (s *Store) Register(ctx context.Context, sessionID, agentID, mode string) (string, error) {
	return s.RegisterWithTmux(ctx, sessionID, agentID, mode, "")
}

// RegisterWithTmux records a session with an associated tmux session name (SL-9, SL-11).
func (s *Store) RegisterWithTmux(ctx context.Context, sessionID, agentID, mode, tmuxSessionName string) (string, error) {
	if sessionID == "" {
		return "", ErrMissingSessionID
	}
	if agentID == "" {
		return "", ErrMissingAgentID
	}
	secret, err := state.RegisterAgentSessionWithSecret(ctx, s.db, sessionID, agentID, mode, tmuxSessionName)
	if err != nil {
		return "", err
	}
	return secret, nil
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
// It also refreshes branch owner_agent so stale rows recover on heartbeat.
func (s *Store) Heartbeat(ctx context.Context, sessionID, heartbeatSecret string, markProgress bool) error {
	if sessionID == "" {
		return ErrMissingSessionID
	}
	if err := state.ValidateHeartbeatSecret(ctx, s.db, sessionID, heartbeatSecret); err != nil {
		return err
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
		if err := state.UpdateOwnerAgent(ctx, s.db, active.Repo, active.Branch, active.AgentID); err != nil {
			loglib.Warn("session: failed to record owner agent",
				loglib.FieldComponent, "session",
				loglib.FieldSession, sessionID,
				loglib.FieldRepo, active.Repo,
				loglib.FieldBranch, active.Branch,
				"error", err,
			)
		}
	}
	return nil
}

// HeartbeatWithStatus sends a heartbeat and updates inferred_status.
// Used by CLI --status flag for direct-DB fallback path.
func (s *Store) HeartbeatWithStatus(ctx context.Context, sessionID, heartbeatSecret string, markProgress bool, inferredStatus string) error {
	if err := s.Heartbeat(ctx, sessionID, heartbeatSecret, markProgress); err != nil {
		return err
	}
	if inferredStatus != "" {
		normalized := state.NormalizeStatus(inferredStatus)
		if normalized != "" {
			if err := state.UpdateInferredStatus(ctx, s.db, sessionID, normalized); err != nil {
				return fmt.Errorf("update inferred status: %w", err)
			}
		}
	}
	return nil
}

// ListActiveSessions returns all active (non-ended) sessions.
func (s *Store) ListActiveSessions(ctx context.Context) ([]state.AgentSession, error) {
	return state.ListActiveAgentSessions(ctx, s.db)
}

// SessionInfo holds session state plus active assignment if any.
type SessionInfo struct {
	Session    state.AgentSession
	Assignment *state.AgentAssignment
}

// Get retrieves a session and its active assignment (if any) in one call.
// Returns ErrSessionNotFound if the session doesn't exist.
// Returns ErrSessionMismatch if agentID is non-empty and doesn't match.
func (s *Store) Get(ctx context.Context, sessionID, agentID string) (*SessionInfo, error) {
	if sessionID == "" {
		return nil, ErrMissingSessionID
	}

	session, err := state.GetAgentSession(ctx, s.db, sessionID)
	if err != nil {
		if errors.Is(err, state.ErrAgentSessionNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("Store.Get: %w", err)
	}

	if agentID != "" && session.AgentID != agentID {
		return nil, ErrSessionMismatch
	}

	assignment, err := state.GetActiveAgentAssignment(ctx, s.db, sessionID)
	if err != nil && !errors.Is(err, state.ErrAgentAssignmentNotFound) {
		return nil, fmt.Errorf("Store.Get: get assignment: %w", err)
	}

	info := &SessionInfo{
		Session: *session,
	}
	if err == nil {
		info.Assignment = assignment
	}

	return info, nil
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

	// Check branch exists before creating session/assignment rows to avoid
	// orphan writes when the branch is not in the state store.
	var branchExists int
	if err := s.db.Unwrap().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM branch_states WHERE repo = ? AND branch = ?`,
		repo, branch,
	).Scan(&branchExists); err != nil {
		return fmt.Errorf("attach assignment: check branch: %w", err)
	}
	if branchExists == 0 {
		return state.ErrBranchNotFound
	}

	if err := state.RegisterAgentSession(ctx, s.db, sessionID, agentID, mode, ""); err != nil {
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
	if _, err := s.db.Unwrap().ExecContext(ctx, `
		UPDATE branch_states
		SET owner_session_id = ?, owner_session_last_seen = datetime('now'),
		    owner_agent = ?, updated_at = datetime('now')
		WHERE repo = ? AND branch = ?`,
		sessionID, agentID, repo, branch,
	); err != nil {
		return fmt.Errorf("attach assignment: sync branch state: %w", err)
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
	err := state.FinalizeAgentSession(ctx, s.db, sessionID, agentID, state.AgentSessionCompletion{
		TaskID:     completion.TaskID,
		Status:     completion.Status,
		Substatus:  completion.Substatus,
		Summary:    completion.Summary,
		Tests:      completion.Tests,
		FinishedAt: completion.FinishedAt,
	})
	if err != nil {
		if errors.Is(err, state.ErrAgentSessionNotFound) || errors.Is(err, state.ErrAgentSessionAlreadyEnded) {
			return ErrSessionNotFound
		}
		if errors.Is(err, state.ErrAgentSessionAgentMismatch) {
			return ErrSessionMismatch
		}
		return fmt.Errorf("Store.Finalize: %w", err)
	}
	return nil
}
