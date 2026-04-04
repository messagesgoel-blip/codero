package state

import (
	"context"
	"errors"
	"testing"
)

func TestCreateSubmission_Persists(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer db.Close()

	rec := SubmissionRecord{
		SubmissionID:  "sub-001",
		AssignmentID:  "assign-001",
		SessionID:     "sess-001",
		Repo:          "owner/repo",
		Branch:        "feat/test",
		HeadSHA:       "abc123def456",
		DiffHash:      "hash123",
		AttemptLocal:  0,
		AttemptRemote: 0,
		State:         "submitted",
		Result:        "",
	}

	if err := CreateSubmission(ctx, db, rec); err != nil {
		t.Fatalf("CreateSubmission failed: %v", err)
	}

	// Query back
	subs, err := GetSubmissionsByBranch(ctx, db, "owner/repo", "feat/test")
	if err != nil {
		t.Fatalf("GetSubmissionsByBranch failed: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(subs))
	}

	got := subs[0]
	if got.SubmissionID != "sub-001" {
		t.Errorf("SubmissionID = %q, want %q", got.SubmissionID, "sub-001")
	}
	if got.AssignmentID != "assign-001" {
		t.Errorf("AssignmentID = %q, want %q", got.AssignmentID, "assign-001")
	}
	if got.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "sess-001")
	}
	if got.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", got.Repo, "owner/repo")
	}
	if got.Branch != "feat/test" {
		t.Errorf("Branch = %q, want %q", got.Branch, "feat/test")
	}
	if got.HeadSHA != "abc123def456" {
		t.Errorf("HeadSHA = %q, want %q", got.HeadSHA, "abc123def456")
	}
	if got.DiffHash != "hash123" {
		t.Errorf("DiffHash = %q, want %q", got.DiffHash, "hash123")
	}
	if got.State != "submitted" {
		t.Errorf("State = %q, want %q", got.State, "submitted")
	}
}

func TestCreateSubmission_Duplicate(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer db.Close()

	rec := SubmissionRecord{
		SubmissionID: "sub-001",
		AssignmentID: "assign-001", // non-empty triggers dedup index
		SessionID:    "sess-001",
		Repo:         "owner/repo",
		Branch:       "feat/test",
		HeadSHA:      "abc123",
		DiffHash:     "hash123",
		State:        "submitted",
	}

	if err := CreateSubmission(ctx, db, rec); err != nil {
		t.Fatalf("first CreateSubmission failed: %v", err)
	}

	// Second insert with same (assignment_id, diff_hash, head_sha)
	rec2 := SubmissionRecord{
		SubmissionID: "sub-002", // different ID
		AssignmentID: "assign-001",
		SessionID:    "sess-001",
		Repo:         "owner/repo",
		Branch:       "feat/test",
		HeadSHA:      "abc123",  // same
		DiffHash:     "hash123", // same
		State:        "submitted",
	}

	err := CreateSubmission(ctx, db, rec2)
	if !errors.Is(err, ErrDuplicateSubmission) {
		t.Errorf("expected ErrDuplicateSubmission, got %v", err)
	}
}

func TestCreateSubmission_DifferentDiff(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer db.Close()

	rec1 := SubmissionRecord{
		SubmissionID: "sub-001",
		AssignmentID: "assign-001",
		SessionID:    "sess-001",
		Repo:         "owner/repo",
		Branch:       "feat/test",
		HeadSHA:      "abc123",
		DiffHash:     "hash-AAA",
		State:        "submitted",
	}

	rec2 := SubmissionRecord{
		SubmissionID: "sub-002",
		AssignmentID: "assign-001",
		SessionID:    "sess-001",
		Repo:         "owner/repo",
		Branch:       "feat/test",
		HeadSHA:      "abc123",
		DiffHash:     "hash-BBB", // different diff
		State:        "submitted",
	}

	if err := CreateSubmission(ctx, db, rec1); err != nil {
		t.Fatalf("first CreateSubmission failed: %v", err)
	}
	if err := CreateSubmission(ctx, db, rec2); err != nil {
		t.Fatalf("second CreateSubmission should succeed with different diff: %v", err)
	}

	subs, err := GetSubmissionsByBranch(ctx, db, "owner/repo", "feat/test")
	if err != nil {
		t.Fatalf("GetSubmissionsByBranch failed: %v", err)
	}
	if len(subs) != 2 {
		t.Errorf("expected 2 submissions, got %d", len(subs))
	}
}

