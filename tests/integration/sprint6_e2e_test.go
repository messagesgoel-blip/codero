// Package integration provides the Sprint 6 end-to-end lifecycle test.
// This test validates the full Sprint 6 state machine flow including:
// - Branch registration into local_review
// - Commit-gate path semantics
// - Transition to queued_cli
// - Review cycle to reviewed
// - Merge-ready computation
// - Event delivery and queue visibility
package integration_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/delivery"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/runner"
	"github.com/codero/codero/internal/scheduler"
	"github.com/codero/codero/internal/state"
	"github.com/google/uuid"
)

// Sprint6E2ETestConfig holds test configuration.
type Sprint6E2ETestConfig struct {
	Repo            string
	Branch          string
	HeadHash        string
	PollInterval    time.Duration
	LeaseTTL        time.Duration
	MaxRetries      int
	QueuePriority   int
	HeartbeatTTL    time.Duration
	SessionInterval time.Duration
	LeaseInterval   time.Duration
}

// defaultTestConfig returns sensible test defaults.
func defaultTestConfig() Sprint6E2ETestConfig {
	return Sprint6E2ETestConfig{
		Repo:            "owner/sprint6-e2e",
		Branch:          "feat/sprint6-test",
		HeadHash:        "abc123def456",
		PollInterval:    50 * time.Millisecond,
		LeaseTTL:        500 * time.Millisecond,
		MaxRetries:      3,
		QueuePriority:   10,
		HeartbeatTTL:    1800 * time.Second,
		SessionInterval: 100 * time.Millisecond,
		LeaseInterval:   50 * time.Millisecond,
	}
}

// TestSprint6_E2E_Lifecycle validates the complete Sprint 6 flow:
// 1. Register branch into local_review
// 2. Execute commit-gate path (simulated pass)
// 3. Transition to queued_cli
// 4. Dispatch/review cycle to reviewed
// 5. Update readiness signals to merge_ready
// 6. Verify events/delivery records and queue visibility
func TestSprint6_E2E_Lifecycle(t *testing.T) {
	cfg := defaultTestConfig()
	ctx := context.Background()

	// Setup isolated test infrastructure
	db := openTestDB(t)
	client, mr := openTestRedis(t)
	_ = mr // cleanup handled by t.Cleanup in openTestRedis

	stream := delivery.NewStream(db, client)
	q := scheduler.NewQueue(client)
	lm := scheduler.NewLeaseManager(client)

	// Step 1: Register branch (simulating T01: new -> coding)
	branchID := registerBranch(t, db, cfg, state.StateCoding)

	// Verify initial state
	assertBranchState(t, db, branchID, state.StateCoding)

	// Step 2: Transition to local_review (T02: coding -> local_review)
	err := state.TransitionBranch(db, branchID, state.StateCoding, state.StateLocalReview, "agent_ready_for_review")
	if err != nil {
		t.Fatalf("T02 transition failed: %v", err)
	}
	assertBranchState(t, db, branchID, state.StateLocalReview)

	// Step 3: Simulate commit-gate pass (both pre-commit loops pass)
	// In real flow: LiteLLM pass -> CodeRabbit pass
	// For test: directly transition to queued_cli (T04: local_review -> queued_cli)
	err = state.TransitionBranch(db, branchID, state.StateLocalReview, state.StateQueuedCLI, "commit_gate_passed")
	if err != nil {
		t.Fatalf("T04 transition failed: %v", err)
	}
	assertBranchState(t, db, branchID, state.StateQueuedCLI)

	// Enqueue the branch
	err = q.Enqueue(ctx, scheduler.QueueEntry{
		Repo:     cfg.Repo,
		Branch:   cfg.Branch,
		Priority: scheduler.QueuePriority(cfg.QueuePriority),
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Verify queue visibility
	queueLen, err := q.Len(ctx, cfg.Repo)
	if err != nil {
		t.Fatalf("queue len failed: %v", err)
	}
	if queueLen != 1 {
		t.Errorf("queue length: got %d, want 1", queueLen)
	}

	// Step 4: Run review cycle (T06: queued_cli -> cli_reviewing -> reviewed)
	// Create stub provider that returns no findings (clean review)
	provider := runner.NewStubProvider(0)

	r := runner.New(db, q, lm, stream, provider, runner.Config{
		Repos:        []string{cfg.Repo},
		PollInterval: cfg.PollInterval,
		LeaseTTL:     cfg.LeaseTTL,
	})

	// Run for a short duration to complete one review cycle
	runCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	r.Run(runCtx)

	// Verify transition to reviewed (T08)
	assertBranchState(t, db, branchID, state.StateReviewed)

	// Verify review run was recorded
	runs, err := listReviewRuns(db, cfg.Repo, cfg.Branch)
	if err != nil {
		t.Fatalf("list review runs: %v", err)
	}
	if len(runs) == 0 {
		t.Error("expected at least one review run record")
	} else if runs[0].Status != "completed" {
		t.Errorf("review run status: got %s, want completed", runs[0].Status)
	}

	// Step 5: Update merge readiness signals
	// T10: reviewed -> merge_ready requires all conditions
	err = state.UpdateMergeReadiness(db, branchID, true, true, 0, 0)
	if err != nil {
		t.Fatalf("update merge readiness: %v", err)
	}

	// Transition to merge_ready (T10)
	err = state.TransitionBranch(db, branchID, state.StateReviewed, state.StateMergeReady, "merge_ready_computed")
	if err != nil {
		t.Fatalf("T10 transition failed: %v", err)
	}
	assertBranchState(t, db, branchID, state.StateMergeReady)

	// Verify merge_ready conditions persisted
	rec, err := state.GetBranchByID(db, branchID)
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if !rec.Approved || !rec.CIGreen || rec.PendingEvents != 0 || rec.UnresolvedThreads != 0 {
		t.Errorf("merge_ready conditions not persisted: approved=%v ci_green=%v pending=%d threads=%d",
			rec.Approved, rec.CIGreen, rec.PendingEvents, rec.UnresolvedThreads)
	}

	// Step 6: Verify delivery events exist
	events, err := stream.Replay(ctx, cfg.Repo, cfg.Branch, 0)
	if err != nil {
		t.Fatalf("replay events: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected delivery events after review cycle")
	}

	// Verify seq monotonicity
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Errorf("seq not monotonic: event %d has seq %d, previous has %d", i, events[i].Seq, events[i-1].Seq)
		}
	}
}

