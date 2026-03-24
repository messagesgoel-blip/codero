package state

import (
	"context"
	"errors"
	"testing"
)

// TestLifecycle_AcceptRetryIdempotency verifies that repeated AcceptTask
// calls from the same session return the same assignment.
func TestLifecycle_AcceptRetryIdempotency(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "lc-sess-1", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	a1, err := AcceptTask(ctx, db, "lc-sess-1", "LC-TASK-001")
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	a2, err := AcceptTask(ctx, db, "lc-sess-1", "LC-TASK-001")
	if err != nil {
		t.Fatalf("second accept: %v", err)
	}
	if a1.ID != a2.ID {
		t.Errorf("idempotent accept: IDs differ: %q != %q", a1.ID, a2.ID)
	}
}

// TestLifecycle_AcceptRaceConflict verifies that two sessions racing
// for the same task result in one winner and one conflict.
func TestLifecycle_AcceptRaceConflict(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "lc-sess-2a", "agent-a", ""); err != nil {
		t.Fatalf("RegisterAgentSession session a: %v", err)
	}
	if err := RegisterAgentSession(ctx, db, "lc-sess-2b", "agent-b", ""); err != nil {
		t.Fatalf("RegisterAgentSession session b: %v", err)
	}

	_, err := AcceptTask(ctx, db, "lc-sess-2a", "LC-TASK-002")
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	_, err = AcceptTask(ctx, db, "lc-sess-2b", "LC-TASK-002")
	if err == nil {
		t.Fatal("expected conflict from rival session")
	}
	if !isTaskAlreadyClaimed(err) {
		t.Errorf("expected ErrTaskAlreadyClaimed, got: %v", err)
	}
}

// TestLifecycle_SubmitGateFailFeedbackRevise verifies the full
// submit → gate fail → feedback → revise → re-submit cycle.
func TestLifecycle_SubmitGateFailFeedbackRevise(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "lc-sess-3", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	a, err := AcceptTask(ctx, db, "lc-sess-3", "LC-TASK-003")
	if err != nil {
		t.Fatalf("AcceptTask: %v", err)
	}

	// Submit (agent signals work ready)
	a, err = EmitAssignmentUpdate(ctx, db, a.ID, 1, AssignmentSubstatusWaitingForCI)
	if err != nil {
		t.Fatalf("submit emit: %v", err)
	}
	if a.Substatus != AssignmentSubstatusWaitingForCI {
		t.Errorf("substatus after submit: got %q, want %q", a.Substatus, AssignmentSubstatusWaitingForCI)
	}

	// Gate fails — Codero transitions to blocked_ci_failure
	a, err = EmitAssignmentUpdate(ctx, db, a.ID, 2, AssignmentSubstatusBlockedCIFailure)
	if err != nil {
		t.Fatalf("gate fail emit: %v", err)
	}
	if a.Substatus != AssignmentSubstatusBlockedCIFailure {
		t.Errorf("substatus after gate fail: got %q", a.Substatus)
	}

	// Agent revises and re-submits (back to in_progress, then waiting_for_ci)
	a, err = EmitAssignmentUpdate(ctx, db, a.ID, 3, AssignmentSubstatusInProgress)
	if err != nil {
		t.Fatalf("revise emit: %v", err)
	}
	a, err = EmitAssignmentUpdate(ctx, db, a.ID, 4, AssignmentSubstatusWaitingForCI)
	if err != nil {
		t.Fatalf("re-submit emit: %v", err)
	}
	if a.Version != 5 {
		t.Errorf("version after re-submit: got %d, want 5", a.Version)
	}
}

// TestLifecycle_GitHubLinkCRUD verifies link creation and lookup paths.
func TestLifecycle_GitHubLinkCRUD(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	link := &GitHubLink{
		TaskID:       "LC-TASK-LINK",
		RepoFullName: "owner/repo",
		BranchName:   "feat/link-test",
		PRNumber:     77,
		PRState:      "open",
	}
	if err := UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}

	// Lookup by task
	byTask, err := GetLinkByTaskID(ctx, db, "LC-TASK-LINK")
	if err != nil {
		t.Fatalf("GetLinkByTaskID: %v", err)
	}
	if byTask.PRNumber != 77 {
		t.Errorf("pr_number: got %d, want 77", byTask.PRNumber)
	}

	// Lookup by repo+PR
	byPR, err := GetLinkByRepoPR(ctx, db, "owner/repo", 77)
	if err != nil {
		t.Fatalf("GetLinkByRepoPR: %v", err)
	}
	if byPR.TaskID != "LC-TASK-LINK" {
		t.Errorf("task_id: got %q", byPR.TaskID)
	}

	// Update PR state
	if err := UpdateLinkPRState(ctx, db, link.LinkID, "merged"); err != nil {
		t.Fatalf("UpdateLinkPRState: %v", err)
	}
	updated, _ := GetLinkByTaskID(ctx, db, "LC-TASK-LINK")
	if updated.PRState != "merged" {
		t.Errorf("pr_state: got %q, want merged", updated.PRState)
	}
}

// TestLifecycle_FeedbackCacheInvalidation verifies cache write + invalidation.
func TestLifecycle_FeedbackCacheInvalidation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "lc-sess-fc", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	a, err := AcceptTask(ctx, db, "lc-sess-fc", "LC-TASK-FC")
	if err != nil {
		t.Fatalf("AcceptTask: %v", err)
	}

	fc := &FeedbackCache{
		AssignmentID: a.ID,
		SessionID:    "lc-sess-fc",
		TaskID:       "LC-TASK-FC",
		CISnapshot:   `{"status":"success"}`,
		CacheHash:    "h1",
		SourceStatus: "{}",
	}
	if err := UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache: %v", err)
	}

	// Verify it exists
	got, err := GetFeedbackCacheByAssignment(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("GetFeedbackCacheByAssignment: %v", err)
	}
	if got.CacheHash != "h1" {
		t.Errorf("cache_hash: got %q, want h1", got.CacheHash)
	}

	// Invalidate
	if err := InvalidateFeedbackCache(ctx, db, a.ID); err != nil {
		t.Fatalf("InvalidateFeedbackCache: %v", err)
	}

	// Verify gone
	_, err = GetFeedbackCacheByAssignment(ctx, db, a.ID)
	if err == nil {
		t.Fatal("expected not-found after invalidation")
	}
}

// TestLifecycle_SourceStatusSemantics verifies source_status storage round-trip.
func TestLifecycle_SourceStatusSemantics(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "lc-sess-ss", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	a, err := AcceptTask(ctx, db, "lc-sess-ss", "LC-TASK-SS")
	if err != nil {
		t.Fatalf("AcceptTask: %v", err)
	}

	ss := `{"ci":"available","coderabbit":"pending","human":"not_configured","compliance":"error"}`
	fc := &FeedbackCache{
		AssignmentID: a.ID,
		SessionID:    "lc-sess-ss",
		TaskID:       "LC-TASK-SS",
		CacheHash:    "ss-hash",
		SourceStatus: ss,
	}
	if err := UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache: %v", err)
	}

	got, err := GetFeedbackCacheByAssignment(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("GetFeedbackCacheByAssignment: %v", err)
	}
	if got.SourceStatus != ss {
		t.Errorf("source_status: got %q, want %q", got.SourceStatus, ss)
	}
}

func isTaskAlreadyClaimed(err error) bool {
	return errors.Is(err, ErrTaskAlreadyClaimed)
}
