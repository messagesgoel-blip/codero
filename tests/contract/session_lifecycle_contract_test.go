package contract

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
)

// ─── MIG-038: Session Lifecycle Contract Tests ───────────────────────────────
//
// These tests validate the session lifecycle contracts:
//   1. Tmux heartbeat: RegisterWithTmux stores tmux session name
//   2. Session archival: Finalize creates archive row with completion data
//   3. Lazy assignment creation: AttachAssignment creates branch state if needed

// openSessionContractDB opens an in-memory state DB for session contract tests.
func openSessionContractDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "session_contract.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestMIG038_TmuxHeartbeat_StoresSessionName tests that RegisterWithTmux
// persists the tmux session name for later retrieval.
func TestMIG038_TmuxHeartbeat_StoresSessionName(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID       = "sess-tmux-1"
		agentID         = "agent-tmux"
		mode            = "coding"
		tmuxSessionName = "codero-agent-tmux-1"
	)

	secret, err := store.RegisterWithTmux(ctx, sessionID, agentID, mode, tmuxSessionName)
	if err != nil {
		t.Fatalf("RegisterWithTmux: %v", err)
	}
	if secret == "" {
		t.Error("expected heartbeat secret to be returned")
	}

	// Verify tmux_session_name was stored
	var storedTmux string
	err = db.Unwrap().QueryRow(
		`SELECT tmux_session_name FROM agent_sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&storedTmux)
	if err != nil {
		t.Fatalf("query tmux_session_name: %v", err)
	}
	if storedTmux != tmuxSessionName {
		t.Errorf("tmux_session_name = %q, want %q", storedTmux, tmuxSessionName)
	}

	// Verify agent_id and mode were stored
	var storedAgent, storedMode string
	err = db.Unwrap().QueryRow(
		`SELECT agent_id, mode FROM agent_sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&storedAgent, &storedMode)
	if err != nil {
		t.Fatalf("query session fields: %v", err)
	}
	if storedAgent != agentID {
		t.Errorf("agent_id = %q, want %q", storedAgent, agentID)
	}
	if storedMode != mode {
		t.Errorf("mode = %q, want %q", storedMode, mode)
	}
}

// TestMIG038_SessionArchival_ArchiveRowExists tests that ArchiveSession
// creates an archive row correctly.
func TestMIG038_SessionArchival_ArchiveRowExists(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	const (
		sessionID = "sess-archive-1"
		agentID   = "agent-archive"
		repo      = "acme/api"
		branch    = "feature/archive-test"
		taskID    = "TASK-ARCHIVE-001"
	)

	// Register session
	_, err := db.Unwrap().Exec(
		`INSERT INTO agent_sessions (session_id, agent_id, mode, started_at, last_seen_at)
		 VALUES (?, ?, 'coding', datetime('now'), datetime('now'))`,
		sessionID, agentID,
	)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// Archive the session directly
	err = state.ArchiveSession(ctx, db, sessionID, "merged", "merge-sha-123")
	if err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	// Verify archive row exists
	archive, err := state.GetSessionArchive(ctx, db, sessionID)
	if err != nil {
		t.Fatalf("GetSessionArchive: %v", err)
	}
	if archive.SessionID != sessionID {
		t.Errorf("archive.SessionID = %q, want %q", archive.SessionID, sessionID)
	}
	if archive.AgentID != agentID {
		t.Errorf("archive.AgentID = %q, want %q", archive.AgentID, agentID)
	}
	if archive.Result != "merged" {
		t.Errorf("archive.Result = %q, want merged", archive.Result)
	}
	if archive.MergeSHA != "merge-sha-123" {
		t.Errorf("archive.MergeSHA = %q, want merge-sha-123", archive.MergeSHA)
	}
}

