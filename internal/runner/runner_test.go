package runner_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/delivery"
	"github.com/codero/codero/internal/normalizer"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/runner"
	"github.com/codero/codero/internal/scheduler"
	"github.com/codero/codero/internal/state"
	"github.com/google/uuid"
)

func setupDeps(t *testing.T) (*state.DB, *redislib.Client, *miniredis.Miniredis) {
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

func insertQueuedBranch(t *testing.T, db *state.DB, repo, branch string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, head_hash, state, max_retries, queue_priority)
		VALUES (?, ?, ?, 'abc123', 'queued_cli', 3, 0)
	`, id, repo, branch)
	if err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	return id
}

func getState(t *testing.T, db *state.DB, id string) state.State {
	t.Helper()
	var s string
	if err := db.Unwrap().QueryRow(`SELECT state FROM branch_states WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatalf("get state: %v", err)
	}
	return state.State(s)
}

func getRetryCount(t *testing.T, db *state.DB, id string) int {
	t.Helper()
	var c int
	if err := db.Unwrap().QueryRow(`SELECT retry_count FROM branch_states WHERE id = ?`, id).Scan(&c); err != nil {
		t.Fatalf("get retry_count: %v", err)
	}
	return c
}

func newTestRunner(db *state.DB, client *redislib.Client, repos []string, provider runner.Provider) *runner.ReviewRunner {
	q := scheduler.NewQueue(client)
	lm := scheduler.NewLeaseManager(client, scheduler.WithLeaseTTL(5*time.Second))
	stream := delivery.NewStream(db, client)
	cfg := runner.Config{
		Repos:             repos,
		PollInterval:      100 * time.Millisecond,
		LeaseTTL:          5 * time.Second,
		HeartbeatInterval: 2 * time.Second,
	}
	return runner.New(db, q, lm, stream, provider, cfg)
}

func TestRunner_SuccessPath(t *testing.T) {
	db, client, _ := setupDeps(t)
	repo := "owner/repo"
	branch := "main"

	id := insertQueuedBranch(t, db, repo, branch)

	// Enqueue the branch.
	q := scheduler.NewQueue(client)
	ctx := context.Background()
	if err := q.Enqueue(ctx, scheduler.QueueEntry{Repo: repo, Branch: branch}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	r := newTestRunner(db, client, []string{repo}, runner.NewStubProvider(0))

	// Run one dispatch cycle manually via a short context.
	runCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	r.Run(runCtx)

	// Branch should be in reviewed state.
	if got := getState(t, db, id); got != state.StateReviewed {
		t.Errorf("state: got %q, want %q", got, state.StateReviewed)
	}

	// Findings should be persisted.
	findings, err := state.ListFindings(db, repo, branch)
	if err != nil {
		t.Fatalf("list findings: %v", err)
	}
	if len(findings) == 0 {
		t.Error("expected at least one finding, got none")
	}

	// Delivery stream should have a finding_bundle event.
	stream := delivery.NewStream(db, client)
	events, err := stream.Replay(ctx, repo, branch, 0)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	hasFindingBundle := false
	for _, ev := range events {
		if ev.EventType == "finding_bundle" {
			hasFindingBundle = true
		}
	}
	if !hasFindingBundle {
		t.Error("expected finding_bundle event in delivery stream")
	}
}

func TestRunner_FailurePath_Requeue(t *testing.T) {
	db, client, _ := setupDeps(t)
	repo := "owner/repo"
	branch := "fail-branch"

	// Use very high max_retries so the branch won't be blocked in one test run.
	id := uuid.New().String()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, head_hash, state, max_retries, queue_priority)
		VALUES (?, ?, ?, 'abc123', 'queued_cli', 100, 0)
	`, id, repo, branch)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	q := scheduler.NewQueue(client)
	ctx := context.Background()
	if err := q.Enqueue(ctx, scheduler.QueueEntry{Repo: repo, Branch: branch}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Provider that always fails; use longer poll interval so only 1 dispatch runs.
	failProvider := &errorProvider{name: "fail", err: errors.New("provider unavailable")}
	stream := delivery.NewStream(db, client)
	lm := scheduler.NewLeaseManager(client, scheduler.WithLeaseTTL(5*time.Second))
	r := runner.New(db, q, lm, stream, failProvider, runner.Config{
		Repos:        []string{repo},
		PollInterval: 50 * time.Millisecond,
	})

	// Run for exactly 80ms — one tick at 50ms, second tick at 100ms would exceed context.
	runCtx, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	r.Run(runCtx)

	// Retry count should be exactly 1.
	rc := getRetryCount(t, db, id)
	if rc != 1 {
		t.Errorf("retry_count: got %d, want 1", rc)
	}

	// Branch should be re-queued (queued_cli) since max_retries=100 not reached.
	if got := getState(t, db, id); got != state.StateQueuedCLI {
		t.Errorf("state after 1 failure: got %q, want %q", got, state.StateQueuedCLI)
	}
}

func TestRunner_FailurePath_Blocked(t *testing.T) {
	db, client, _ := setupDeps(t)
	repo := "owner/repo"
	branch := "block-branch"

	// Insert with max_retries=1 to trigger blocked on first failure.
	id := uuid.New().String()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, head_hash, state, max_retries, retry_count, queue_priority)
		VALUES (?, ?, ?, 'abc123', 'queued_cli', 1, 1, 0)
	`, id, repo, branch)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	q := scheduler.NewQueue(client)
	ctx := context.Background()
	if err := q.Enqueue(ctx, scheduler.QueueEntry{Repo: repo, Branch: branch}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	failProvider := &errorProvider{name: "fail", err: errors.New("permanent error")}
	r := newTestRunner(db, client, []string{repo}, failProvider)

	runCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	r.Run(runCtx)

	// Branch should be blocked.
	if got := getState(t, db, id); got != state.StateBlocked {
		t.Errorf("state after max retries: got %q, want %q", got, state.StateBlocked)
	}
}

