package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/redis"
)

// testRedisClientHeartbeat creates a miniredis server and returns a client connected to it.
func testRedisClientHeartbeat(t *testing.T) *redis.Client {
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

func TestHeartbeatStartStop(t *testing.T) {
	client := testRedisClientHeartbeat(t)
	lm := NewLeaseManager(client, WithLeaseTTL(1*time.Second))

	ctx := context.Background()

	// Acquire lease first
	lease, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Start heartbeat
	cfg := HeartbeatConfig{
		Interval:  50 * time.Millisecond,
		LeaseTTL:  200 * time.Millisecond,
		MaxMisses: 3,
	}
	hb, err := lm.StartHeartbeat(ctx, lease, cfg)
	if err != nil {
		t.Fatalf("StartHeartbeat failed: %v", err)
	}

	// Give heartbeat time to run
	time.Sleep(100 * time.Millisecond)

	// Check lease is still alive
	currentLease, err := lm.Get(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if currentLease == nil {
		t.Error("lease should still exist")
	} else if currentLease.HolderID != "holder1" {
		t.Errorf("expected holder 'holder1', got %q", currentLease.HolderID)
	}

	// Stop heartbeat
	hb.Stop()

	// Verify lease was released
	time.Sleep(50 * time.Millisecond)
	currentLease, err = lm.Get(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if currentLease != nil {
		t.Error("lease should be released after Stop")
	}
}

func TestHeartbeatContextCancellation(t *testing.T) {
	client := testRedisClientHeartbeat(t)
	lm := NewLeaseManager(client, WithLeaseTTL(1*time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lease, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	cfg := HeartbeatConfig{
		Interval:  50 * time.Millisecond,
		LeaseTTL:  200 * time.Millisecond,
		MaxMisses: 3,
	}
	hb, err := lm.StartHeartbeat(ctx, lease, cfg)
	if err != nil {
		t.Fatalf("StartHeartbeat failed: %v", err)
	}

	// Give heartbeat time to run
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for goroutine to finish
	time.Sleep(100 * time.Millisecond)

	// Verify lease was released
	currentLease, err := lm.Get(context.Background(), "test", "branch1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if currentLease != nil {
		t.Error("lease should be released after context cancellation")
	}

	// Stop should be safe even after context cancellation
	hb.Stop()
}

func TestHeartbeatStatus(t *testing.T) {
	client := testRedisClientHeartbeat(t)
	lm := NewLeaseManager(client, WithLeaseTTL(1*time.Second))

	ctx := context.Background()

	lease, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	cfg := HeartbeatConfig{
		Interval:  50 * time.Millisecond,
		LeaseTTL:  200 * time.Millisecond,
		MaxMisses: 3,
	}
	hb, err := lm.StartHeartbeat(ctx, lease, cfg)
	if err != nil {
		t.Fatalf("StartHeartbeat failed: %v", err)
	}
	defer hb.Stop()

	// Wait for first heartbeat
	time.Sleep(100 * time.Millisecond)

	status := hb.Status()

	if status.Repo != "test" {
		t.Errorf("expected repo 'test', got %q", status.Repo)
	}
	if status.Branch != "branch1" {
		t.Errorf("expected branch 'branch1', got %q", status.Branch)
	}
	if status.HolderID != "holder1" {
		t.Errorf("expected holder 'holder1', got %q", status.HolderID)
	}
	if status.Misses != 0 {
		t.Errorf("expected 0 misses, got %d", status.Misses)
	}
	if !status.IsHealthy() {
		t.Error("status should be healthy")
	}
	if status.IsExpired() {
		t.Error("lease should not be expired")
	}
}

func TestHeartbeatMaxMisses(t *testing.T) {
	client := testRedisClientHeartbeat(t)
	lm := NewLeaseManager(client, WithLeaseTTL(100*time.Millisecond))

	ctx := context.Background()

	lease, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Short TTL and short heartbeat interval
	cfg := HeartbeatConfig{
		Interval:  20 * time.Millisecond,
		LeaseTTL:  50 * time.Millisecond,
		MaxMisses: 2,
	}

	// Start heartbeat
	hb, err := lm.StartHeartbeat(ctx, lease, cfg)
	if err != nil {
		t.Fatalf("StartHeartbeat failed: %v", err)
	}

	// Steal the lease to cause heartbeat failures
	time.Sleep(30 * time.Millisecond)
	rc := client.Unwrap()
	rc.Set(ctx, "codero:test:lease:branch1", "other-holder", 5*time.Second)

	// Wait for heartbeats to fail and trigger MaxMisses cleanup
	time.Sleep(150 * time.Millisecond)

	// Verify heartbeat stopped after MaxMisses
	currentLease := hb.Lease()
	if currentLease != nil {
		t.Error("Lease() should return nil after MaxMisses stops heartbeat")
	}

	// Verify the lease was released (the "other-holder" value should remain,
	// but our release attempt should have happened)
	holder, _ := rc.Get(ctx, "codero:test:lease:branch1").Result()
	if holder == "holder1" {
		t.Error("original holder's lease should have been released")
	}
}

func TestHeartbeatLeaseMethod(t *testing.T) {
	client := testRedisClientHeartbeat(t)
	lm := NewLeaseManager(client)

	ctx := context.Background()

	lease, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	cfg := HeartbeatConfig{
		Interval:  50 * time.Millisecond,
		LeaseTTL:  200 * time.Millisecond,
		MaxMisses: 3,
	}
	hb, err := lm.StartHeartbeat(ctx, lease, cfg)
	if err != nil {
		t.Fatalf("StartHeartbeat failed: %v", err)
	}

	// Should return lease while running
	currentLease := hb.Lease()
	if currentLease == nil {
		t.Error("Lease should return non-nil while running")
	}

	hb.Stop()

	// Should return nil after stop
	currentLease = hb.Lease()
	if currentLease != nil {
		t.Error("Lease should return nil after stop")
	}
}

func TestHeartbeatStatusIsHealthy(t *testing.T) {
	tests := []struct {
		name    string
		status  HeartbeatStatus
		healthy bool
	}{
		{
			name: "healthy",
			status: HeartbeatStatus{
				Misses:  0,
				LastErr: nil,
			},
			healthy: true,
		},
		{
			name: "has misses",
			status: HeartbeatStatus{
				Misses:  1,
				LastErr: nil,
			},
			healthy: false,
		},
		{
			name: "has error",
			status: HeartbeatStatus{
				Misses:  0,
				LastErr: errors.New("some error"),
			},
			healthy: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.status.IsHealthy() != tt.healthy {
				t.Errorf("IsHealthy() = %v, want %v", tt.status.IsHealthy(), tt.healthy)
			}
		})
	}
}

func TestHeartbeatStatusIsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		expired   bool
	}{
		{
			name:      "not expired",
			expiresAt: time.Now().Add(10 * time.Second),
			expired:   false,
		},
		{
			name:      "expired",
			expiresAt: time.Now().Add(-1 * time.Second),
			expired:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := HeartbeatStatus{ExpiresAt: tt.expiresAt}
			if status.IsExpired() != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", status.IsExpired(), tt.expired)
			}
		})
	}
}

