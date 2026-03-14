package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codero/codero/internal/redis"
	goredis "github.com/redis/go-redis/v9"
)

// Lease represents a time-limited claim on a branch for processing.
// The holder must extend the lease via heartbeats before expiry.
type Lease struct {
	Repo       string    // owner/repo slug
	Branch     string    // branch name
	HolderID   string    // unique identifier of the lease holder
	ExpiresAt  time.Time // when the lease expires
	AcquiredAt time.Time // when the lease was acquired
}

// IsExpired returns true if the lease has passed its expiry time.
func (l *Lease) IsExpired() bool {
	return time.Now().After(l.ExpiresAt)
}

// TimeRemaining returns the duration until lease expiry.
// Returns 0 if already expired.
func (l *Lease) TimeRemaining() time.Duration {
	remaining := time.Until(l.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// LeaseManager handles lease acquisition, release, and renewal.
// All lease operations are coordinated through Redis for cross-process safety.
type LeaseManager struct {
	client     *redis.Client
	defaultTTL time.Duration
}

// LeaseOption configures a LeaseManager.
type LeaseOption func(*LeaseManager)

// WithLeaseTTL sets the default lease TTL.
func WithLeaseTTL(d time.Duration) LeaseOption {
	return func(lm *LeaseManager) {
		lm.defaultTTL = d
	}
}

// ErrLeaseConflict is returned when attempting to acquire a lease
// that is held by another holder.
var ErrLeaseConflict = errors.New("lease held by another holder")

// ErrLeaseNotFound is returned when attempting to release or extend
// a lease that does not exist or belongs to a different holder.
var ErrLeaseNotFound = errors.New("lease not found or not owned")

// ErrLeaseExpired is returned when attempting to extend an expired lease.
var ErrLeaseExpired = errors.New("lease has expired")

// NewLeaseManager creates a LeaseManager with the given Redis client.
func NewLeaseManager(client *redis.Client, opts ...LeaseOption) *LeaseManager {
	lm := &LeaseManager{
		client:     client,
		defaultTTL: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(lm)
	}
	return lm
}

// Acquire attempts to obtain a lease on a branch.
// Returns ErrLeaseConflict if the lease is held by another holder.
// If extendIfHeld is true and the caller already holds the lease, it will be extended.
func (lm *LeaseManager) Acquire(ctx context.Context, repo, branch, holderID string) (*Lease, error) {
	return lm.AcquireWithTTL(ctx, repo, branch, holderID, lm.defaultTTL)
}

// AcquireWithTTL is like Acquire but with a custom TTL.
func (lm *LeaseManager) AcquireWithTTL(ctx context.Context, repo, branch, holderID string, ttl time.Duration) (*Lease, error) {
	key, err := redis.BuildKey(repo, "lease", branch)
	if err != nil {
		return nil, fmt.Errorf("acquire lease: %w", err)
	}

	now := time.Now()
	expiry := now.Add(ttl)

	// Use SET NX (only set if not exists) for atomic acquisition.
	rc := lm.client.Unwrap()
	acquired, err := rc.SetNX(ctx, key, holderID, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire lease: redis error: %w", err)
	}

	if !acquired {
		// Check if we already hold the lease.
		currentHolder, err := rc.Get(ctx, key).Result()
		if err != nil {
			return nil, fmt.Errorf("acquire lease: check current holder: %w", err)
		}
		if currentHolder == holderID {
			// We already hold it; extend the lease.
			if err := rc.Set(ctx, key, holderID, ttl).Err(); err != nil {
				return nil, fmt.Errorf("acquire lease: extend own lease: %w", err)
			}
			return &Lease{
				Repo:       repo,
				Branch:     branch,
				HolderID:   holderID,
				ExpiresAt:  expiry,
				AcquiredAt: now,
			}, nil
		}
		return nil, ErrLeaseConflict
	}

	return &Lease{
		Repo:       repo,
		Branch:     branch,
		HolderID:   holderID,
		ExpiresAt:  expiry,
		AcquiredAt: now,
	}, nil
}

// Release releases a lease. Returns ErrLeaseNotFound if the lease
// does not exist or is held by a different holder.
// Safe to call on non-existent leases (returns nil in that case).
func (lm *LeaseManager) Release(ctx context.Context, repo, branch, holderID string) error {
	key, err := redis.BuildKey(repo, "lease", branch)
	if err != nil {
		return fmt.Errorf("release lease: %w", err)
	}

	// Use Lua script for atomic check-and-delete.
	rc := lm.client.Unwrap()
	script := `
		local current = redis.call('GET', KEYS[1])
		if current == ARGV[1] then
			redis.call('DEL', KEYS[1])
			return 1
		elseif current == false then
			return 0
		else
			return -1
		end
	`
	result, err := rc.Eval(ctx, script, []string{key}, holderID).Int()
	if err != nil {
		return fmt.Errorf("release lease: redis error: %w", err)
	}

	switch result {
	case 1:
		return nil // released
	case 0:
		return nil // lease did not exist
	case -1:
		return ErrLeaseNotFound // held by another
	default:
		return fmt.Errorf("release lease: unexpected script result %d", result)
	}
}

// Extend renews a lease with a new TTL. The lease must be held by holderID.
// Returns ErrLeaseNotFound if the lease doesn't exist or is held by another.
// Returns ErrLeaseExpired if the lease has already expired.
func (lm *LeaseManager) Extend(ctx context.Context, repo, branch, holderID string, ttl time.Duration) (*Lease, error) {
	key, err := redis.BuildKey(repo, "lease", branch)
	if err != nil {
		return nil, fmt.Errorf("extend lease: %w", err)
	}

	// Use Lua script for atomic check-and-extend.
	rc := lm.client.Unwrap()
	script := `
		local current = redis.call('GET', KEYS[1])
		if current == ARGV[1] then
			redis.call('PEXPIRE', KEYS[1], ARGV[2])
			return 1
		elseif current == false then
			return 0
		else
			return -1
		end
	`
	result, err := rc.Eval(ctx, script, []string{key}, holderID, ttl.Milliseconds()).Int()
	if err != nil {
		return nil, fmt.Errorf("extend lease: redis error: %w", err)
	}

	switch result {
	case 1:
		return &Lease{
			Repo:       repo,
			Branch:     branch,
			HolderID:   holderID,
			ExpiresAt:  time.Now().Add(ttl),
			AcquiredAt: time.Now(), // approximate; original acquire time not preserved
		}, nil
	case 0:
		return nil, ErrLeaseExpired
	case -1:
		return nil, ErrLeaseNotFound
	default:
		return nil, fmt.Errorf("extend lease: unexpected script result %d", result)
	}
}

// Get retrieves the current lease holder for a branch.
// Returns nil if no lease exists.
func (lm *LeaseManager) Get(ctx context.Context, repo, branch string) (*Lease, error) {
	key, err := redis.BuildKey(repo, "lease", branch)
	if err != nil {
		return nil, fmt.Errorf("get lease: %w", err)
	}

	rc := lm.client.Unwrap()
	holderID, err := rc.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, nil // no lease
		}
		return nil, fmt.Errorf("get lease: redis error: %w", err)
	}

	ttl, err := rc.TTL(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("get lease: get TTL: %w", err)
	}

	return &Lease{
		Repo:      repo,
		Branch:    branch,
		HolderID:  holderID,
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}