func TestCreateSubmission_NoAssignment(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer db.Close()

	// When assignment_id is empty, the partial unique index doesn't apply
	rec1 := SubmissionRecord{
		SubmissionID: "sub-001",
		AssignmentID: "", // empty
		SessionID:    "",
		Repo:         "owner/repo",
		Branch:       "feat/test",
		HeadSHA:      "abc123",
		DiffHash:     "hash123",
		State:        "submitted",
	}

	rec2 := SubmissionRecord{
		SubmissionID: "sub-002",
		AssignmentID: "", // empty
		SessionID:    "",
		Repo:         "owner/repo",
		Branch:       "feat/test",
		HeadSHA:      "abc123",  // same
		DiffHash:     "hash123", // same
		State:        "submitted",
	}

	if err := CreateSubmission(ctx, db, rec1); err != nil {
		t.Fatalf("first CreateSubmission failed: %v", err)
	}
	// This should NOT trigger duplicate error since assignment_id is empty
	if err := CreateSubmission(ctx, db, rec2); err != nil {
		t.Fatalf("second CreateSubmission with empty assignment_id should succeed: %v", err)
	}

	subs, err := GetSubmissionsByBranch(ctx, db, "owner/repo", "feat/test")
	if err != nil {
		t.Fatalf("GetSubmissionsByBranch failed: %v", err)
	}
	if len(subs) != 2 {
		t.Errorf("expected 2 submissions, got %d", len(subs))
	}
}

func TestUpdateSubmissionState(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer db.Close()

	rec := SubmissionRecord{
		SubmissionID: "sub-001",
		AssignmentID: "assign-001",
		SessionID:    "sess-001",
		Repo:         "owner/repo",
		Branch:       "feat/test",
		HeadSHA:      "abc123",
		DiffHash:     "hash123",
		State:        "submitted",
		Result:       "",
	}

	if err := CreateSubmission(ctx, db, rec); err != nil {
		t.Fatalf("CreateSubmission failed: %v", err)
	}

	// Update state
	if err := UpdateSubmissionState(ctx, db, "sub-001", "merged", "PR merged successfully"); err != nil {
		t.Fatalf("UpdateSubmissionState failed: %v", err)
	}

	got, err := GetSubmissionByID(ctx, db, "sub-001")
	if err != nil {
		t.Fatalf("GetSubmissionByID failed: %v", err)
	}
	if got.State != "merged" {
		t.Errorf("State = %q, want %q", got.State, "merged")
	}
	if got.Result != "PR merged successfully" {
		t.Errorf("Result = %q, want %q", got.Result, "PR merged successfully")
	}
}

func TestGetSubmissionByID_NotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer db.Close()

	got, err := GetSubmissionByID(ctx, db, "nonexistent")
	if err != nil {
		t.Fatalf("GetSubmissionByID should not error for missing: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing submission, got %+v", got)
	}
}

func TestSubmissionCountForBranch(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer db.Close()

	// Initially zero
	count, err := SubmissionCountForBranch(ctx, db, "owner/repo", "feat/test")
	if err != nil {
		t.Fatalf("SubmissionCountForBranch failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	// Add some submissions
	for i, id := range []string{"sub-001", "sub-002", "sub-003"} {
		rec := SubmissionRecord{
			SubmissionID: id,
			AssignmentID: "",
			Repo:         "owner/repo",
			Branch:       "feat/test",
			HeadSHA:      "sha" + id,
			DiffHash:     "hash" + id,
			State:        "submitted",
		}
		if err := CreateSubmission(ctx, db, rec); err != nil {
			t.Fatalf("CreateSubmission %d failed: %v", i, err)
		}
	}

	count, err = SubmissionCountForBranch(ctx, db, "owner/repo", "feat/test")
	if err != nil {
		t.Fatalf("SubmissionCountForBranch failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}

func TestLatestSubmissionForBranch(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	defer db.Close()

	// Initially empty
	latest, err := LatestSubmissionForBranch(ctx, db, "owner/repo", "feat/test")
	if err != nil {
		t.Fatalf("LatestSubmissionForBranch failed: %v", err)
	}
	if latest != "" {
		t.Errorf("expected empty, got %q", latest)
	}

	// Add submission
	rec := SubmissionRecord{
		SubmissionID: "sub-latest",
		AssignmentID: "",
		Repo:         "owner/repo",
		Branch:       "feat/test",
		HeadSHA:      "abc",
		DiffHash:     "hash",
		State:        "submitted",
	}
	if err := CreateSubmission(ctx, db, rec); err != nil {
		t.Fatalf("CreateSubmission failed: %v", err)
	}

	latest, err = LatestSubmissionForBranch(ctx, db, "owner/repo", "feat/test")
	if err != nil {
		t.Fatalf("LatestSubmissionForBranch failed: %v", err)
	}
	if latest != "sub-latest" {
		t.Errorf("expected %q, got %q", "sub-latest", latest)
	}
}
