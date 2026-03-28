package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/codero/codero/internal/state"
)

func openSessionTestDB(t *testing.T) *state.DB {
	t.Helper()

	db, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ---------------------------------------------------------------------------
// Confirm
// ---------------------------------------------------------------------------

func TestConfirm_HappyPath(t *testing.T) {
	db := openSessionTestDB(t)
	ctx := context.Background()
	store := NewStore(db)

	_, err := store.Register(ctx, "sess-confirm", "agent-1", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := store.Confirm(ctx, "sess-confirm", "agent-1"); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
}

func TestConfirm_MissingSessionID(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewStore(db)
	if err := store.Confirm(context.Background(), "", "agent-1"); err != ErrMissingSessionID {
		t.Fatalf("got %v, want ErrMissingSessionID", err)
	}
}

func TestConfirm_MissingAgentID(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewStore(db)
	if err := store.Confirm(context.Background(), "sess-1", ""); err != ErrMissingAgentID {
		t.Fatalf("got %v, want ErrMissingAgentID", err)
	}
}

func TestConfirm_NotFound(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewStore(db)
	if err := store.Confirm(context.Background(), "no-such-session", "agent-1"); err != ErrSessionNotFound {
		t.Fatalf("got %v, want ErrSessionNotFound", err)
	}
}

func TestConfirm_AgentMismatch(t *testing.T) {
	db := openSessionTestDB(t)
	ctx := context.Background()
	store := NewStore(db)

	_, err := store.Register(ctx, "sess-mismatch", "agent-A", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := store.Confirm(ctx, "sess-mismatch", "agent-B"); err != ErrSessionMismatch {
		t.Fatalf("got %v, want ErrSessionMismatch", err)
	}
}

// ---------------------------------------------------------------------------
// Finalize
// ---------------------------------------------------------------------------

func TestFinalize_HappyPath(t *testing.T) {
	db := openSessionTestDB(t)
	ctx := context.Background()
	store := NewStore(db)

	_, err := store.Register(ctx, "sess-finalize", "agent-fin", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	comp := Completion{
		TaskID:    "TASK-99",
		Status:    "completed",
		Substatus: "terminal_finished",
		Summary:   "all done",
	}
	if err := store.Finalize(ctx, "sess-finalize", "agent-fin", comp); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Verify session was ended
	var endedAt *string
	if err := db.Unwrap().QueryRow(
		`SELECT ended_at FROM agent_sessions WHERE session_id = ?`, "sess-finalize",
	).Scan(&endedAt); err != nil {
		t.Fatalf("read ended_at: %v", err)
	}
	if endedAt == nil {
		t.Fatal("session ended_at should be set after Finalize")
	}
}

func TestFinalize_SessionNotFound(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewStore(db)
	comp := Completion{Status: "completed", Substatus: "terminal_finished"}
	err := store.Finalize(context.Background(), "no-such-session", "agent-1", comp)
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
}

func TestFinalize_AgentMismatch(t *testing.T) {
	db := openSessionTestDB(t)
	ctx := context.Background()
	store := NewStore(db)

	_, err := store.Register(ctx, "sess-fin-mismatch", "agent-X", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	comp := Completion{Status: "completed", Substatus: "terminal_finished"}
	err = store.Finalize(ctx, "sess-fin-mismatch", "agent-Y", comp)
	if err == nil {
		t.Fatal("expected error for agent mismatch")
	}
}

func TestFinalize_MissingSessionID(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewStore(db)
	err := store.Finalize(context.Background(), "", "agent-1", Completion{Status: "completed"})
	if err != ErrMissingSessionID {
		t.Fatalf("got %v, want ErrMissingSessionID", err)
	}
}

func TestFinalize_MissingAgentID(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewStore(db)
	err := store.Finalize(context.Background(), "sess-1", "", Completion{Status: "completed"})
	if err != ErrMissingAgentID {
		t.Fatalf("got %v, want ErrMissingAgentID", err)
	}
}

func TestFinalize_MissingStatus(t *testing.T) {
	db := openSessionTestDB(t)
	store := NewStore(db)
	err := store.Finalize(context.Background(), "sess-1", "agent-1", Completion{})
	if err != ErrMissingStatus {
		t.Fatalf("got %v, want ErrMissingStatus", err)
	}
}

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

func TestHeartbeatRefreshesOwnerAgent(t *testing.T) {
	db := openSessionTestDB(t)
	ctx := context.Background()

	const (
		sessionID = "sess-heartbeat-owner-agent"
		agentID   = "agent-heartbeat"
		repo      = "acme/api"
		branch    = "feature/heartbeat-owner-agent"
	)

	if _, err := db.Unwrap().Exec(
		`INSERT INTO branch_states (id, repo, branch, state)
		 VALUES (?, ?, ?, ?)`,
		"branch-heartbeat", repo, branch, string(state.StateSubmitted),
	); err != nil {
		t.Fatalf("seed branch state: %v", err)
	}

	store := NewStore(db)
	secret, err := store.Register(ctx, sessionID, agentID, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if secret == "" {
		t.Fatal("Register returned empty heartbeat secret")
	}

	worktreePath := t.TempDir()
	if err := store.AttachAssignment(ctx, sessionID, agentID, repo, branch, worktreePath, "", "TASK-1", ""); err != nil {
		t.Fatalf("AttachAssignment: %v", err)
	}

	if _, err := db.Unwrap().Exec(
		`UPDATE branch_states SET owner_agent = '' WHERE repo = ? AND branch = ?`,
		repo, branch,
	); err != nil {
		t.Fatalf("clear owner_agent: %v", err)
	}

	if err := store.Heartbeat(ctx, sessionID, secret, false); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	var ownerAgent string
	if err := db.Unwrap().QueryRow(
		`SELECT owner_agent FROM branch_states WHERE repo = ? AND branch = ?`,
		repo, branch,
	).Scan(&ownerAgent); err != nil {
		t.Fatalf("read owner_agent: %v", err)
	}
	if ownerAgent != agentID {
		t.Fatalf("owner_agent: got %q, want %q", ownerAgent, agentID)
	}
}