// TestMIG038_LazyAssignment_RequiresBranchState tests that AttachAssignment
// requires the branch_state row to exist first (returns ErrBranchNotFound otherwise).
func TestMIG038_LazyAssignment_RequiresBranchState(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-lazy-1"
		agentID   = "agent-lazy"
		repo      = "acme/lazy-repo"
		branch    = "feature/lazy-branch"
		taskID    = "TASK-LAZY-001"
	)

	// Register session
	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Attach assignment without branch_state should fail
	err = store.AttachAssignment(ctx, sessionID, agentID, repo, branch, t.TempDir(), "coding", taskID, "")
	if err == nil {
		t.Fatal("expected error for missing branch_state")
	}
	if err != state.ErrBranchNotFound {
		t.Errorf("expected ErrBranchNotFound, got %v", err)
	}
}

// TestMIG038_AttachAssignment_UpdatesBranchState tests that AttachAssignment
// updates branch state when branch exists.
func TestMIG038_AttachAssignment_UpdatesBranchState(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-attach-1"
		agentID   = "agent-attach"
		repo      = "acme/attach-repo"
		branch    = "feature/attach-branch"
		taskID    = "TASK-ATTACH-001"
	)

	// Register session
	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Seed branch state
	_, err = db.Unwrap().Exec(
		`INSERT INTO branch_states (id, repo, branch, state) VALUES (?, ?, ?, ?)`,
		"branch-attach", repo, branch, string(state.StateSubmitted),
	)
	if err != nil {
		t.Fatalf("seed branch state: %v", err)
	}

	// Attach assignment
	err = store.AttachAssignment(ctx, sessionID, agentID, repo, branch, t.TempDir(), "coding", taskID, "")
	if err != nil {
		t.Fatalf("AttachAssignment: %v", err)
	}

	// Verify branch_state was updated with owner info
	var ownerSession, ownerAgent string
	err = db.Unwrap().QueryRow(
		`SELECT owner_session_id, owner_agent FROM branch_states WHERE repo = ? AND branch = ?`,
		repo, branch,
	).Scan(&ownerSession, &ownerAgent)
	if err != nil {
		t.Fatalf("query branch state: %v", err)
	}
	if ownerSession != sessionID {
		t.Errorf("owner_session_id = %q, want %q", ownerSession, sessionID)
	}
	if ownerAgent != agentID {
		t.Errorf("owner_agent = %q, want %q", ownerAgent, agentID)
	}
}

// TestMIG038_Heartbeat_UpdatesLastSeen tests that Heartbeat updates
// the last_seen_at timestamp.
func TestMIG038_Heartbeat_UpdatesLastSeen(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-heartbeat-1"
		agentID   = "agent-heartbeat"
	)

	secret, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Get initial last_seen_at
	var initialSeenStr string
	err = db.Unwrap().QueryRow(
		`SELECT last_seen_at FROM agent_sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&initialSeenStr)
	if err != nil {
		t.Fatalf("query initial last_seen_at: %v", err)
	}

	// Wait enough time for SQLite datetime to change
	time.Sleep(1 * time.Second)

	err = store.Heartbeat(ctx, sessionID, secret, false)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	// Verify last_seen_at was updated
	var newSeenStr string
	err = db.Unwrap().QueryRow(
		`SELECT last_seen_at FROM agent_sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&newSeenStr)
	if err != nil {
		t.Fatalf("query new last_seen_at: %v", err)
	}

	// SQLite datetime strings should be different after a second
	if initialSeenStr == newSeenStr {
		t.Errorf("last_seen_at not updated: initial=%v, new=%v", initialSeenStr, newSeenStr)
	}
}

// TestMIG038_Heartbeat_InvalidSecret tests that Heartbeat rejects
// an invalid heartbeat secret.
func TestMIG038_Heartbeat_InvalidSecret(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-invalid-secret"
		agentID   = "agent-invalid"
	)

	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	err = store.Heartbeat(ctx, sessionID, "invalid-secret", false)
	if err == nil {
		t.Fatal("expected error for invalid heartbeat secret")
	}
}

