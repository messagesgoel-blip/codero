package adapters_test

import (
	"testing"
	"time"

	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui/adapters"
)

func TestFromBranchRecord(t *testing.T) {
	now := time.Now()
	r := state.BranchRecord{
		ID:             "id1",
		Repo:           "codero/codero",
		Branch:         "feat/COD-001-foo",
		HeadHash:       "abcdef1234567890",
		State:          state.StateQueuedCLI,
		RetryCount:     1,
		MaxRetries:     3,
		Approved:       true,
		CIGreen:        false,
		QueuePriority:  5,
		SubmissionTime: &now,
	}
	item := adapters.FromBranchRecord(r)
	if item.ID != "id1" {
		t.Errorf("ID: want id1, got %q", item.ID)
	}
	if item.HeadHash != "abcdef12" {
		t.Errorf("HeadHash: want abcdef12, got %q", item.HeadHash)
	}
	if item.State != string(state.StateQueuedCLI) {
		t.Errorf("State: want %q, got %q", state.StateQueuedCLI, item.State)
	}
	if !item.Approved {
		t.Error("expected Approved=true")
	}
	if item.WaitingSec < 0 {
		t.Error("expected WaitingSec >= 0")
	}
}

func TestFromBranchRecords(t *testing.T) {
	records := []state.BranchRecord{
		{ID: "a", Repo: "r", Branch: "b1", State: state.StateSubmitted},
		{ID: "b", Repo: "r", Branch: "b2", State: state.StateMergeReady},
	}
	items := adapters.FromBranchRecords(records)
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	if items[0].Branch != "b1" || items[1].Branch != "b2" {
		t.Error("branch order mismatch")
	}
}

func TestFromBranchRecord_LongBranch(t *testing.T) {
	r := state.BranchRecord{
		Branch: "feat/COD-999-this-is-a-very-long-branch-name-that-exceeds-24-chars",
		State:  state.StateSubmitted,
	}
	item := adapters.FromBranchRecord(r)
	if item.Branch != r.Branch {
		t.Errorf("branch should be stored as-is in QueueItem.Branch: want %q, got %q", r.Branch, item.Branch)
	}
	// DisplayLine should have been truncated
	if len(item.DisplayLine) == 0 {
		t.Error("expected non-empty DisplayLine")
	}
}