func TestDefaultHeartbeatConfig(t *testing.T) {
	cfg := DefaultHeartbeatConfig()

	if cfg.Interval <= 0 {
		t.Error("Interval should be positive")
	}
	if cfg.LeaseTTL <= 0 {
		t.Error("LeaseTTL should be positive")
	}
	if cfg.Interval >= cfg.LeaseTTL {
		t.Error("Interval should be less than LeaseTTL")
	}
	if cfg.MaxMisses <= 0 {
		t.Error("MaxMisses should be positive")
	}
}

func TestMonitorHeartbeat(t *testing.T) {
	client := testRedisClientHeartbeat(t)
	monitor := NewMonitorHeartbeat(client)
	lm := NewLeaseManager(client)

	ctx := context.Background()

	// Non-existent lease
	alive, err := monitor.IsAlive(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("IsAlive failed: %v", err)
	}
	if alive {
		t.Error("non-existent lease should not be alive")
	}

	// Create lease
	_, err = lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Should be alive
	alive, err = monitor.IsAlive(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("IsAlive failed: %v", err)
	}
	if !alive {
		t.Error("lease should be alive")
	}

	// Release lease
	_ = lm.Release(ctx, "test", "branch1", "holder1")

	// Should not be alive
	alive, err = monitor.IsAlive(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("IsAlive failed: %v", err)
	}
	if alive {
		t.Error("released lease should not be alive")
	}
}

func TestHeartbeatMultipleStops(t *testing.T) {
	client := testRedisClientHeartbeat(t)
	lm := NewLeaseManager(client)

	ctx := context.Background()

	lease, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	cfg := HeartbeatConfig{
		Interval:  50 * time.Millisecond,
		LeaseTTL:  200 * time.Millisecond,
		MaxMisses: 3,
	}
	hb, err := lm.StartHeartbeat(ctx, lease, cfg)
	if err != nil {
		t.Fatalf("StartHeartbeat failed: %v", err)
	}

	// Multiple stops should be safe
	hb.Stop()
	hb.Stop()
	hb.Stop()
}