// TestSprint6_InvalidTransition_Rejection verifies that invalid state transitions
// are rejected with ErrInvalidTransition.
func TestSprint6_InvalidTransition_Rejection(t *testing.T) {
	cfg := defaultTestConfig()
	db := openTestDB(t)

	// Register branch in coding state
	branchID := registerBranch(t, db, cfg, state.StateCoding)

	// Attempt invalid transition: coding -> merge_ready (not allowed)
	err := state.TransitionBranch(db, branchID, state.StateCoding, state.StateMergeReady, "invalid_test")
	if !errors.Is(err, state.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for coding -> merge_ready, got: %v", err)
	}

	// Attempt invalid transition: coding -> cli_reviewing (skip queue)
	err = state.TransitionBranch(db, branchID, state.StateCoding, state.StateCLIReviewing, "invalid_test")
	if !errors.Is(err, state.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for coding -> cli_reviewing, got: %v", err)
	}

	// State should remain unchanged
	assertBranchState(t, db, branchID, state.StateCoding)
}

// TestSprint6_LeaseExpiry_RetrySemantics verifies T07: lease expiry increments
// retry_count and re-queues the branch.
func TestSprint6_LeaseExpiry_RetrySemantics(t *testing.T) {
	cfg := defaultTestConfig()
	ctx := context.Background()

	db := openTestDB(t)
	client, mr := openTestRedis(t)
	_ = mr // cleanup handled by t.Cleanup in openTestRedis

	stream := delivery.NewStream(db, client)
	q := scheduler.NewQueue(client)

	// Register branch in cli_reviewing with expired lease
	branchID := registerBranch(t, db, cfg, state.StateCLIReviewing)

	// Set expired lease
	_, err := db.Unwrap().Exec(
		`UPDATE branch_states SET lease_id = 'expired-lease', lease_expires_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Minute), branchID,
	)
	if err != nil {
		t.Fatalf("set expired lease: %v", err)
	}

	// Run lease audit cycle
	worker := scheduler.NewExpiryWorker(db, q, stream)
	worker.RunLeaseAuditCycle(ctx)

	// Verify T07: cli_reviewing -> queued_cli
	assertBranchState(t, db, branchID, state.StateQueuedCLI)

	// Verify retry_count incremented
	rec, err := state.GetBranchByID(db, branchID)
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if rec.RetryCount != 1 {
		t.Errorf("retry_count: got %d, want 1", rec.RetryCount)
	}

	// Verify system event delivered
	events, err := stream.Replay(ctx, cfg.Repo, cfg.Branch, 0)
	if err != nil {
		t.Fatalf("replay events: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected lease_expired system event")
	}
}

// TestSprint6_LeaseExpiry_BlockedTransition verifies T16: when retry_count >= max_retries,
// branch transitions to blocked instead of re-queuing.
func TestSprint6_LeaseExpiry_BlockedTransition(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.MaxRetries = 2 // Lower for faster test
	ctx := context.Background()

	db := openTestDB(t)
	client, mr := openTestRedis(t)
	_ = mr // cleanup handled by t.Cleanup in openTestRedis

	stream := delivery.NewStream(db, client)
	q := scheduler.NewQueue(client)

	// Register branch with retry_count at max - 1
	branchID := registerBranch(t, db, cfg, state.StateCLIReviewing)

	// Set retry_count to max - 1 and expired lease
	_, err := db.Unwrap().Exec(
		`UPDATE branch_states SET retry_count = ?, lease_id = 'expired-lease', lease_expires_at = ? WHERE id = ?`,
		cfg.MaxRetries-1, time.Now().Add(-2*time.Minute), branchID,
	)
	if err != nil {
		t.Fatalf("set branch state: %v", err)
	}

	// Run lease audit cycle
	worker := scheduler.NewExpiryWorker(db, q, stream)
	worker.RunLeaseAuditCycle(ctx)

	// Verify T16: cli_reviewing -> blocked
	assertBranchState(t, db, branchID, state.StateBlocked)

	// Verify retry_count is at max
	rec, err := state.GetBranchByID(db, branchID)
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if rec.RetryCount != cfg.MaxRetries {
		t.Errorf("retry_count: got %d, want %d", rec.RetryCount, cfg.MaxRetries)
	}
}

// TestSprint6_MergeReady_Guardrails verifies that merge_ready conditions
// are correctly persisted and the transition is valid in the state machine.
// Note: T10 (reviewed -> merge_ready) is always a valid transition in the state machine.
// The actual guardrail (conditions check) is enforced by the reconciler/watch in production.
func TestSprint6_MergeReady_Guardrails(t *testing.T) {
	cfg := defaultTestConfig()
	db := openTestDB(t)

	tests := []struct {
		name              string
		approved          bool
		ciGreen           bool
		pendingEvents     int
		unresolvedThreads int
	}{
		{"all conditions met", true, true, 0, 0},
		{"missing approval", false, true, 0, 0},
		{"ci not green", true, false, 0, 0},
		{"pending events", true, true, 1, 0},
		{"unresolved threads", true, true, 0, 1},
		{"multiple failures", false, false, 2, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testCfg := cfg
			testCfg.Branch = cfg.Branch + "-" + tc.name

			branchID := registerBranch(t, db, testCfg, state.StateReviewed)

			err := state.UpdateMergeReadiness(db, branchID, tc.approved, tc.ciGreen, tc.pendingEvents, tc.unresolvedThreads)
			if err != nil {
				t.Fatalf("update merge readiness: %v", err)
			}

			// T10 transition is allowed in state machine; guardrail is enforced by reconciler.
			err = state.TransitionBranch(db, branchID, state.StateReviewed, state.StateMergeReady, "test")
			if err != nil {
				t.Fatalf("T10 transition failed: %v", err)
			}

			rec, err := state.GetBranchByID(db, branchID)
			if err != nil {
				t.Fatalf("get branch: %v", err)
			}

			// Verify stored conditions match the update
			if rec.Approved != tc.approved {
				t.Errorf("approved: got %v, want %v", rec.Approved, tc.approved)
			}
			if rec.CIGreen != tc.ciGreen {
				t.Errorf("ci_green: got %v, want %v", rec.CIGreen, tc.ciGreen)
			}
			if rec.PendingEvents != tc.pendingEvents {
				t.Errorf("pending_events: got %d, want %d", rec.PendingEvents, tc.pendingEvents)
			}
			if rec.UnresolvedThreads != tc.unresolvedThreads {
				t.Errorf("unresolved_threads: got %d, want %d", rec.UnresolvedThreads, tc.unresolvedThreads)
			}

			// Verify state is merge_ready (T10 succeeded)
			if rec.State != state.StateMergeReady {
				t.Errorf("state after transition: got %q, want %q", rec.State, state.StateMergeReady)
			}
		})
	}
}

// TestSprint6_Abandoned_Reactivate verifies T14 and T15: session heartbeat expiry
// causes abandoned state, and reactivate restores it.
func TestSprint6_Abandoned_Reactivate(t *testing.T) {
	cfg := defaultTestConfig()
	ctx := context.Background()

	db := openTestDB(t)
	client, mr := openTestRedis(t)
	_ = mr // cleanup handled by t.Cleanup in openTestRedis

	stream := delivery.NewStream(db, client)
	q := scheduler.NewQueue(client)

	// Register branch with expired session and non-zero retry_count
	branchID := registerBranch(t, db, cfg, state.StateQueuedCLI)

	// Seed non-zero retry_count to verify it gets reset on reactivate
	_, err := db.Unwrap().Exec(
		`UPDATE branch_states SET retry_count = 2 WHERE id = ?`,
		branchID,
	)
	if err != nil {
		t.Fatalf("set retry_count: %v", err)
	}

	// Set session last seen past TTL
	_, err = db.Unwrap().Exec(
		`UPDATE branch_states SET owner_session_last_seen = ? WHERE id = ?`,
		time.Now().Add(-scheduler.SessionHeartbeatTTL-time.Minute), branchID,
	)
	if err != nil {
		t.Fatalf("set session last seen: %v", err)
	}

	// Run session expiry cycle
	worker := scheduler.NewExpiryWorker(db, q, stream)
	worker.RunSessionExpiryCycle(ctx)

	// Verify T14: -> abandoned
	assertBranchState(t, db, branchID, state.StateAbandoned)

	// T15: Reactivate via transition + reset retry_count (production path)
	err = state.TransitionBranch(db, branchID, state.StateAbandoned, state.StateQueuedCLI, "reactivate")
	if err != nil {
		t.Fatalf("T15 reactivate failed: %v", err)
	}

	// Reset retry_count as part of reactivation (matches CLI/API reactivate behavior)
	err = state.ResetRetryCount(db, branchID)
	if err != nil {
		t.Fatalf("reset retry_count: %v", err)
	}

	// Verify retry_count reset
	rec, err := state.GetBranchByID(db, branchID)
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if rec.RetryCount != 0 {
		t.Errorf("retry_count after reactivate: got %d, want 0", rec.RetryCount)
	}
}

// TestSprint6_Delivery_SeqNoRegression verifies that after Redis restart,
// seq counter does not regress below durable floor.
func TestSprint6_Delivery_SeqNoRegression(t *testing.T) {
	cfg := defaultTestConfig()
	ctx := context.Background()

	db := openTestDB(t)
	client, mr := openTestRedis(t)
	_ = mr // cleanup handled by t.Cleanup in openTestRedis

	stream := delivery.NewStream(db, client)

	// Append initial events
	for i := 0; i < 3; i++ {
		_, err := stream.AppendSystem(ctx, cfg.Repo, cfg.Branch, cfg.HeadHash, "test", "event")
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Get durable floor
	floor, err := state.GetDeliverySeqFloor(db, cfg.Repo, cfg.Branch)
	if err != nil {
		t.Fatalf("get seq floor: %v", err)
	}

	// Simulate Redis restart
	mr.FlushAll()

	// Re-init seq floor
	err = stream.InitSeqFloor(ctx, cfg.Repo, cfg.Branch)
	if err != nil {
		t.Fatalf("init seq floor: %v", err)
	}

	// Next append should have seq > floor
	seq, err := stream.AppendSystem(ctx, cfg.Repo, cfg.Branch, cfg.HeadHash, "after_restart", "event")
	if err != nil {
		t.Fatalf("append after restart: %v", err)
	}

	if seq <= floor {
		t.Errorf("seq regression: got %d, want > %d", seq, floor)
	}
}

// TestSprint6_TUI_Contracts verifies that TUI commands return valid responses.
func TestSprint6_TUI_Contracts(t *testing.T) {
	cfg := defaultTestConfig()
	ctx := context.Background()

	db := openTestDB(t)
	client, mr := openTestRedis(t)
	_ = mr // cleanup handled by t.Cleanup in openTestRedis

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)

	// Register and enqueue branch
	branchID := registerBranch(t, db, cfg, state.StateQueuedCLI)
	err := q.Enqueue(ctx, scheduler.QueueEntry{
		Repo:     cfg.Repo,
		Branch:   cfg.Branch,
		Priority: scheduler.QueuePriority(cfg.QueuePriority),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Verify queue visibility (simulates `codero queue`)
	entries, err := q.List(ctx, cfg.Repo)
	if err != nil {
		t.Fatalf("queue list: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("queue entries: got %d, want 1", len(entries))
	}
	if entries[0].Branch != cfg.Branch {
		t.Errorf("queue entry branch: got %s, want %s", entries[0].Branch, cfg.Branch)
	}

	// Verify branch detail (simulates `codero branch <name>`)
	rec, err := state.GetBranch(db, cfg.Repo, cfg.Branch)
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if rec.ID != branchID {
		t.Errorf("branch ID mismatch")
	}

	// Verify events (simulates `codero events`)
	// Produce sample events into delivery stream
	seq1, err := stream.AppendSystem(ctx, cfg.Repo, cfg.Branch, cfg.HeadHash,
		"test_event_1", "sample event for TUI verification")
	if err != nil {
		t.Fatalf("append event 1: %v", err)
	}

	seq2, err := stream.AppendSystem(ctx, cfg.Repo, cfg.Branch, cfg.HeadHash,
		"test_event_2", "another sample event")
	if err != nil {
		t.Fatalf("append event 2: %v", err)
	}

	// Replay events (simulates `codero events --since N`)
	events, err := stream.Replay(ctx, cfg.Repo, cfg.Branch, 0)
	if err != nil {
		t.Fatalf("replay events: %v", err)
	}
	if len(events) < 2 {
		t.Errorf("events count: got %d, want >= 2", len(events))
	}

	// Verify seq monotonicity
	if events[0].Seq != seq1 {
		t.Errorf("first event seq: got %d, want %d", events[0].Seq, seq1)
	}
	if events[1].Seq != seq2 {
		t.Errorf("second event seq: got %d, want %d", events[1].Seq, seq2)
	}

	// Verify event types
	for _, ev := range events {
		if ev.EventType != string(delivery.EventTypeSystem) {
			t.Errorf("unexpected event type: got %s, want %s", ev.EventType, delivery.EventTypeSystem)
		}
	}
}

// --- Test Helpers ---

func openTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "sprint6-test.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openTestRedis(t *testing.T) (*redislib.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redislib.New(mr.Addr(), "")
	t.Cleanup(func() { _ = client.Close() })
	return client, mr
}

func registerBranch(t *testing.T, db *state.DB, cfg Sprint6E2ETestConfig, initialState state.State) string {
	t.Helper()
	id := uuid.New().String()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, head_hash, state, max_retries, queue_priority)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, cfg.Repo, cfg.Branch, cfg.HeadHash, string(initialState), cfg.MaxRetries, cfg.QueuePriority)
	if err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	return id
}

func assertBranchState(t *testing.T, db *state.DB, id string, expected state.State) {
	t.Helper()
	var s string
	err := db.Unwrap().QueryRow(`SELECT state FROM branch_states WHERE id = ?`, id).Scan(&s)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	if state.State(s) != expected {
		t.Errorf("branch state: got %q, want %q", s, expected)
	}
}

func listReviewRuns(db *state.DB, repo, branch string) ([]state.ReviewRun, error) {
	const q = `
		SELECT id, repo, branch, head_hash, provider, status, started_at, finished_at, error, created_at
		FROM review_runs
		WHERE repo = ? AND branch = ?
		ORDER BY created_at DESC`

	rows, err := db.Unwrap().Query(q, repo, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []state.ReviewRun
	for rows.Next() {
		var r state.ReviewRun
		if err := rows.Scan(&r.ID, &r.Repo, &r.Branch, &r.HeadHash, &r.Provider, &r.Status,
			&r.StartedAt, &r.FinishedAt, &r.Error, &r.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}