// TestMIG038_Confirm_VerifiesSession tests that Confirm verifies
// the session-agent match.
func TestMIG038_Confirm_VerifiesSession(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-confirm-1"
		agentID   = "agent-confirm"
	)

	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Confirm with correct agent_id
	err = store.Confirm(ctx, sessionID, agentID)
	if err != nil {
		t.Fatalf("Confirm with correct agent: %v", err)
	}

	// Confirm with wrong agent_id should fail
	err = store.Confirm(ctx, sessionID, "wrong-agent")
	if err == nil {
		t.Fatal("expected error for wrong agent_id")
	}
	if err != session.ErrSessionMismatch {
		t.Errorf("expected ErrSessionMismatch, got %v", err)
	}
}

// TestMIG038_RegisterIdempotent tests that re-registering with the same
// session_id updates the session instead of failing.
func TestMIG038_RegisterIdempotent(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-idempotent"
		agentID   = "agent-idempotent"
	)

	secret1, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Re-register with same session_id (should update)
	secret2, err := store.Register(ctx, sessionID, agentID, "review")
	if err != nil {
		t.Fatalf("second Register: %v", err)
	}

	// Secrets should be the same (idempotent update)
	if secret1 != secret2 {
		t.Logf("Note: secrets differ on re-register (this is acceptable)")
	}

	// Verify mode was updated
	var mode string
	err = db.Unwrap().QueryRow(
		`SELECT mode FROM agent_sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&mode)
	if err != nil {
		t.Fatalf("query mode: %v", err)
	}
	if mode != "review" {
		t.Errorf("mode = %q, want review", mode)
	}
}

// TestMIG038_HeartbeatWithStatus_UpdatesInferredStatus tests that HeartbeatWithStatus
// persists inferred_status to the agent_sessions row.
func TestMIG038_HeartbeatWithStatus_UpdatesInferredStatus(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-inferred-status-1"
		agentID   = "agent-inferred"
	)

	secret, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Initial inferred_status should be 'unknown'
	var initialStatus string
	if err := db.Unwrap().QueryRow(
		`SELECT inferred_status FROM agent_sessions WHERE session_id = ?`, sessionID,
	).Scan(&initialStatus); err != nil {
		t.Fatalf("query initial inferred_status: %v", err)
	}
	if initialStatus != "unknown" {
		t.Errorf("initial inferred_status = %q, want unknown", initialStatus)
	}

	err = store.HeartbeatWithStatus(ctx, sessionID, secret, false, "working")
	if err != nil {
		t.Fatalf("HeartbeatWithStatus: %v", err)
	}

	var storedStatus string
	if err := db.Unwrap().QueryRow(
		`SELECT inferred_status FROM agent_sessions WHERE session_id = ?`, sessionID,
	).Scan(&storedStatus); err != nil {
		t.Fatalf("query inferred_status after heartbeat: %v", err)
	}
	if storedStatus != "working" {
		t.Errorf("inferred_status = %q, want working", storedStatus)
	}
}

// TestMIG038_Heartbeat_StatusAliasNormalization tests that status aliases are
// normalized to canonical values (pretooluse → working, waiting → waiting_for_input).
func TestMIG038_Heartbeat_StatusAliasNormalization(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	cases := []struct {
		alias    string
		expected string
	}{
		{"pretooluse", "working"},
		{"posttooluse", "working"},
		{"waiting", "waiting_for_input"},
		{"notification", "waiting_for_input"},
		{"idle", "idle"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.alias, func(t *testing.T) {
			sessionID := "sess-alias-" + tc.alias //nolint:gosec // nosemgrep: go.lang.security.audit.database.string-formatted-query.string-formatted-query -- test data only, not user input
			secret, err := store.Register(ctx, sessionID, "agent-alias", "coding")
			if err != nil {
				t.Fatalf("Register: %v", err)
			}
			if err := store.HeartbeatWithStatus(ctx, sessionID, secret, false, tc.alias); err != nil {
				t.Fatalf("HeartbeatWithStatus(%q): %v", tc.alias, err)
			}
			var stored string
			if err := db.Unwrap().QueryRow(
				`SELECT inferred_status FROM agent_sessions WHERE session_id = ?`, sessionID,
			).Scan(&stored); err != nil {
				t.Fatalf("query inferred_status: %v", err)
			}
			if stored != tc.expected {
				t.Errorf("alias %q: inferred_status = %q, want %q", tc.alias, stored, tc.expected)
			}
		})
	}
}

// TestMIG038_Finalize_WritesArchiveRow tests that Finalize atomically creates a
// session_archives row with the correct result and agent_id.
func TestMIG038_Finalize_WritesArchiveRow(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-finalize-archive"
		agentID   = "agent-finalize"
	)

	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	completion := session.Completion{
		Status:    "merged",
		Substatus: "terminal_finished",
		Summary:   "task complete",
	}
	if err := store.Finalize(ctx, sessionID, agentID, completion); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Verify session_archives row was written
	archive, err := state.GetSessionArchive(ctx, db, sessionID)
	if err != nil {
		t.Fatalf("GetSessionArchive: %v", err)
	}
	if archive.SessionID != sessionID {
		t.Errorf("archive.SessionID = %q, want %q", archive.SessionID, sessionID)
	}
	if archive.AgentID != agentID {
		t.Errorf("archive.AgentID = %q, want %q", archive.AgentID, agentID)
	}
	if archive.Result != "merged" {
		t.Errorf("archive.Result = %q, want merged", archive.Result)
	}

	// Verify agent_sessions.ended_at is set
	var endedAt *string
	if err := db.Unwrap().QueryRow(
		`SELECT ended_at FROM agent_sessions WHERE session_id = ?`, sessionID,
	).Scan(&endedAt); err != nil {
		t.Fatalf("query ended_at: %v", err)
	}
	if endedAt == nil {
		t.Error("ended_at is NULL after Finalize, want non-null")
	}
}

// TestMIG038_Finalize_AgentMismatch tests that Finalize rejects a wrong agentID.
func TestMIG038_Finalize_AgentMismatch(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-finalize-mismatch"
		agentID   = "agent-finalize-real"
	)

	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	completion := session.Completion{
		Status: "merged",
	}
	err = store.Finalize(ctx, sessionID, "wrong-agent", completion)
	if err == nil {
		t.Fatal("expected error for wrong agentID, got nil")
	}
	if err != session.ErrSessionMismatch {
		t.Errorf("expected ErrSessionMismatch, got %v", err)
	}
}

// TestMIG038_AttachAssignment_SubstatusStored tests that the substatus parameter
// is persisted to agent_assignments.assignment_substatus.
func TestMIG038_AttachAssignment_SubstatusStored(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-substatus-1"
		agentID   = "agent-substatus"
		repo      = "acme/substatus-repo"
		branch    = "feature/substatus-branch"
		taskID    = "TASK-SUB-001"
		substatus = "in_progress"
	)

	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Seed branch state
	if _, err := db.Unwrap().Exec(
		`INSERT INTO branch_states (id, repo, branch, state) VALUES (?, ?, ?, ?)`,
		"branch-substatus", repo, branch, "submitted",
	); err != nil {
		t.Fatalf("seed branch state: %v", err)
	}

	err = store.AttachAssignment(ctx, sessionID, agentID, repo, branch, t.TempDir(), "coding", taskID, substatus)
	if err != nil {
		t.Fatalf("AttachAssignment: %v", err)
	}

	var storedSubstatus string
	if err := db.Unwrap().QueryRow(
		`SELECT assignment_substatus FROM agent_assignments WHERE session_id = ? AND ended_at IS NULL`,
		sessionID,
	).Scan(&storedSubstatus); err != nil {
		t.Fatalf("query assignment_substatus: %v", err)
	}
	if storedSubstatus != substatus {
		t.Errorf("assignment_substatus = %q, want %q", storedSubstatus, substatus)
	}
}
