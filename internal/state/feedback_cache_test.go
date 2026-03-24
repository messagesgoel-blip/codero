package state

import (
	"context"
	"errors"
	"testing"
)

func TestUpsertFeedbackCache_InsertAndUpdate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create prerequisite session + assignment.
	if err := RegisterAgentSession(ctx, db, "fc-sess-1", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	a, err := AcceptTask(ctx, db, "fc-sess-1", "FC-TASK-001")
	if err != nil {
		t.Fatalf("AcceptTask: %v", err)
	}

	// Insert a new feedback cache row.
	fc := &FeedbackCache{
		AssignmentID:        a.ID,
		SessionID:           "fc-sess-1",
		TaskID:              "FC-TASK-001",
		CISnapshot:          "ci-snap-v1",
		CoderabbitSnapshot:  "cr-snap-v1",
		HumanReviewSnapshot: "hr-snap-v1",
		ComplianceSnapshot:  "comp-snap-v1",
		ContextBlock:        "ctx-block-v1",
		CacheHash:           "hash-v1",
		SourceStatus:        `{"ci":"green"}`,
	}
	if err := UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache insert: %v", err)
	}
	if fc.CacheID == "" {
		t.Fatal("CacheID should be auto-generated")
	}

	// Verify the row was inserted correctly.
	got, err := GetFeedbackCacheByAssignment(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("GetFeedbackCacheByAssignment: %v", err)
	}
	if got.CacheID != fc.CacheID {
		t.Errorf("cache_id: got %q, want %q", got.CacheID, fc.CacheID)
	}
	if got.AssignmentID != a.ID {
		t.Errorf("assignment_id: got %q, want %q", got.AssignmentID, a.ID)
	}
	if got.SessionID != "fc-sess-1" {
		t.Errorf("session_id: got %q, want %q", got.SessionID, "fc-sess-1")
	}
	if got.TaskID != "FC-TASK-001" {
		t.Errorf("task_id: got %q, want %q", got.TaskID, "FC-TASK-001")
	}
	if got.CISnapshot != "ci-snap-v1" {
		t.Errorf("ci_snapshot: got %q, want %q", got.CISnapshot, "ci-snap-v1")
	}
	if got.CoderabbitSnapshot != "cr-snap-v1" {
		t.Errorf("coderabbit_snapshot: got %q, want %q", got.CoderabbitSnapshot, "cr-snap-v1")
	}
	if got.HumanReviewSnapshot != "hr-snap-v1" {
		t.Errorf("human_review_snapshot: got %q, want %q", got.HumanReviewSnapshot, "hr-snap-v1")
	}
	if got.ComplianceSnapshot != "comp-snap-v1" {
		t.Errorf("compliance_snapshot: got %q, want %q", got.ComplianceSnapshot, "comp-snap-v1")
	}
	if got.ContextBlock != "ctx-block-v1" {
		t.Errorf("context_block: got %q, want %q", got.ContextBlock, "ctx-block-v1")
	}
	if got.CacheHash != "hash-v1" {
		t.Errorf("cache_hash: got %q, want %q", got.CacheHash, "hash-v1")
	}
	if got.SourceStatus != `{"ci":"green"}` {
		t.Errorf("source_status: got %q, want %q", got.SourceStatus, `{"ci":"green"}`)
	}
	if got.SnapshotAt.IsZero() {
		t.Error("snapshot_at should be set")
	}
	firstSnapshotAt := got.SnapshotAt

	// Update the same row via upsert (same assignment_id).
	fc.CISnapshot = "ci-snap-v2"
	fc.CacheHash = "hash-v2"
	fc.SourceStatus = `{"ci":"red"}`
	if err := UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache update: %v", err)
	}

	got2, err := GetFeedbackCacheByAssignment(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("GetFeedbackCacheByAssignment after update: %v", err)
	}
	if got2.CISnapshot != "ci-snap-v2" {
		t.Errorf("ci_snapshot after update: got %q, want %q", got2.CISnapshot, "ci-snap-v2")
	}
	if got2.CacheHash != "hash-v2" {
		t.Errorf("cache_hash after update: got %q, want %q", got2.CacheHash, "hash-v2")
	}
	if got2.SourceStatus != `{"ci":"red"}` {
		t.Errorf("source_status after update: got %q, want %q", got2.SourceStatus, `{"ci":"red"}`)
	}
	if got2.SnapshotAt.Before(firstSnapshotAt) {
		t.Errorf("snapshot_at should not regress: got %v, first was %v", got2.SnapshotAt, firstSnapshotAt)
	}
}

