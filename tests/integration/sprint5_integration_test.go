// Package integration provides end-to-end integration tests for Sprint 5.
// Tests cover: webhook dedup, reconciliation drift repair, polling-only mode,
// delivery replay semantics, Redis restart recovery, and lease expiry mid-review.
package integration_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/delivery"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/runner"
	"github.com/codero/codero/internal/scheduler"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/webhook"
	"github.com/google/uuid"
)

// ---- helpers ----

func openDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openRedis(t *testing.T) (*redislib.Client, *miniredis.Miniredis) {
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

func insertBranch(t *testing.T, db *state.DB, repo, branch string, st state.State, head string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, head_hash, state, max_retries, queue_priority)
		VALUES (?, ?, ?, ?, ?, 3, 0)
	`, id, repo, branch, head, string(st))
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

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookPayload(t *testing.T, repo string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"repository": map[string]any{"full_name": repo},
		"action":     "opened",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// ---- tests ----

// TestIntegration_WebhookDedup verifies that duplicate webhook deliveries are
// dropped idempotently and only the first delivery is processed.
func TestIntegration_WebhookDedup(t *testing.T) {
	db := openDB(t)
	client, _ := openRedis(t)

	secret := "integration-secret"
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewHandler(secret, dedup, &webhook.NopProcessor{})

	body := webhookPayload(t, "owner/repo")
	deliveryID := "int-del-001"
	sig := sign(secret, body)

	// First delivery: must succeed.
	req1 := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req1.Header.Set("X-GitHub-Delivery", deliveryID)
	req1.Header.Set("X-GitHub-Event", "pull_request")
	req1.Header.Set("X-Hub-Signature-256", sig)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first delivery: got %d, want 200", rr1.Code)
	}

	// Second delivery with identical ID: must be dropped silently.
	req2 := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req2.Header.Set("X-GitHub-Delivery", deliveryID)
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("duplicate delivery: got %d, want 200", rr2.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(rr2.Body).Decode(&resp)
	if resp["status"] != "duplicate" {
		t.Errorf("duplicate response: got %q, want %q", resp["status"], "duplicate")
	}

	// Verify durable dedup record exists.
	known, err := dedup.IsKnown(context.Background(), deliveryID)
	if err != nil {
		t.Fatalf("IsKnown: %v", err)
	}
	if !known {
		t.Error("delivery should be marked known in durable store")
	}
}

// TestIntegration_ReconciliationDriftRepair verifies that the reconciler detects
// and repairs state drift: a branch in reviewed state with PR closed → closed.
func TestIntegration_ReconciliationDriftRepair(t *testing.T) {
	db := openDB(t)

	repo, branch := "owner/repo", "drift-branch"
	id := insertBranch(t, db, repo, branch, state.StateReviewed, "abc123")

	// GitHub says the PR is closed (drift from reviewed).
	ghClient := &mockGitHub{
		closedRepos: map[string]bool{repo + "/" + branch: true},
	}

	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go rec.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()

	if got := getState(t, db, id); got != state.StateClosed {
		t.Errorf("state: got %q, want %q (drift repair: PR closed)", got, state.StateClosed)
	}
}

// TestIntegration_PollingOnlyMode verifies the daemon can operate without
// webhooks enabled: runner processes branches, reconciler polls, expiry fires.
func TestIntegration_PollingOnlyMode(t *testing.T) {
	db := openDB(t)
	client, _ := openRedis(t)

	repo, branch := "owner/repo", "polling-only"
	id := insertBranch(t, db, repo, branch, state.StateQueuedCLI, "abc123")

	// Enqueue the branch.
	q := scheduler.NewQueue(client)
	ctx := context.Background()
	if err := q.Enqueue(ctx, scheduler.QueueEntry{Repo: repo, Branch: branch}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stream := delivery.NewStream(db, client)
	lm := scheduler.NewLeaseManager(client)

	// webhookEnabled=false: polling-only mode.
	r := runner.New(db, q, lm, stream,
		runner.NewStubProvider(0),
		runner.Config{
			Repos:        []string{repo},
			PollInterval: 50 * time.Millisecond,
		},
	)

	runCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	r.Run(runCtx)

	// Branch should have been reviewed.
	if got := getState(t, db, id); got != state.StateReviewed {
		t.Errorf("state: got %q, want %q (polling-only mode must work)", got, state.StateReviewed)
	}
}

// TestIntegration_DeliveryReplaySemantics verifies that replay fetches exactly
// the events since sinceSeq, in order, idempotently.
func TestIntegration_DeliveryReplaySemantics(t *testing.T) {
	db := openDB(t)
	client, _ := openRedis(t)

	stream := delivery.NewStream(db, client)
	ctx := context.Background()

	repo, branch := "owner/repo", "replay-branch"

	// Append 5 events.
	for i := 0; i < 5; i++ {
		if _, err := stream.AppendSystem(ctx, repo, branch, "abc", "test", "event"); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Replay all.
	all, err := stream.Replay(ctx, repo, branch, 0)
	if err != nil {
		t.Fatalf("replay all: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("replay all: got %d, want 5", len(all))
	}

	// Replay since seq 3: must return events 4, 5.
	partial, err := stream.Replay(ctx, repo, branch, 3)
	if err != nil {
		t.Fatalf("replay partial: %v", err)
	}
	if len(partial) != 2 {
		t.Fatalf("replay since 3: got %d, want 2", len(partial))
	}
	if partial[0].Seq != 4 || partial[1].Seq != 5 {
		t.Errorf("partial seq: got %d,%d; want 4,5", partial[0].Seq, partial[1].Seq)
	}

	// Replay again: idempotent.
	partial2, err := stream.Replay(ctx, repo, branch, 3)
	if err != nil {
		t.Fatalf("replay partial idempotent: %v", err)
	}
	if len(partial2) != len(partial) {
		t.Errorf("idempotent: got %d then %d", len(partial), len(partial2))
	}
}

// TestIntegration_RedisRestart_SeqNoRegression verifies that after Redis is
// flushed (simulating a restart), the delivery seq counter is re-seeded from
// the durable floor and does not regress.
func TestIntegration_RedisRestart_SeqNoRegression(t *testing.T) {
	db := openDB(t)
	client, mr := openRedis(t)

	stream := delivery.NewStream(db, client)
	ctx := context.Background()

	repo, branch := "owner/repo", "redis-restart"

	// Append 3 events (seq 1, 2, 3).
	for i := 0; i < 3; i++ {
		if _, err := stream.AppendSystem(ctx, repo, branch, "abc", "test", "ev"); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Simulate Redis restart: flush all keys.
	mr.FlushAll()

	// Re-seed seq floor from durable store.
	if err := stream.InitSeqFloor(ctx, repo, branch); err != nil {
		t.Fatalf("InitSeqFloor: %v", err)
	}

	// Next append must have seq > 3.
	seq, err := stream.AppendSystem(ctx, repo, branch, "abc", "test", "after-restart")
	if err != nil {
		t.Fatalf("append after restart: %v", err)
	}
	if seq <= 3 {
		t.Errorf("seq regression: got %d, want > 3", seq)
	}
}

// TestIntegration_LeaseExpiryDuringReview verifies that when a runner's lease
// expires during in-flight review (simulated via expiry worker audit), the
// branch is re-queued and retry_count is incremented.
func TestIntegration_LeaseExpiryDuringReview(t *testing.T) {
	db := openDB(t)
	client, _ := openRedis(t)

	repo, branch := "owner/repo", "lease-expiry"
	id := insertBranch(t, db, repo, branch, state.StateCLIReviewing, "abc123")

	// Simulate a lease that has already expired.
	_, err := db.Unwrap().Exec(
		`UPDATE branch_states SET lease_id = 'expired-lease', lease_expires_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Minute), id,
	)
	if err != nil {
		t.Fatalf("set expired lease: %v", err)
	}

	q := scheduler.NewQueue(client)
	stream := delivery.NewStream(db, client)
	worker := scheduler.NewExpiryWorker(db, q, stream)

	ctx := context.Background()
	worker.RunLeaseAuditCycle(ctx)

	// Branch should be re-queued (retry_count 1 < max_retries 3).
	if got := getState(t, db, id); got != state.StateQueuedCLI {
		t.Errorf("state: got %q, want %q (lease expired → requeue)", got, state.StateQueuedCLI)
	}

	var rc int
	if err := db.Unwrap().QueryRow(`SELECT retry_count FROM branch_states WHERE id = ?`, id).Scan(&rc); err != nil {
		t.Fatalf("get retry_count: %v", err)
	}
	if rc != 1 {
		t.Errorf("retry_count: got %d, want 1", rc)
	}
}

