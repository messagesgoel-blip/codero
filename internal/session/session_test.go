package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/codero/codero/internal/state"
)

func openTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestStoreHeartbeat_UpdatesRule004ForActiveAssignment(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	worktree := filepath.Join(t.TempDir(), "wt")

	if err := store.Register(ctx, "sess-heartbeat", "agent-1", "cli"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := state.AttachAgentAssignment(ctx, db, &state.AgentAssignment{
		ID:        "assign-heartbeat",
		SessionID: "sess-heartbeat",
		AgentID:   "agent-1",
		Repo:      "acme/api",
		Branch:    "feat/rule-004",
		Worktree:  worktree,
		TaskID:    "TASK-1",
	}); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}

	if err := store.Heartbeat(ctx, "sess-heartbeat", true); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	var (
		result          string
		violationRaised int
		detail          string
		resolvedAt      any
	)
	err := db.Unwrap().QueryRowContext(ctx, `
		SELECT result, violation_raised, detail, resolved_at
		FROM assignment_rule_checks
		WHERE assignment_id = ? AND rule_id = 'RULE-004'`,
		"assign-heartbeat",
	).Scan(&result, &violationRaised, &detail, &resolvedAt)
	if err != nil {
		t.Fatalf("query rule-004 check: %v", err)
	}
	if result != "pass" {
		t.Fatalf("result: got %q, want %q", result, "pass")
	}
	if violationRaised != 0 {
		t.Fatalf("violation_raised: got %d, want 0", violationRaised)
	}
	if detail != `{"source":"heartbeat","progress":"fresh"}` {
		t.Fatalf("detail: got %q", detail)
	}
	if resolvedAt != nil {
		t.Fatalf("resolved_at: got %v, want nil", resolvedAt)
	}

	session, err := state.GetAgentSession(ctx, db, "sess-heartbeat")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if session.LastProgressAt == nil {
		t.Fatal("last_progress_at should be set after progress heartbeat")
	}
}