func TestGetFeedbackCacheByAssignment_NotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := GetFeedbackCacheByAssignment(ctx, db, "nonexistent-assignment")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrFeedbackCacheNotFound) {
		t.Fatalf("expected ErrFeedbackCacheNotFound, got %v", err)
	}
}

func TestGetFeedbackCacheByTaskID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "fc-sess-2", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	a, err := AcceptTask(ctx, db, "fc-sess-2", "FC-TASK-002")
	if err != nil {
		t.Fatalf("AcceptTask: %v", err)
	}

	fc := &FeedbackCache{
		AssignmentID: a.ID,
		SessionID:    "fc-sess-2",
		TaskID:       "FC-TASK-002",
		CacheHash:    "hash-task-lookup",
		SourceStatus: "{}",
	}
	if err := UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache: %v", err)
	}

	got, err := GetFeedbackCacheByTaskID(ctx, db, "FC-TASK-002")
	if err != nil {
		t.Fatalf("GetFeedbackCacheByTaskID: %v", err)
	}
	if got.AssignmentID != a.ID {
		t.Errorf("assignment_id: got %q, want %q", got.AssignmentID, a.ID)
	}
	if got.TaskID != "FC-TASK-002" {
		t.Errorf("task_id: got %q, want %q", got.TaskID, "FC-TASK-002")
	}
	if got.CacheHash != "hash-task-lookup" {
		t.Errorf("cache_hash: got %q, want %q", got.CacheHash, "hash-task-lookup")
	}

	// Not-found case.
	_, err = GetFeedbackCacheByTaskID(ctx, db, "nonexistent-task")
	if !errors.Is(err, ErrFeedbackCacheNotFound) {
		t.Fatalf("expected ErrFeedbackCacheNotFound for missing task, got %v", err)
	}
}

func TestInvalidateFeedbackCache(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "fc-sess-3", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	a, err := AcceptTask(ctx, db, "fc-sess-3", "FC-TASK-003")
	if err != nil {
		t.Fatalf("AcceptTask: %v", err)
	}

	fc := &FeedbackCache{
		AssignmentID: a.ID,
		SessionID:    "fc-sess-3",
		TaskID:       "FC-TASK-003",
		CacheHash:    "hash-invalidate",
		SourceStatus: "{}",
	}
	if err := UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache: %v", err)
	}

	// Verify it exists.
	if _, err := GetFeedbackCacheByAssignment(ctx, db, a.ID); err != nil {
		t.Fatalf("GetFeedbackCacheByAssignment before invalidate: %v", err)
	}

	// Invalidate it.
	if err := InvalidateFeedbackCache(ctx, db, a.ID); err != nil {
		t.Fatalf("InvalidateFeedbackCache: %v", err)
	}

	// Verify it's gone.
	_, err = GetFeedbackCacheByAssignment(ctx, db, a.ID)
	if !errors.Is(err, ErrFeedbackCacheNotFound) {
		t.Fatalf("expected ErrFeedbackCacheNotFound after invalidate, got %v", err)
	}

	// Invalidating a nonexistent row should be a no-op (no error).
	if err := InvalidateFeedbackCache(ctx, db, "nonexistent-assignment"); err != nil {
		t.Fatalf("InvalidateFeedbackCache on nonexistent should not error: %v", err)
	}
}
