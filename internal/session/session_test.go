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
		"branch-heartbeat", repo, branch, string(state.StateCoding),
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
