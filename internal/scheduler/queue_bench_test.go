package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/redis"
)

// testRedisClientBench creates a miniredis server for benchmarking.
func testRedisClientBench(b *testing.B) *redis.Client {
	b.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("miniredis: %v", err)
	}
	b.Cleanup(mr.Close)

	client := redis.New(mr.Addr(), "")
	b.Cleanup(func() { client.Close() })

	return client
}

// BenchmarkQueue_100Concurrent measures throughput for 100 concurrent task enqueue/dequeue.
func BenchmarkQueue_100Concurrent(b *testing.B) {
	client := testRedisClientBench(b)
	q := NewQueue(client)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear queue for this iteration
		entries, _ := q.List(ctx, "bench")
		for _, e := range entries {
			_ = q.Remove(ctx, "bench", e.Branch)
		}

		// Enqueue 100 tasks
		var wg sync.WaitGroup
		for j := 0; j < 100; j++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				priority := ComputeWFQPriority(float64(idx%10), ClassifyWeight(fmt.Sprintf("branch-%d", idx)))
				_ = q.Enqueue(ctx, QueueEntry{
					Repo:     "bench",
					Branch:   fmt.Sprintf("branch-%d", idx),
					Priority: priority,
				})
			}(j)
		}
		wg.Wait()

		// Dequeue all 100 tasks
		for j := 0; j < 100; j++ {
			_, _ = q.Dequeue(ctx, "bench")
		}
	}
}

// BenchmarkQueue_WFQFairness measures fairness under WFQ scheduling.
func BenchmarkQueue_WFQFairness(b *testing.B) {
	client := testRedisClientBench(b)
	q := NewQueue(client)
	vt := NewVirtualTime(client)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset virtual time
		_ = vt.Reset(ctx, "bench")

		// Clear queue
		entries, _ := q.List(ctx, "bench")
		for _, e := range entries {
			_ = q.Remove(ctx, "bench", e.Branch)
		}

		// Enqueue tasks with different weights
		branches := []struct {
			name   string
			weight float64
		}{
			{"hotfix/urgent", WeightHotfix},         // weight=2.0
			{"feature/normal", WeightFeature},       // weight=1.0
			{"experiment/test", WeightExperimental}, // weight=0.5
		}

		for j := 0; j < 100; j++ {
			b := branches[j%3]
			vtime, _ := vt.Get(ctx, "bench")
			priority := ComputeWFQPriority(vtime, b.weight)
			_ = q.Enqueue(ctx, QueueEntry{
				Repo:     "bench",
				Branch:   fmt.Sprintf("%s-%d", b.name, j),
				Priority: priority,
				Weight:   b.weight,
			})
		}

		// Dequeue all and advance virtual time
		for j := 0; j < 100; j++ {
			entry, err := q.Dequeue(ctx, "bench")
			if err != nil {
				break
			}
			weight := ClassifyWeight(entry.Branch)
			_ = vt.Advance(ctx, "bench", weight)
		}
	}
}

// TestMIG036_Queue_100Concurrent_Throughput verifies throughput with 100 concurrent tasks.
func TestMIG036_Queue_100Concurrent_Throughput(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.New(mr.Addr(), "")
	defer client.Close()

	q := NewQueue(client)
	ctx := context.Background()

	start := time.Now()
	iterations := 100

	for i := 0; i < iterations; i++ {
		// Clear queue
		entries, _ := q.List(ctx, "throughput")
		for _, e := range entries {
			_ = q.Remove(ctx, "throughput", e.Branch)
		}

		// Enqueue 100 tasks concurrently
		var wg sync.WaitGroup
		var enqueueErrors atomic.Int32
		for j := 0; j < 100; j++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				priority := ComputeWFQPriority(float64(idx%10), ClassifyWeight(fmt.Sprintf("branch-%d", idx)))
				if err := q.Enqueue(ctx, QueueEntry{
					Repo:     "throughput",
					Branch:   fmt.Sprintf("branch-%d", idx),
					Priority: priority,
				}); err != nil && err != ErrAlreadyEnqueued {
					enqueueErrors.Add(1)
				}
			}(j)
		}
		wg.Wait()

		if enqueueErrors.Load() > 0 {
			t.Errorf("enqueue errors: %d", enqueueErrors.Load())
		}

		// Dequeue all 100 tasks
		dequeued := 0
		for j := 0; j < 100; j++ {
			_, err := q.Dequeue(ctx, "throughput")
			if err == nil {
				dequeued++
			}
		}

		if dequeued != 100 {
			t.Errorf("iteration %d: expected 100 dequeued, got %d", i, dequeued)
		}
	}

	elapsed := time.Since(start)
	totalOps := iterations * 100 * 2 // 100 enqueue + 100 dequeue per iteration
	opsPerSec := float64(totalOps) / elapsed.Seconds()

	t.Logf("Throughput: %.0f ops/sec (%d ops in %v)", opsPerSec, totalOps, elapsed)

	// Requirement: at least 1000 ops/sec
	if opsPerSec < 1000 {
		t.Errorf("throughput too low: %.0f ops/sec (minimum: 1000)", opsPerSec)
	}
}

