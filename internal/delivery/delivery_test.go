package delivery_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/delivery"
	"github.com/codero/codero/internal/normalizer"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/state"
)

func setupStream(t *testing.T) (*delivery.Stream, *state.DB, *miniredis.Miniredis) {
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

	return delivery.NewStream(db, client), db, mr
}

const (
	testRepo   = "owner/repo"
	testBranch = "main"
	testHead   = "abc123"
)

func TestStream_AppendAndReplay(t *testing.T) {
	stream, _, _ := setupStream(t)
	ctx := context.Background()

	seq1, err := stream.AppendSystem(ctx, testRepo, testBranch, testHead, "test", "first event")
	if err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if seq1 != 1 {
		t.Errorf("seq1: got %d, want 1", seq1)
	}

	seq2, err := stream.AppendSystem(ctx, testRepo, testBranch, testHead, "test", "second event")
	if err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if seq2 != 2 {
		t.Errorf("seq2: got %d, want 2", seq2)
	}

	// Replay all events (sinceSeq=0).
	events, err := stream.Replay(ctx, testRepo, testBranch, 0)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("replay: got %d events, want 2", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Errorf("seq order: got %d, %d; want 1, 2", events[0].Seq, events[1].Seq)
	}
}

func TestStream_ReplaySinceSeq(t *testing.T) {
	stream, _, _ := setupStream(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := stream.AppendSystem(ctx, testRepo, testBranch, testHead, "test", "event"); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Replay events after seq 3.
	events, err := stream.Replay(ctx, testRepo, testBranch, 3)
	if err != nil {
		t.Fatalf("replay since 3: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events since seq 3, got %d", len(events))
	}
	if events[0].Seq != 4 || events[1].Seq != 5 {
		t.Errorf("expected seq 4,5; got %d,%d", events[0].Seq, events[1].Seq)
	}
}

func TestStream_ReplayIdempotent(t *testing.T) {
	stream, _, _ := setupStream(t)
	ctx := context.Background()

	if _, err := stream.AppendSystem(ctx, testRepo, testBranch, testHead, "test", "event"); err != nil {
		t.Fatalf("append: %v", err)
	}

	events1, err := stream.Replay(ctx, testRepo, testBranch, 0)
	if err != nil {
		t.Fatalf("replay 1: %v", err)
	}
	events2, err := stream.Replay(ctx, testRepo, testBranch, 0)
	if err != nil {
		t.Fatalf("replay 2: %v", err)
	}
	if len(events1) != len(events2) {
		t.Errorf("idempotent: got %d then %d events", len(events1), len(events2))
	}
}

func TestStream_MonotonicSeq(t *testing.T) {
	stream, _, _ := setupStream(t)
	ctx := context.Background()

	const n = 10
	prev := int64(0)
	for i := 0; i < n; i++ {
		seq, err := stream.AppendSystem(ctx, testRepo, testBranch, testHead, "test", "event")
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if seq <= prev {
			t.Errorf("seq not monotonic: got %d after %d", seq, prev)
		}
		prev = seq
	}
}

func TestStream_AppendFindingBundle(t *testing.T) {
	stream, _, _ := setupStream(t)
	ctx := context.Background()

	payload := delivery.FindingBundlePayload{
		RunID:    "run-123",
		Provider: "stub",
		Findings: []normalizer.Finding{
			{
				Severity: normalizer.SeverityWarning,
				Category: "security",
				File:     "main.go",
				Line:     10,
				Message:  "test finding",
				Source:   "stub",
			},
		},
	}

	seq, err := stream.AppendFindingBundle(ctx, testRepo, testBranch, testHead, payload)
	if err != nil {
		t.Fatalf("append finding bundle: %v", err)
	}
	if seq != 1 {
		t.Errorf("seq: got %d, want 1", seq)
	}

	events, err := stream.Replay(ctx, testRepo, testBranch, 0)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != "finding_bundle" {
		t.Errorf("event_type: got %q, want %q", events[0].EventType, "finding_bundle")
	}
}

func TestStream_InitSeqFloor_PreventsRegression(t *testing.T) {
	stream, db, mr := setupStream(t)
	ctx := context.Background()

	// Append some events to build up seq.
	for i := 0; i < 5; i++ {
		if _, err := stream.AppendSystem(ctx, testRepo, testBranch, testHead, "test", "event"); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Flush Redis (simulates Redis restart).
	mr.FlushAll()

	// InitSeqFloor should recover the counter from durable store.
	if err := stream.InitSeqFloor(ctx, testRepo, testBranch); err != nil {
		t.Fatalf("InitSeqFloor: %v", err)
	}

	// Next append should have seq > 5.
	seq, err := stream.AppendSystem(ctx, testRepo, testBranch, testHead, "test", "after restart")
	if err != nil {
		t.Fatalf("append after restart: %v", err)
	}
	if seq <= 5 {
		t.Errorf("seq after Redis restart: got %d, want > 5 (no regression)", seq)
	}

	// Verify all events are accessible via replay from the DB.
	events, err := stream.Replay(ctx, testRepo, testBranch, 0)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	_ = db
	if len(events) < 6 {
		t.Errorf("expected >=6 events in replay, got %d", len(events))
	}
}

func TestStream_EmptyReplay(t *testing.T) {
	stream, _, _ := setupStream(t)
	ctx := context.Background()

	events, err := stream.Replay(ctx, testRepo, testBranch, 0)
	if err != nil {
		t.Fatalf("replay on empty stream: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events on empty stream, got %d", len(events))
	}
}

func TestStream_CurrentSeq(t *testing.T) {
	stream, _, _ := setupStream(t)
	ctx := context.Background()

	seq0, err := stream.CurrentSeq(ctx, testRepo, testBranch)
	if err != nil {
		t.Fatalf("CurrentSeq on empty: %v", err)
	}
	if seq0 != 0 {
		t.Errorf("empty stream: got seq %d, want 0", seq0)
	}

	if _, err := stream.AppendSystem(ctx, testRepo, testBranch, testHead, "test", "event"); err != nil {
		t.Fatalf("append: %v", err)
	}

	seq1, err := stream.CurrentSeq(ctx, testRepo, testBranch)
	if err != nil {
		t.Fatalf("CurrentSeq after append: %v", err)
	}
	if seq1 != 1 {
		t.Errorf("after 1 append: got seq %d, want 1", seq1)
	}
}
