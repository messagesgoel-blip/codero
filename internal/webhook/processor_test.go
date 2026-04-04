package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/delivery"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/state"
)

// setupProcessorDB creates an in-memory state DB with a registered branch.
func setupProcessorDB(t *testing.T, repo, branch string, st state.State) (*state.DB, string) {
	t.Helper()
	db, err := state.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	const id = "test-id-1"
	_, err = db.Unwrap().Exec(
		`INSERT INTO branch_states (id, repo, branch, state, head_hash, max_retries)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, repo, branch, string(st), "oldhash", 3,
	)
	if err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	return db, id
}

func setupStream(t *testing.T, db *state.DB) *delivery.Stream {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redislib.New(mr.Addr(), "")
	t.Cleanup(func() { client.Close() })
	return delivery.NewStream(db, client)
}

func makeEvent(eventType, repo string, payload map[string]any) GitHubEvent {
	return GitHubEvent{
		DeliveryID: "test-delivery",
		EventType:  eventType,
		Repo:       repo,
		Payload:    payload,
	}
}

func TestEventProcessor_PullRequest_Closed(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	payload := map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"merged": true,
			"head": map[string]any{
				"ref": "feat",
				"sha": "newhash",
			},
		},
	}
	ev := makeEvent("pull_request", "owner/repo", payload)

	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	rec, err := state.GetBranch(db, "owner/repo", "feat")
	if err != nil {
		t.Fatalf("GetBranch: %v", err)
	}
	if rec.State != state.StateMerged {
		t.Errorf("expected merged, got %s", rec.State)
	}
}

func TestEventProcessor_PullRequest_Synchronize(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	payload := map[string]any{
		"action": "synchronize",
		"pull_request": map[string]any{
			"head": map[string]any{
				"ref": "feat",
				"sha": "newhash456",
			},
		},
	}
	ev := makeEvent("pull_request", "owner/repo", payload)

	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	rec, err := state.GetBranch(db, "owner/repo", "feat")
	if err != nil {
		t.Fatalf("GetBranch: %v", err)
	}
	if rec.State != state.StateStale {
		t.Errorf("expected stale, got %s", rec.State)
	}
	if rec.HeadHash != "newhash456" {
		t.Errorf("head_hash: want newhash456, got %s", rec.HeadHash)
	}
}

func TestEventProcessor_PRReview_Approved(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	payload := map[string]any{
		"action": "submitted",
		"review": map[string]any{
			"state": "approved",
		},
		"pull_request": map[string]any{
			"head": map[string]any{
				"ref": "feat",
				"sha": "abc",
			},
		},
	}
	ev := makeEvent("pull_request_review", "owner/repo", payload)

	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	rec, err := state.GetBranch(db, "owner/repo", "feat")
	if err != nil {
		t.Fatalf("GetBranch: %v", err)
	}
	if !rec.Approved {
		t.Error("expected approved=true after APPROVED review")
	}
}

func TestEventProcessor_CheckRun_Success(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	payload := map[string]any{
		"check_run": map[string]any{
			"status":     "completed",
			"conclusion": "success",
			"head_sha":   "abc",
			"check_suite": map[string]any{
				"head_branch": "feat",
			},
		},
	}
	ev := makeEvent("check_run", "owner/repo", payload)

	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	rec, err := state.GetBranch(db, "owner/repo", "feat")
	if err != nil {
		t.Fatalf("GetBranch: %v", err)
	}
	if !rec.CIGreen {
		t.Error("expected ci_green=true after successful check_run")
	}
}

func TestEventProcessor_UnknownEventType_Noop(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	ev := makeEvent("push", "owner/repo", map[string]any{})
	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
}

// insertTestSessionAndAssignment inserts minimal agent_sessions and
// agent_assignments rows for testing link/cache scenarios. Returns the
// assignment_id.
func insertTestSessionAndAssignment(t *testing.T, db *state.DB, sessionID, assignmentID, taskID string) {
	t.Helper()
	raw := db.Unwrap()
	_, err := raw.Exec(
		`INSERT INTO agent_sessions (session_id, agent_id, mode)
		 VALUES (?, 'test-agent', '')`,
		sessionID,
	)
	if err != nil {
		t.Fatalf("insert agent_session: %v", err)
	}
	_, err = raw.Exec(
		`INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch, task_id)
		 VALUES (?, ?, 'test-agent', 'owner/repo', 'feat', ?)`,
		assignmentID, sessionID, taskID,
	)
	if err != nil {
		t.Fatalf("insert agent_assignment: %v", err)
	}
}

func TestProcessEvent_PROpened_UpdatesGitHubLink(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	ctx := context.Background()

	// Pre-create a github link for this branch (simulates task acceptance).
	link := &state.GitHubLink{
		TaskID:       "TASK-PR-OPEN",
		RepoFullName: "owner/repo",
		BranchName:   "feat",
	}
	if err := state.UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}

	// Verify link has no PR number yet.
	got, err := state.GetLinkByBranch(ctx, db, "owner/repo", "feat")
	if err != nil {
		t.Fatalf("GetLinkByBranch before: %v", err)
	}
	if got.PRNumber != 0 {
		t.Fatalf("pr_number before: want 0, got %d", got.PRNumber)
	}

	// Fire a PR opened event.
	payload := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": float64(42),
			"head": map[string]any{
				"ref": "feat",
				"sha": "openhash",
			},
		},
	}
	ev := makeEvent("pull_request", "owner/repo", payload)

	if err := proc.ProcessEvent(ctx, ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// Assert link now has pr_number and pr_state set.
	got, err = state.GetLinkByBranch(ctx, db, "owner/repo", "feat")
	if err != nil {
		t.Fatalf("GetLinkByBranch after: %v", err)
	}
	if got.PRNumber != 42 {
		t.Errorf("pr_number: want 42, got %d", got.PRNumber)
	}
	if got.PRState != "open" {
		t.Errorf("pr_state: want %q, got %q", "open", got.PRState)
	}
}

func TestProcessEvent_CheckRun_InvalidatesFeedbackCache(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	ctx := context.Background()

	// Create prerequisite session + assignment for FK constraints.
	insertTestSessionAndAssignment(t, db, "sess-cr-1", "asgn-cr-1", "TASK-CR-001")

	// Insert a github link for the branch.
	link := &state.GitHubLink{
		TaskID:       "TASK-CR-001",
		RepoFullName: "owner/repo",
		BranchName:   "feat",
	}
	if err := state.UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}

	// Insert a feedback cache row for this task.
	fc := &state.FeedbackCache{
		AssignmentID: "asgn-cr-1",
		SessionID:    "sess-cr-1",
		TaskID:       "TASK-CR-001",
		CacheHash:    "hash-before",
		SourceStatus: "{}",
	}
	if err := state.UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache: %v", err)
	}

	// Verify cache exists.
	if _, err := state.GetFeedbackCacheByAssignment(ctx, db, "asgn-cr-1"); err != nil {
		t.Fatalf("GetFeedbackCacheByAssignment before: %v", err)
	}

	// Fire a check_run completed event.
	payload := map[string]any{
		"check_run": map[string]any{
			"id":         float64(99999),
			"status":     "completed",
			"conclusion": "success",
			"head_sha":   "abc",
			"check_suite": map[string]any{
				"head_branch": "feat",
			},
		},
	}
	ev := makeEvent("check_run", "owner/repo", payload)

	if err := proc.ProcessEvent(ctx, ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// Assert feedback cache has been invalidated (deleted).
	_, err := state.GetFeedbackCacheByAssignment(ctx, db, "asgn-cr-1")
	if err == nil {
		t.Fatal("expected feedback cache to be deleted after check_run, but it still exists")
	}
	if !errors.Is(err, state.ErrFeedbackCacheNotFound) {
		t.Fatalf("expected ErrFeedbackCacheNotFound, got %v", err)
	}

	// Also verify the link's ci_run_id was updated.
	got, err := state.GetLinkByBranch(ctx, db, "owner/repo", "feat")
	if err != nil {
		t.Fatalf("GetLinkByBranch after: %v", err)
	}
	if got.LastCIRunID != "99999" {
		t.Errorf("last_ci_run_id: want %q, got %q", "99999", got.LastCIRunID)
	}
}

func TestProcessEvent_Review_InvalidatesFeedbackCache(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	ctx := context.Background()

	// Create prerequisite session + assignment for FK constraints.
	insertTestSessionAndAssignment(t, db, "sess-rv-1", "asgn-rv-1", "TASK-RV-001")

	// Insert a github link for the branch.
	link := &state.GitHubLink{
		TaskID:       "TASK-RV-001",
		RepoFullName: "owner/repo",
		BranchName:   "feat",
	}
	if err := state.UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}

	// Insert a feedback cache row for this task.
	fc := &state.FeedbackCache{
		AssignmentID: "asgn-rv-1",
		SessionID:    "sess-rv-1",
		TaskID:       "TASK-RV-001",
		CacheHash:    "hash-review",
		SourceStatus: "{}",
	}
	if err := state.UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache: %v", err)
	}

	// Verify cache exists.
	if _, err := state.GetFeedbackCacheByAssignment(ctx, db, "asgn-rv-1"); err != nil {
		t.Fatalf("GetFeedbackCacheByAssignment before: %v", err)
	}

	// Fire a pull_request_review approved event.
	payload := map[string]any{
		"action": "submitted",
		"review": map[string]any{
			"state": "approved",
		},
		"pull_request": map[string]any{
			"head": map[string]any{
				"ref": "feat",
				"sha": "abc",
			},
		},
	}
	ev := makeEvent("pull_request_review", "owner/repo", payload)

	if err := proc.ProcessEvent(ctx, ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	// Assert feedback cache has been invalidated (deleted).
	_, err := state.GetFeedbackCacheByAssignment(ctx, db, "asgn-rv-1")
	if err == nil {
		t.Fatal("expected feedback cache to be deleted after review, but it still exists")
	}
	if !errors.Is(err, state.ErrFeedbackCacheNotFound) {
		t.Fatalf("expected ErrFeedbackCacheNotFound, got %v", err)
	}
}

// --- OCL-022: Findings storage & delivery tests ---

func TestEventProcessor_PRReview_FindingsStored(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	payload := map[string]any{
		"action": "submitted",
		"review": map[string]any{
			"state": "changes_requested",
			"body":  "main.go:10: unused variable x",
			"user":  map[string]any{"login": "coderabbitai[bot]"},
		},
		"pull_request": map[string]any{
			"head": map[string]any{"ref": "feat", "sha": "abc"},
		},
	}
	ev := makeEvent("pull_request_review", "owner/repo", payload)

	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}

	findings, err := state.ListFindings(db, "owner/repo", "feat")
	if err != nil {
		t.Fatalf("ListFindings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings count: got %d, want 1", len(findings))
	}
	if findings[0].Severity != "error" {
		t.Errorf("severity: got %q, want error", findings[0].Severity)
	}
	if findings[0].Source != "coderabbitai[bot]" {
		t.Errorf("source: got %q, want coderabbitai[bot]", findings[0].Source)
	}
}

func TestEventProcessor_PRReview_DeliverCalledWithSession(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)

	// Insert active session with repo+branch.
	raw := db.Unwrap()
	_, err := raw.Exec(
		`INSERT INTO agent_sessions (session_id, agent_id, mode, tmux_session_name, repo, branch)
		 VALUES ('sess-1', 'agent-1', 'claude', 'tmux-1', 'owner/repo', 'feat')`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	var deliverCalled bool
	mockAdapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliverCalled = true
	}))
	defer mockAdapter.Close()

	oc := NewOpenClawClient(mockAdapter.URL, mockAdapter.Client())
	proc := NewEventProcessor(db, stream).WithOpenClawClient(oc)

	payload := map[string]any{
		"action": "submitted",
		"review": map[string]any{
			"state": "changes_requested",
			"body":  "main.go:10: unused variable x",
			"user":  map[string]any{"login": "coderabbitai[bot]"},
		},
		"pull_request": map[string]any{
			"head": map[string]any{"ref": "feat", "sha": "abc"},
		},
	}
	ev := makeEvent("pull_request_review", "owner/repo", payload)

	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if !deliverCalled {
		t.Fatal("expected Deliver to be called when active session exists")
	}
}

func TestEventProcessor_PRReview_NoSessionNoDelivery(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)

	deliverCalled := false
	mockAdapter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliverCalled = true
	}))
	defer mockAdapter.Close()

	oc := NewOpenClawClient(mockAdapter.URL, mockAdapter.Client())
	proc := NewEventProcessor(db, stream).WithOpenClawClient(oc)

	payload := map[string]any{
		"action": "submitted",
		"review": map[string]any{
			"state": "changes_requested",
			"body":  "main.go:10: unused variable",
			"user":  map[string]any{"login": "reviewer"},
		},
		"pull_request": map[string]any{
			"head": map[string]any{"ref": "feat", "sha": "abc"},
		},
	}
	ev := makeEvent("pull_request_review", "owner/repo", payload)

	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if deliverCalled {
		t.Fatal("Deliver should NOT be called when no active session exists")
	}
}

func TestEventProcessor_PRReview_NilOpenClaw_NoPanic(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewApproved)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream) // NO WithOpenClawClient

	payload := map[string]any{
		"action": "submitted",
		"review": map[string]any{
			"state": "changes_requested",
			"body":  "main.go:10: unused variable",
			"user":  map[string]any{"login": "reviewer"},
		},
		"pull_request": map[string]any{
			"head": map[string]any{"ref": "feat", "sha": "abc"},
		},
	}
	ev := makeEvent("pull_request_review", "owner/repo", payload)

	// Should not panic even without OpenClaw client.
	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
}
