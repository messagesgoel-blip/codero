package webhook

import (
	"context"
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
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewed)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	payload := map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
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
	if rec.State != state.StateClosed {
		t.Errorf("expected closed, got %s", rec.State)
	}
}

func TestEventProcessor_PullRequest_Synchronize(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewed)
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
	if rec.State != state.StateStaleBranch {
		t.Errorf("expected stale_branch, got %s", rec.State)
	}
	if rec.HeadHash != "newhash456" {
		t.Errorf("head_hash: want newhash456, got %s", rec.HeadHash)
	}
}

func TestEventProcessor_PRReview_Approved(t *testing.T) {
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewed)
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
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewed)
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
	db, _ := setupProcessorDB(t, "owner/repo", "feat", state.StateReviewed)
	stream := setupStream(t, db)
	proc := NewEventProcessor(db, stream)

	ev := makeEvent("push", "owner/repo", map[string]any{})
	if err := proc.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
}
