package state

import (
	"context"
	"errors"
	"testing"
)

func TestUpsertGitHubLink_InsertAndUpdate(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	link := &GitHubLink{
		TaskID:       "TASK-100",
		RepoFullName: "acme/api",
		PRNumber:     42,
		IssueNumber:  10,
		BranchName:   "feat/stuff",
		HeadSHA:      "abc123",
		PRState:      "open",
		LastCIRunID:  "run-1",
	}

	// Insert
	if err := UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink (insert): %v", err)
	}
	if link.LinkID == "" {
		t.Fatal("expected LinkID to be generated, got empty")
	}
	if link.LastSyncedAt == nil {
		t.Fatal("expected LastSyncedAt to be set, got nil")
	}

	got, err := GetLinkByTaskID(ctx, db, "TASK-100")
	if err != nil {
		t.Fatalf("GetLinkByTaskID after insert: %v", err)
	}
	if got.LinkID != link.LinkID {
		t.Errorf("LinkID: got %q, want %q", got.LinkID, link.LinkID)
	}
	if got.RepoFullName != "acme/api" {
		t.Errorf("RepoFullName: got %q, want %q", got.RepoFullName, "acme/api")
	}
	if got.PRNumber != 42 {
		t.Errorf("PRNumber: got %d, want 42", got.PRNumber)
	}
	if got.IssueNumber != 10 {
		t.Errorf("IssueNumber: got %d, want 10", got.IssueNumber)
	}
	if got.BranchName != "feat/stuff" {
		t.Errorf("BranchName: got %q, want %q", got.BranchName, "feat/stuff")
	}
	if got.HeadSHA != "abc123" {
		t.Errorf("HeadSHA: got %q, want %q", got.HeadSHA, "abc123")
	}
	if got.PRState != "open" {
		t.Errorf("PRState: got %q, want %q", got.PRState, "open")
	}
	if got.LastCIRunID != "run-1" {
		t.Errorf("LastCIRunID: got %q, want %q", got.LastCIRunID, "run-1")
	}
	if got.LastSyncedAt == nil {
		t.Fatal("expected LastSyncedAt to be non-nil after insert")
	}

	// Update via upsert (same task_id, different fields)
	link.PRNumber = 99
	link.HeadSHA = "def456"
	link.PRState = "merged"

	if err := UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink (update): %v", err)
	}

	got2, err := GetLinkByTaskID(ctx, db, "TASK-100")
	if err != nil {
		t.Fatalf("GetLinkByTaskID after update: %v", err)
	}
	if got2.PRNumber != 99 {
		t.Errorf("PRNumber after update: got %d, want 99", got2.PRNumber)
	}
	if got2.HeadSHA != "def456" {
		t.Errorf("HeadSHA after update: got %q, want %q", got2.HeadSHA, "def456")
	}
	if got2.PRState != "merged" {
		t.Errorf("PRState after update: got %q, want %q", got2.PRState, "merged")
	}
}

func TestGetLinkByTaskID_NotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := GetLinkByTaskID(ctx, db, "nonexistent-task")
	if err == nil {
		t.Fatal("expected error for nonexistent task_id, got nil")
	}
	if !errors.Is(err, ErrGitHubLinkNotFound) {
		t.Errorf("expected ErrGitHubLinkNotFound, got: %v", err)
	}
}

func TestGetLinkByRepoPR(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	link := &GitHubLink{
		TaskID:       "TASK-200",
		RepoFullName: "acme/web",
		PRNumber:     55,
		BranchName:   "feat/pr-lookup",
	}
	if err := UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}

	got, err := GetLinkByRepoPR(ctx, db, "acme/web", 55)
	if err != nil {
		t.Fatalf("GetLinkByRepoPR: %v", err)
	}
	if got.TaskID != "TASK-200" {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, "TASK-200")
	}
	if got.PRNumber != 55 {
		t.Errorf("PRNumber: got %d, want 55", got.PRNumber)
	}

	// Not found case
	_, err = GetLinkByRepoPR(ctx, db, "acme/web", 999)
	if !errors.Is(err, ErrGitHubLinkNotFound) {
		t.Errorf("expected ErrGitHubLinkNotFound for bad PR, got: %v", err)
	}
}

func TestGetLinkByBranch(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	link := &GitHubLink{
		TaskID:       "TASK-300",
		RepoFullName: "acme/lib",
		BranchName:   "feat/branch-lookup",
	}
	if err := UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}

	got, err := GetLinkByBranch(ctx, db, "acme/lib", "feat/branch-lookup")
	if err != nil {
		t.Fatalf("GetLinkByBranch: %v", err)
	}
	if got.TaskID != "TASK-300" {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, "TASK-300")
	}
	if got.BranchName != "feat/branch-lookup" {
		t.Errorf("BranchName: got %q, want %q", got.BranchName, "feat/branch-lookup")
	}

	// Not found case
	_, err = GetLinkByBranch(ctx, db, "acme/lib", "nonexistent-branch")
	if !errors.Is(err, ErrGitHubLinkNotFound) {
		t.Errorf("expected ErrGitHubLinkNotFound for bad branch, got: %v", err)
	}
}

func TestUpdateLinkPRState(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	link := &GitHubLink{
		TaskID:       "TASK-400",
		RepoFullName: "acme/svc",
		PRNumber:     10,
		PRState:      "open",
	}
	if err := UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}

	if err := UpdateLinkPRState(ctx, db, link.LinkID, "closed"); err != nil {
		t.Fatalf("UpdateLinkPRState: %v", err)
	}

	got, err := GetLinkByTaskID(ctx, db, "TASK-400")
	if err != nil {
		t.Fatalf("GetLinkByTaskID: %v", err)
	}
	if got.PRState != "closed" {
		t.Errorf("PRState: got %q, want %q", got.PRState, "closed")
	}
	if got.LastSyncedAt == nil {
		t.Error("expected LastSyncedAt to be updated")
	}

	// Not found case
	err = UpdateLinkPRState(ctx, db, "nonexistent-link", "open")
	if !errors.Is(err, ErrGitHubLinkNotFound) {
		t.Errorf("expected ErrGitHubLinkNotFound, got: %v", err)
	}
}

func TestUpdateLinkHeadSHA(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	link := &GitHubLink{
		TaskID:       "TASK-500",
		RepoFullName: "acme/mono",
		HeadSHA:      "sha-old",
	}
	if err := UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}

	if err := UpdateLinkHeadSHA(ctx, db, link.LinkID, "sha-new"); err != nil {
		t.Fatalf("UpdateLinkHeadSHA: %v", err)
	}

	got, err := GetLinkByTaskID(ctx, db, "TASK-500")
	if err != nil {
		t.Fatalf("GetLinkByTaskID: %v", err)
	}
	if got.HeadSHA != "sha-new" {
		t.Errorf("HeadSHA: got %q, want %q", got.HeadSHA, "sha-new")
	}
	if got.LastSyncedAt == nil {
		t.Error("expected LastSyncedAt to be updated")
	}

	// Not found case
	err = UpdateLinkHeadSHA(ctx, db, "nonexistent-link", "sha-x")
	if !errors.Is(err, ErrGitHubLinkNotFound) {
		t.Errorf("expected ErrGitHubLinkNotFound, got: %v", err)
	}
}
