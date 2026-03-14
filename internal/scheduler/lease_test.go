package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/redis"
	goredis "github.com/redis/go-redis/v9"
)

// testRedisClient creates a miniredis server and returns a client connected to it.
func testRedisClient(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.New(mr.Addr(), "")
	t.Cleanup(func() { client.Close() })

	return client, mr
}

func TestLeaseAcquire(t *testing.T) {
	client, _ := testRedisClient(t)
	lm := NewLeaseManager(client, WithLeaseTTL(5*time.Second))

	ctx := context.Background()

	lease, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if lease.Repo != "test" {
		t.Errorf("expected repo 'test', got %q", lease.Repo)
	}
	if lease.Branch != "branch1" {
		t.Errorf("expected branch 'branch1', got %q", lease.Branch)
	}
	if lease.HolderID != "holder1" {
		t.Errorf("expected holder 'holder1', got %q", lease.HolderID)
	}
	if lease.IsExpired() {
		t.Error("lease should not be expired immediately after acquisition")
	}
}

func TestLeaseConflict(t *testing.T) {
	client, _ := testRedisClient(t)
	lm := NewLeaseManager(client)

	ctx := context.Background()

	// First acquisition succeeds
	_, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}

	// Second acquisition by different holder fails
	_, err = lm.Acquire(ctx, "test", "branch1", "holder2")
	if !errors.Is(err, ErrLeaseConflict) {
		t.Errorf("expected ErrLeaseConflict, got %v", err)
	}
}

func TestLeaseAcquireSameHolder(t *testing.T) {
	client, _ := testRedisClient(t)
	lm := NewLeaseManager(client, WithLeaseTTL(5*time.Second))

	ctx := context.Background()

	// First acquisition
	lease1, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}

	// Same holder acquires again - should extend existing lease
	lease2, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}

	// Leases should have different expiry times (extension)
	if !lease2.ExpiresAt.After(lease1.ExpiresAt) {
		t.Error("second acquisition should extend lease expiry")
	}
}

func TestLeaseRelease(t *testing.T) {
	client, _ := testRedisClient(t)
	lm := NewLeaseManager(client)

	ctx := context.Background()

	_, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Release by owner succeeds
	err = lm.Release(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// After release, someone else can acquire
	_, err = lm.Acquire(ctx, "test", "branch1", "holder2")
	if err != nil {
		t.Fatalf("Acquire after release failed: %v", err)
	}
}

func TestLeaseReleaseWrongHolder(t *testing.T) {
	client, _ := testRedisClient(t)
	lm := NewLeaseManager(client)

	ctx := context.Background()

	_, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Release by wrong holder fails
	err = lm.Release(ctx, "test", "branch1", "holder2")
	if !errors.Is(err, ErrLeaseNotFound) {
		t.Errorf("expected ErrLeaseNotFound, got %v", err)
	}

	// Original holder still holds the lease
	lease, err := lm.Get(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if lease == nil {
		t.Error("lease should still exist")
	} else if lease.HolderID != "holder1" {
		t.Errorf("expected holder 'holder1', got %q", lease.HolderID)
	}
}

func TestLeaseExtend(t *testing.T) {
	client, _ := testRedisClient(t)
	lm := NewLeaseManager(client, WithLeaseTTL(1*time.Second))

	ctx := context.Background()

	lease, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Extend the lease
	extended, err := lm.Extend(ctx, "test", "branch1", "holder1", 5*time.Second)
	if err != nil {
		t.Fatalf("Extend failed: %v", err)
	}

	if !extended.ExpiresAt.After(lease.ExpiresAt) {
		t.Error("extended lease should have later expiry")
	}
}

func TestLeaseExtendWrongHolder(t *testing.T) {
	client, _ := testRedisClient(t)
	lm := NewLeaseManager(client)

	ctx := context.Background()

	_, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Extend by wrong holder fails
	_, err = lm.Extend(ctx, "test", "branch1", "holder2", 5*time.Second)
	if !errors.Is(err, ErrLeaseNotFound) {
		t.Errorf("expected ErrLeaseNotFound, got %v", err)
	}
}

func TestLeaseExpiry(t *testing.T) {
	client, mr := testRedisClient(t)
	lm := NewLeaseManager(client, WithLeaseTTL(500*time.Millisecond))

	ctx := context.Background()

	_, err := lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Use miniredis FastForward to simulate TTL expiry
	mr.FastForward(600 * time.Millisecond)

	// Extend should fail for expired lease
	_, err = lm.Extend(ctx, "test", "branch1", "holder1", 5*time.Second)
	if !errors.Is(err, ErrLeaseExpired) {
		t.Errorf("expected ErrLeaseExpired, got %v", err)
	}

	// Another holder can now acquire
	_, err = lm.Acquire(ctx, "test", "branch1", "holder2")
	if err != nil {
		t.Fatalf("Acquire after expiry failed: %v", err)
	}
}

func TestLeaseGet(t *testing.T) {
	client, _ := testRedisClient(t)
	lm := NewLeaseManager(client)

	ctx := context.Background()

	// Get non-existent lease returns nil
	lease, err := lm.Get(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if lease != nil {
		t.Error("expected nil for non-existent lease")
	}

	// Create lease
	_, err = lm.Acquire(ctx, "test", "branch1", "holder1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Get returns the lease
	lease, err = lm.Get(ctx, "test", "branch1")
	if err != nil {
		t.Fatalf("Get after Acquire failed: %v", err)
	}
	if lease == nil {
		t.Fatal("expected lease, got nil")
	}
	if lease.HolderID != "holder1" {
		t.Errorf("expected holder 'holder1', got %q", lease.HolderID)
	}
}

func TestLeaseTimeRemaining(t *testing.T) {
	lease := &Lease{
		ExpiresAt: time.Now().Add(5 * time.Second),
	}

	remaining := lease.TimeRemaining()
	if remaining <= 0 || remaining > 5*time.Second {
		t.Errorf("expected ~5s remaining, got %v", remaining)
	}

	// Expired lease
	lease.ExpiresAt = time.Now().Add(-1 * time.Second)
	remaining = lease.TimeRemaining()
	if remaining != 0 {
		t.Errorf("expected 0 remaining for expired lease, got %v", remaining)
	}
}

// Ensure LeaseManager implements expected interface
var _ = func() {
	_ = func(lm *LeaseManager, ctx context.Context) {
		var repo, branch, holder string
		var ttl time.Duration
		_, _ = lm.Acquire(ctx, repo, branch, holder)
		_, _ = lm.AcquireWithTTL(ctx, repo, branch, holder, ttl)
		_ = lm.Release(ctx, repo, branch, holder)
		_, _ = lm.Extend(ctx, repo, branch, holder, ttl)
		_, _ = lm.Get(ctx, repo, branch)
	}
}

// Verify we can access the underlying goredis.Nil error
var _ = goredis.Nil
