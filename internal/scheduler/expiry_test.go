package scheduler_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/delivery"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/scheduler"
	"github.com/codero/codero/internal/state"
	"github.com/google/uuid"
)

type mockTmuxChecker struct {
	alive map[string]bool
}

func (m mockTmuxChecker) HasSession(_ context.Context, name string) bool {
	return m.alive[name]
}

func setupExpiryDeps(t *testing.T) (*state.DB, *redislib.Client, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	client := redislib.New(mr.Addr(), "")
	t.Cleanup(func() { _ = client.Close() })

	return db, client, mr
}

func insertBranchWithState(t *testing.T, db *state.DB, repo, branch string, st state.State, maxRetries, retryCount int) string {
	t.Helper()
	id := uuid.New().String()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, head_hash, state, max_retries, retry_count, queue_priority)
		VALUES (?, ?, ?, 'abc123', ?, ?, ?, 0)
	`, id, repo, branch, string(st), maxRetries, retryCount)
	if err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	return id
}

func setSessionLastSeen(t *testing.T, db *state.DB, id string, ts time.Time) {
	t.Helper()
	_, err := db.Unwrap().Exec(
		`UPDATE branch_states SET owner_session_last_seen = ? WHERE id = ?`, ts, id,
	)
	if err != nil {
		t.Fatalf("set session last seen: %v", err)
	}
}

func setLeaseExpiresAt(t *testing.T, db *state.DB, id string, ts time.Time) {
	t.Helper()
	_, err := db.Unwrap().Exec(
		`UPDATE branch_states SET lease_id = 'test-lease', lease_expires_at = ? WHERE id = ?`, ts, id,
	)
	if err != nil {
		t.Fatalf("set lease_expires_at: %v", err)
	}
}

func getBranchState(t *testing.T, db *state.DB, id string) state.State {
	t.Helper()
	var s string
	if err := db.Unwrap().QueryRow(`SELECT state FROM branch_states WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatalf("get state: %v", err)
	}
	return state.State(s)
}

func TestExpiryWorker_SessionExpiry_Abandoned(t *testing.T) {
	db, client, _ := setupExpiryDeps(t)

	repo, branch := "owner/repo", "expired-session"
	id := insertBranchWithState(t, db, repo, branch, state.StateSubmitted, 3, 0)

	// Set last_seen far in the past (past SessionHeartbeatTTL).
	pastTime := time.Now().Add(-scheduler.SessionHeartbeatTTL - 60*time.Second)
	setSessionLastSeen(t, db, id, pastTime)

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)

	ctx := context.Background()
	// Call the cycle directly — no ticker needed for unit tests.
	worker.RunSessionExpiryCycle(ctx)

	// Branch should be abandoned.
	if got := getBranchState(t, db, id); got != state.StateAbandoned {
		t.Errorf("state: got %q, want %q", got, state.StateAbandoned)
	}

	// System event should be appended.
	events, err := delivery.NewStream(db, client).Replay(ctx, repo, branch, 0)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	hasSystem := false
	for _, ev := range events {
		if ev.EventType == "system" {
			hasSystem = true
		}
	}
	if !hasSystem {
		t.Error("expected system event after session expiry")
	}
}

func TestExpiryWorker_SessionExpiry_SkipsRecent(t *testing.T) {
	db, client, _ := setupExpiryDeps(t)

	repo, branch := "owner/repo", "active-session"
	id := insertBranchWithState(t, db, repo, branch, state.StateSubmitted, 3, 0)

	// Set last_seen very recently.
	setSessionLastSeen(t, db, id, time.Now())

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)

	ctx := context.Background()
	worker.RunSessionExpiryCycle(ctx)

	// State should remain unchanged.
	if got := getBranchState(t, db, id); got != state.StateSubmitted {
		t.Errorf("state: got %q, want %q (should not expire recent session)", got, state.StateSubmitted)
	}
}

func TestExpiryWorker_TmuxHeartbeatKeepsSessionAlive(t *testing.T) {
	ctx := context.Background()
	db, client, _ := setupExpiryDeps(t)

	tmuxName := "codero-test-alive"
	if err := state.RegisterAgentSession(ctx, db, "sess-tmux-alive", "agent-1", "agent", tmuxName); err != nil {
		t.Fatalf("register: %v", err)
	}

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)
	worker.TmuxChecker = mockTmuxChecker{alive: map[string]bool{tmuxName: true}}

	worker.RunSessionExpiryCycle(ctx)

	sess, err := state.GetAgentSession(ctx, db, "sess-tmux-alive")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if sess.EndedAt != nil {
		t.Errorf("session should remain active, ended_at=%v", sess.EndedAt)
	}
}

func TestExpiryWorker_TmuxHeartbeatMarksLost(t *testing.T) {
	ctx := context.Background()
	db, client, _ := setupExpiryDeps(t)

	tmuxName := "codero-test-lost"
	if err := state.RegisterAgentSession(ctx, db, "sess-tmux-lost", "agent-1", "agent", tmuxName); err != nil {
		t.Fatalf("register: %v", err)
	}

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)
	worker.TmuxChecker = mockTmuxChecker{alive: map[string]bool{tmuxName: false}}

	worker.RunSessionExpiryCycle(ctx)

	sess, err := state.GetAgentSession(ctx, db, "sess-tmux-lost")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if sess.EndReason != "lost" {
		t.Errorf("end_reason: got %q, want lost", sess.EndReason)
	}
}