// TestIntegration_DuplicateWebhooks_RaceCondition verifies that concurrent
// duplicate webhooks are safely deduplicated without data corruption.
func TestIntegration_DuplicateWebhooks_RaceCondition(t *testing.T) {
	db := openDB(t)
	client, _ := openRedis(t)

	dedup := webhook.NewDeduplicator(db, client)
	ctx := context.Background()

	deliveryID := "race-del-001"
	const workers = 10
	results := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func() {
			results <- dedup.Check(ctx, deliveryID, "push", "owner/repo")
		}()
	}

	// Collect results: exactly one nil (first delivery), rest ErrDuplicate.
	successes := 0
	for i := 0; i < workers; i++ {
		err := <-results
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Errorf("concurrent dedup: got %d successes, want exactly 1", successes)
	}
}

// ---- mock GitHub client ----

type mockGitHub struct {
	closedRepos map[string]bool // key: "repo/branch"
}

func (m *mockGitHub) GetPRState(_ context.Context, repo, branch string) (*webhook.GitHubState, error) {
	key := repo + "/" + branch
	if m.closedRepos[key] {
		return &webhook.GitHubState{
			Repo:   repo,
			Branch: branch,
			PROpen: false,
		}, nil
	}
	return &webhook.GitHubState{
		Repo:   repo,
		Branch: branch,
		PROpen: true,
	}, nil
}
