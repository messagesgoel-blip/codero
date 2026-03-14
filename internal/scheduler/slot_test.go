package scheduler

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/redis"
)

func testRedisClientStall(t *testing.T) *redis.Client {
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

func TestStallDetectorEmptyQueue(t *testing.T) {
	client := testRedisClientStall(t)
	q := NewQueue(client)

	// Create detector with nil db (won't be called for empty queue)
	sd := NewStallDetector(q, nil)

	ctx := context.Background()
	status, err := sd.CheckStalled(ctx, "test")
	if err != nil {
		t.Fatalf("CheckStalled failed: %v", err)
	}

	if !status.QueueEmpty {
		t.Error("expected empty queue")
	}
	if status.IsStalled {
		t.Error("empty queue should not be stalled")
	}
	if status.TotalItems != 0 {
		t.Errorf("expected 0 items, got %d", status.TotalItems)
	}
}

func TestSlotCounterAcquireRelease(t *testing.T) {
	client := testRedisClientStall(t)
	sc := NewSlotCounter(client)

	ctx := context.Background()

	// Initially 0
	count, err := sc.GetSlotCount(ctx, "test-repo")
	if err != nil {
		t.Fatalf("GetSlotCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	// Acquire slot (limit 2)
	acquired, err := sc.AcquireSlot(ctx, "test-repo", 2)
	if err != nil {
		t.Fatalf("AcquireSlot failed: %v", err)
	}
	if !acquired {
		t.Error("expected slot to be acquired")
	}

	count, err = sc.GetSlotCount(ctx, "test-repo")
	if err != nil {
		t.Fatalf("GetSlotCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}

	// Acquire second slot
	acquired, err = sc.AcquireSlot(ctx, "test-repo", 2)
	if err != nil {
		t.Fatalf("AcquireSlot failed: %v", err)
	}
	if !acquired {
		t.Error("expected second slot to be acquired")
	}

	// Try to acquire third (should fail)
	acquired, err = sc.AcquireSlot(ctx, "test-repo", 2)
	if err != nil {
		t.Fatalf("AcquireSlot failed: %v", err)
	}
	if acquired {
		t.Error("expected third slot to be rejected")
	}

	// Release one slot
	err = sc.ReleaseSlot(ctx, "test-repo")
	if err != nil {
		t.Fatalf("ReleaseSlot failed: %v", err)
	}

	count, err = sc.GetSlotCount(ctx, "test-repo")
	if err != nil {
		t.Fatalf("GetSlotCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 after release, got %d", count)
	}
}

func TestSlotCounterReset(t *testing.T) {
	client := testRedisClientStall(t)
	sc := NewSlotCounter(client)

	ctx := context.Background()

	// Acquire a slot
	_, err := sc.AcquireSlot(ctx, "test-repo", 5)
	if err != nil {
		t.Fatalf("AcquireSlot failed: %v", err)
	}

	// Reset
	err = sc.ResetSlotCount(ctx, "test-repo")
	if err != nil {
		t.Fatalf("ResetSlotCount failed: %v", err)
	}

	count, err := sc.GetSlotCount(ctx, "test-repo")
	if err != nil {
		t.Fatalf("GetSlotCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 after reset, got %d", count)
	}
}

func TestSlotCounterSetCount(t *testing.T) {
	client := testRedisClientStall(t)
	sc := NewSlotCounter(client)

	ctx := context.Background()

	// Set count directly
	err := sc.SetSlotCount(ctx, "test-repo", 5)
	if err != nil {
		t.Fatalf("SetSlotCount failed: %v", err)
	}

	count, err := sc.GetSlotCount(ctx, "test-repo")
	if err != nil {
		t.Fatalf("GetSlotCount failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}

	// Set to 0 should delete key
	err = sc.SetSlotCount(ctx, "test-repo", 0)
	if err != nil {
		t.Fatalf("SetSlotCount to 0 failed: %v", err)
	}

	count, err = sc.GetSlotCount(ctx, "test-repo")
	if err != nil {
		t.Fatalf("GetSlotCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 after set to 0, got %d", count)
	}
}