func TestRunner_SkipsNonQueuedBranch(t *testing.T) {
	db, client, _ := setupDeps(t)
	repo := "owner/repo"
	branch := "coding-branch"

	// Insert a branch in coding state.
	id := uuid.New().String()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, head_hash, state, max_retries, queue_priority)
		VALUES (?, ?, ?, 'abc123', 'coding', 3, 0)
	`, id, repo, branch)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Enqueue it anyway (should not process it).
	q := scheduler.NewQueue(client)
	ctx := context.Background()
	if err := q.Enqueue(ctx, scheduler.QueueEntry{Repo: repo, Branch: branch}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	r := newTestRunner(db, client, []string{repo}, runner.NewStubProvider(0))
	runCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	r.Run(runCtx)

	// State should remain coding (runner skips non-queued_cli).
	if got := getState(t, db, id); got != state.StateCoding {
		t.Errorf("state: got %q, want %q (runner should skip non-queued_cli)", got, state.StateCoding)
	}
}

func TestRunner_StateTransitionsAuditLogged(t *testing.T) {
	db, client, _ := setupDeps(t)
	repo := "owner/repo"
	branch := "audit-branch"

	id := insertQueuedBranch(t, db, repo, branch)
	q := scheduler.NewQueue(client)
	ctx := context.Background()
	if err := q.Enqueue(ctx, scheduler.QueueEntry{Repo: repo, Branch: branch}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	r := newTestRunner(db, client, []string{repo}, runner.NewStubProvider(0))
	runCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	r.Run(runCtx)

	// Check audit log.
	var count int
	if err := db.Unwrap().QueryRow(
		`SELECT COUNT(*) FROM state_transitions WHERE branch_state_id = ?`, id,
	).Scan(&count); err != nil {
		t.Fatalf("count transitions: %v", err)
	}
	if count < 2 {
		t.Errorf("expected >=2 audit log entries (queued_cli->cli_reviewing, cli_reviewing->reviewed), got %d", count)
	}
}

func TestStubProvider_Deterministic(t *testing.T) {
	ctx := context.Background()
	p := runner.NewStubProvider(0)
	req := runner.ReviewRequest{Repo: "r/r", Branch: "main", HeadHash: "abc"}

	resp1, err := p.Review(ctx, req)
	if err != nil {
		t.Fatalf("review 1: %v", err)
	}
	resp2, err := p.Review(ctx, req)
	if err != nil {
		t.Fatalf("review 2: %v", err)
	}
	if len(resp1.Findings) != len(resp2.Findings) {
		t.Fatalf("non-deterministic finding count: %d vs %d", len(resp1.Findings), len(resp2.Findings))
	}
	for i := range resp1.Findings {
		f1, f2 := resp1.Findings[i], resp2.Findings[i]
		if f1.Severity != f2.Severity || f1.Category != f2.Category ||
			f1.File != f2.File || f1.Line != f2.Line ||
			f1.Message != f2.Message || f1.Source != f2.Source ||
			f1.RuleID != f2.RuleID {
			t.Errorf("finding[%d] non-deterministic:\n  got  %+v\n  want %+v", i, f2, f1)
		}
	}
}

// errorProvider is a test provider that always fails.
type errorProvider struct {
	name string
	err  error
}

func (e *errorProvider) Name() string { return e.name }
func (e *errorProvider) Review(_ context.Context, _ runner.ReviewRequest) (*runner.ReviewResponse, error) {
	return nil, e.err
}

// customProvider returns specific findings for testing.
type customProvider struct {
	findings []normalizer.RawFinding
}

func (c *customProvider) Name() string { return "custom" }
func (c *customProvider) Review(_ context.Context, _ runner.ReviewRequest) (*runner.ReviewResponse, error) {
	return &runner.ReviewResponse{Findings: c.findings}, nil
}
