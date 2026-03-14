package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/redis"
)

// testRedisClientQueue creates a miniredis server and returns a client connected to it.
func testRedisClientQueue(t *testing.T) *redis.Client {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.New(mr.Addr(), "")
	t.Cleanup(func() { client.Close() })

	return client
}

func TestQueueEnqueueDequeue(t *testing.T) {
	client := testRedisClientQueue(t)
	q := NewQueue(client)

	ctx := context.Background()

	// Enqueue entries with different priorities
	err := q.Enqueue(ctx, QueueEntry{
		Repo:     "test",
		Branch:   "branch1",
		Priority: 10.0,
	})
	if err != nil {
		t.Fatalf("Enqueue branch1 failed: %v", err)
	}

	err = q.Enqueue(ctx, QueueEntry{
		Repo:     "test",
		Branch:   "branch2",
		Priority: 5.0, // higher priority (lower score)
	})
	if err != nil {
		t.Fatalf("Enqueue branch2 failed: %v", err)
	}

	// Dequeue should return highest priority (lowest score)
	entry, err := q.Dequeue(ctx, "test")
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if entry.Branch != "branch2" {
		t.Errorf("expected branch2 (higher priority), got %q", entry.Branch)
	}

	// Next dequeue should return branch1
	entry, err = q.Dequeue(ctx, "test")
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if entry.Branch != "branch1" {
		t.Errorf("expected branch1, got %q", entry.Branch)
	}

	// Queue should now be empty
	_, err = q.Dequeue(ctx, "test")
	if !errors.Is(err, ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestQueueAlreadyEnqueued(t *testing.T) {
	client := testRedisClientQueue(t)
	q := NewQueue(client)

	ctx := context.Background()

	err := q.Enqueue(ctx, QueueEntry{
		Repo:     "test",
		Branch:   "branch1",
		Priority: 10.0,
	})
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Enqueue same branch again
	err = q.Enqueue(ctx, QueueEntry{
		Repo:     "test",
		Branch:   "branch1",
		Priority: 5.0,
	})
	if !errors.Is(err, ErrAlreadyEnqueued) {
		t.Errorf("expected ErrAlreadyEnqueued, got %v", err)
	}
}

func TestQueuePeek(t *testing.T) {
	client := testRedisClientQueue(t)
	q := NewQueue(client)

	ctx := context.Background()

	// Peek on empty queue
	_, err := q.Peek(ctx, "test")
	if !errors.Is(err, ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}

	// Enqueue
	err = q.Enqueue(ctx, QueueEntry{
		Repo:     "test",
		Branch:   "branch1",
		Priority: 10.0,
	})
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Peek should return entry without removing
	entry, err := q.Peek(ctx, "test")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if entry.Branch != "branch1" {
		t.Errorf("expected branch1, got %q", entry.Branch)
	}

	// Queue should still have the entry
	len, err := q.Len(ctx, "test")
	if err != nil {
		t.Fatalf("Len failed: %v", err)
	}
	if len != 1 {
		t.Errorf("expected length 1, got %d", len)
	}
}

func TestQueueRemove(t *testing.T) {
	client := testRedisClientQueue(t)
	q := NewQueue(client)

	ctx := context.Background()

	err := q.Enqueue(ctx, QueueEntry{
		Repo:     "test",
		Branch:   "branch1",
		Priority: 10.0,
	})
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Remove
	err = q.Remove(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Should be empty
	len, err := q.Len(ctx, "test")
	if err != nil {
		t.Fatalf("Len failed: %v", err)
	}
	if len != 0 {
		t.Errorf("expected length 0 after remove, got %d", len)
	}

	// Remove non-existent should succeed
	err = q.Remove(ctx, "test", "nonexistent")
	if err != nil {
		t.Errorf("Remove non-existent should not error: %v", err)
	}
}

func TestQueueList(t *testing.T) {
	client := testRedisClientQueue(t)
	q := NewQueue(client)

	ctx := context.Background()

	// Empty list
	entries, err := q.List(ctx, "test")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}

	// Add entries
	for i, priority := range []float64{30.0, 10.0, 20.0} {
		err := q.Enqueue(ctx, QueueEntry{
			Repo:     "test",
			Branch:   string(rune('a' + i)),
			Priority: QueuePriority(priority),
		})
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// List should return in priority order (lowest score first)
	entries, err = q.List(ctx, "test")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// 'b' has lowest priority score (10.0), should be first
	if entries[0].Branch != "b" {
		t.Errorf("expected 'b' first, got %q", entries[0].Branch)
	}
	if entries[0].Priority != 10.0 {
		t.Errorf("expected priority 10.0, got %v", entries[0].Priority)
	}
}

func TestQueueUpdatePriority(t *testing.T) {
	client := testRedisClientQueue(t)
	q := NewQueue(client)

	ctx := context.Background()

	err := q.Enqueue(ctx, QueueEntry{
		Repo:     "test",
		Branch:   "branch1",
		Priority: 10.0,
	})
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Update priority
	err = q.UpdatePriority(ctx, "test", "branch1", 1.0)
	if err != nil {
		t.Fatalf("UpdatePriority failed: %v", err)
	}

	// Check new priority
	entry, err := q.Peek(ctx, "test")
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}
	if entry.Priority != 1.0 {
		t.Errorf("expected priority 1.0, got %v", entry.Priority)
	}
}

func TestComputeWFQPriority(t *testing.T) {
	tests := []struct {
		vtime   float64
		weight  float64
		wantMin float64
		wantMax float64
	}{
		{0.0, 1.0, 0.9, 1.1},    // ~1.0
		{10.0, 1.0, 10.9, 11.1}, // ~11.0
		{0.0, 2.0, 0.4, 0.6},    // ~0.5 (higher weight = lower priority score = better)
		{5.0, 0.5, 6.9, 7.1},    // ~7.0 (lower weight = higher priority score = worse)
	}

	for _, tt := range tests {
		p := ComputeWFQPriority(tt.vtime, tt.weight)
		if p < QueuePriority(tt.wantMin) || p > QueuePriority(tt.wantMax) {
			t.Errorf("ComputeWFQPriority(%v, %v) = %v, want between %v and %v",
				tt.vtime, tt.weight, p, tt.wantMin, tt.wantMax)
		}
	}
}

func TestVirtualTime(t *testing.T) {
	client := testRedisClientQueue(t)
	vt := NewVirtualTime(client)

	ctx := context.Background()

	// Initial time is 0
	vtime, err := vt.Get(ctx, "test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if vtime != 0 {
		t.Errorf("expected initial time 0, got %v", vtime)
	}

	// Advance by weight 1.0 (delta = 1.0)
	err = vt.Advance(ctx, "test", 1.0)
	if err != nil {
		t.Fatalf("Advance failed: %v", err)
	}

	vtime, err = vt.Get(ctx, "test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if vtime != 1.0 {
		t.Errorf("expected time 1.0, got %v", vtime)
	}

	// Advance by weight 2.0 (delta = 0.5)
	err = vt.Advance(ctx, "test", 2.0)
	if err != nil {
		t.Fatalf("Advance failed: %v", err)
	}

	vtime, err = vt.Get(ctx, "test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if vtime != 1.5 {
		t.Errorf("expected time 1.5, got %v", vtime)
	}

	// Reset
	err = vt.Reset(ctx, "test")
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	vtime, err = vt.Get(ctx, "test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if vtime != 0 {
		t.Errorf("expected time 0 after reset, got %v", vtime)
	}
}

func TestComputeAgingPriority(t *testing.T) {
	base := QueuePriority(10.0)
	waitTime := 100 * time.Second
	agingFactor := 0.01

	p := ComputeAgingPriority(base, waitTime, agingFactor)

	// Aging should reduce priority score (improve scheduling)
	// 10.0 - (100 * 0.01) = 9.0
	expected := QueuePriority(9.0)
	if p != expected {
		t.Errorf("ComputeAgingPriority = %v, want %v", p, expected)
	}
}

func TestClassifyWeight(t *testing.T) {
	tests := []struct {
		branch string
		want   float64
	}{
		{"hotfix/urgent-fix", WeightHotfix},
		{"urgent/security-patch", WeightHotfix},
		{"critical/db-fix", WeightHotfix},
		{"release/v1.0.0", WeightRelease},
		{"rel/v2.0.0", WeightRelease},
		{"experiment/new-feature", WeightExperimental},
		{"exp/test-idea", WeightExperimental},
		{"feature/new-feature", WeightDefault},
		{"main", WeightDefault},
		{"develop", WeightDefault},
	}

	for _, tt := range tests {
		got := ClassifyWeight(tt.branch)
		if got != tt.want {
			t.Errorf("ClassifyWeight(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

func TestQueueLen(t *testing.T) {
	client := testRedisClientQueue(t)
	q := NewQueue(client)

	ctx := context.Background()

	len, err := q.Len(ctx, "test")
	if err != nil {
		t.Fatalf("Len failed: %v", err)
	}
	if len != 0 {
		t.Errorf("expected length 0, got %d", len)
	}

	for i := 0; i < 5; i++ {
		err := q.Enqueue(ctx, QueueEntry{
			Repo:     "test",
			Branch:   string(rune('a' + i)),
			Priority: QueuePriority(i),
		})
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	len, err = q.Len(ctx, "test")
	if err != nil {
		t.Fatalf("Len failed: %v", err)
	}
	if len != 5 {
		t.Errorf("expected length 5, got %d", len)
	}
}