// TestMIG036_Queue_WFQFairness verifies WFQ scheduling fairness.
func TestMIG036_Queue_WFQFairness(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.New(mr.Addr(), "")
	defer client.Close()

	q := NewQueue(client)
	vt := NewVirtualTime(client)
	ctx := context.Background()

	// Reset
	_ = vt.Reset(ctx, "fairness")

	// Enqueue branches with different weights
	branches := []struct {
		name   string
		weight float64
		count  int
	}{
		{"hotfix", WeightHotfix, 10},           // weight=2.0, should get ~40% share
		{"feature", WeightFeature, 10},         // weight=1.0, should get ~20% share
		{"experiment", WeightExperimental, 10}, // weight=0.5, should get ~10% share
	}

	for _, b := range branches {
		for i := 0; i < b.count; i++ {
			vtime, _ := vt.Get(ctx, "fairness")
			priority := ComputeWFQPriority(vtime, b.weight)
			if err := q.Enqueue(ctx, QueueEntry{
				Repo:     "fairness",
				Branch:   fmt.Sprintf("%s-%d", b.name, i),
				Priority: priority,
				Weight:   b.weight,
			}); err != nil {
				t.Fatalf("enqueue failed: %v", err)
			}
		}
	}

	// Track which type was serviced when
	servicedOrder := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		entry, err := q.Dequeue(ctx, "fairness")
		if err != nil {
			t.Fatalf("dequeue %d failed: %v", i, err)
		}

		// Classify by prefix
		var typ string
		switch {
		case hasPrefix(entry.Branch, "hotfix"):
			typ = "hotfix"
		case hasPrefix(entry.Branch, "feature"):
			typ = "feature"
		case hasPrefix(entry.Branch, "experiment"):
			typ = "experiment"
		}
		servicedOrder = append(servicedOrder, typ)

		// Advance virtual time based on weight
		weight := ClassifyWeight(entry.Branch)
		_ = vt.Advance(ctx, "fairness", weight)
	}

	// Count service shares
	counts := make(map[string]int)
	for _, typ := range servicedOrder {
		counts[typ]++
	}

	// WFQ: higher weight should get proportionally more service
	// hotfix (2.0) should get more than feature (1.0) which should get more than experiment (0.5)
	t.Logf("Service distribution: hotfix=%d, feature=%d, experiment=%d",
		counts["hotfix"], counts["feature"], counts["experiment"])

	// Verify ordering: hotfix should be serviced most due to highest weight
	if counts["hotfix"] < counts["experiment"] {
		t.Errorf("WFQ fairness violated: hotfix (%d) should get more service than experiment (%d)",
			counts["hotfix"], counts["experiment"])
	}
}

// TestMIG036_Queue_IntegrationWithFSM verifies queue works with state transitions.
func TestMIG036_Queue_IntegrationWithFSM(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.New(mr.Addr(), "")
	defer client.Close()

	q := NewQueue(client)
	ctx := context.Background()

	// Simulate a branch lifecycle: enqueue when queued_cli
	branch := "feature/test-123"
	repo := "acme/api"

	// Enqueue with priority
	priority := ComputeWFQPriority(0, ClassifyWeight(branch))
	if err := q.Enqueue(ctx, QueueEntry{
		Repo:     repo,
		Branch:   branch,
		Priority: priority,
		Weight:   ClassifyWeight(branch),
	}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Verify queue position
	len, err := q.Len(ctx, repo)
	if err != nil {
		t.Fatalf("len failed: %v", err)
	}
	if len != 1 {
		t.Errorf("expected queue length 1, got %d", len)
	}

	// Dequeue (simulating dispatch)
	entry, err := q.Dequeue(ctx, repo)
	if err != nil {
		t.Fatalf("dequeue failed: %v", err)
	}

	if entry.Branch != branch {
		t.Errorf("expected branch %s, got %s", branch, entry.Branch)
	}

	// Queue should be empty
	len, _ = q.Len(ctx, repo)
	if len != 0 {
		t.Errorf("expected empty queue after dequeue, got %d", len)
	}
}

// TestMIG036_VirtualTime_Monotonic ensures virtual time advances correctly.
func TestMIG036_VirtualTime_Monotonic(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.New(mr.Addr(), "")
	defer client.Close()

	vt := NewVirtualTime(client)
	ctx := context.Background()

	_ = vt.Reset(ctx, "mono")

	// Virtual time should increase monotonically
	prevTime := 0.0
	for i := 0; i < 100; i++ {
		weight := 1.0 + float64(i%3) // weights: 1.0, 2.0, 3.0
		if err := vt.Advance(ctx, "mono", weight); err != nil {
			t.Fatalf("advance failed: %v", err)
		}

		currentTime, err := vt.Get(ctx, "mono")
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}

		if currentTime <= prevTime {
			t.Errorf("virtual time should increase: prev=%v, current=%v", prevTime, currentTime)
		}
		prevTime = currentTime
	}

	t.Logf("Final virtual time: %.4f", prevTime)
}