func TestExpiryWorker_AgentSessionExpiry_EndsSessionAndAssignment(t *testing.T) {
	db, client, _ := setupExpiryDeps(t)

	ctx := context.Background()
	if err := state.RegisterAgentSession(ctx, db, "sess-agent", "agent-1", "cli", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	if err := state.AttachAgentAssignment(ctx, db, &state.AgentAssignment{
		ID:        "assign-agent",
		SessionID: "sess-agent",
		AgentID:   "agent-1",
		Repo:      "owner/repo",
		Branch:    "feat/COD-123-expiry",
		Worktree:  "/worktrees/codero/wt-agent",
		TaskID:    "COD-123",
	}); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}
	_, err := db.Unwrap().Exec(
		`UPDATE agent_sessions SET last_seen_at = datetime('now','-2 hours') WHERE session_id = ?`,
		"sess-agent",
	)
	if err != nil {
		t.Fatalf("seed last_seen_at: %v", err)
	}

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)

	worker.RunSessionExpiryCycle(ctx)

	session, err := state.GetAgentSession(ctx, db, "sess-agent")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if session.EndedAt == nil {
		t.Fatal("ended_at should be set")
	}
	if session.EndReason != "lost" {
		t.Errorf("end_reason: got %q, want %q", session.EndReason, "lost")
	}

	_, err = state.GetActiveAgentAssignment(ctx, db, "sess-agent")
	if !errors.Is(err, state.ErrAgentAssignmentNotFound) {
		t.Fatalf("GetActiveAgentAssignment: expected ErrAgentAssignmentNotFound, got %v", err)
	}

	assignments, err := state.ListAgentAssignments(ctx, db, "sess-agent")
	if err != nil {
		t.Fatalf("ListAgentAssignments: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("assignments count: got %d, want 1", len(assignments))
	}
	if assignments[0].EndedAt == nil {
		t.Fatal("assignment ended_at should be set")
	}
	if assignments[0].EndReason != "lost" {
		t.Errorf("assignment end_reason: got %q, want %q", assignments[0].EndReason, "lost")
	}
	if assignments[0].Substatus != state.AssignmentSubstatusTerminalLost {
		t.Errorf("assignment substatus: got %q, want %q", assignments[0].Substatus, state.AssignmentSubstatusTerminalLost)
	}

	events, err := state.ListAgentEvents(ctx, db, "sess-agent", 0)
	if err != nil {
		t.Fatalf("ListAgentEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events count: got %d, want 3", len(events))
	}
	if events[len(events)-1].EventType != "session_protocol_lost" {
		t.Errorf("event_type: got %q, want %q", events[len(events)-1].EventType, "session_protocol_lost")
	}
}

func TestExpiryWorker_LeaseAudit_Requeue(t *testing.T) {
	db, client, _ := setupExpiryDeps(t)

	repo, branch := "owner/repo", "expired-lease"
	id := insertBranchWithState(t, db, repo, branch, state.StateCLIReviewing, 3, 0)

	// Set lease_expires_at in the past.
	setLeaseExpiresAt(t, db, id, time.Now().Add(-60*time.Second))

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)

	ctx := context.Background()
	// Call the lease audit cycle directly.
	worker.RunLeaseAuditCycle(ctx)

	// Branch should be re-queued (queued_cli) since retry_count(1) < max_retries(3).
	if got := getBranchState(t, db, id); got != state.StateQueuedCLI {
		t.Errorf("state after lease expiry: got %q, want %q", got, state.StateQueuedCLI)
	}

	// retry_count should be incremented.
	var rc int
	if err := db.Unwrap().QueryRow(`SELECT retry_count FROM branch_states WHERE id = ?`, id).Scan(&rc); err != nil {
		t.Fatalf("get retry_count: %v", err)
	}
	if rc != 1 {
		t.Errorf("retry_count: got %d, want 1", rc)
	}
}

func TestExpiryWorker_LeaseAudit_Blocked(t *testing.T) {
	db, client, _ := setupExpiryDeps(t)

	repo, branch := "owner/repo", "max-retries"
	// max_retries=2, retry_count=2 → will be blocked after increment.
	id := insertBranchWithState(t, db, repo, branch, state.StateCLIReviewing, 2, 2)

	setLeaseExpiresAt(t, db, id, time.Now().Add(-60*time.Second))

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)

	ctx := context.Background()
	worker.RunLeaseAuditCycle(ctx)

	if got := getBranchState(t, db, id); got != state.StateBlocked {
		t.Errorf("state: got %q, want %q (max retries exceeded)", got, state.StateBlocked)
	}
}

func TestExpiryWorker_Run_StopsOnCancel(t *testing.T) {
	db, client, _ := setupExpiryDeps(t)
	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// Use very short intervals to verify ticks fire and worker stops cleanly.
		worker.RunWithIntervals(ctx, 50*time.Millisecond, 50*time.Millisecond)
		close(done)
	}()

	time.Sleep(120 * time.Millisecond) // let a couple ticks fire
	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Error("worker did not stop after context cancellation")
	}
}
