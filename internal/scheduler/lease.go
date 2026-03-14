package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// If the caller already holds the lease, it is extended.
func (lm *LeaseManager) Acquire(ctx context.Context, repo, branch, holderID string) (*Lease, error) {
	return lm.AcquireWithTTL(ctx, repo, branch, holderID, lm.defaultTTL)
}

// AcquireWithTTL is like Acquire but with a custom TTL.
// Uses an atomic Lua script to avoid TOCTOU races between check and extend.
// When extending an existing lease held by the same holder, AcquiredAt is preserved.
func (lm *LeaseManager) AcquireWithTTL(ctx context.Context, repo, branch, holderID string, ttl time.Duration) (*Lease, error) {
	key, err := redis.BuildKey(repo, "lease", branch)
	if err != nil {
		return nil, fmt.Errorf("acquire lease: %w", err)
	}

	now := time.Now()
	expiry := now.Add(ttl)

	// Use atomic Lua script for check-and-acquire-or-extend.
	// This avoids TOCTOU race between SetNX failure and subsequent GET+SET.
	// Returns: {1, acquired_at_ms} for new lease, {1, 0} for extended existing lease, {0} for conflict
	rc := lm.client.Unwrap()
	script := `
		local current = redis.call('GET', KEYS[1])
		if current == false then
			-- No lease exists: acquire it, store acquisition time
			local acquired_at = ARGV[3]
			redis.call('SET', KEYS[1], ARGV[1] .. '|' .. acquired_at, 'PX', ARGV[2])
			return {1, tonumber(acquired_at)}
		end
		-- Extract holderID from value using literal comparison (avoid pattern metacharacters)
		local pipe_pos = string.find(current, '|', 1, true)
		local stored_holder = current
		local acquired_at = '0'
		if pipe_pos then
			stored_holder = string.sub(current, 1, pipe_pos - 1)
			acquired_at = string.sub(current, pipe_pos + 1)
		end
		if stored_holder == ARGV[1] then
			-- We already hold it: extend TTL, return stored acquisition time
			redis.call('PEXPIRE', KEYS[1], ARGV[2])
			return {1, tonumber(acquired_at) or 0}
		else
			-- Held by another: conflict
			return {0, 0}
		end
	`
	result, err := rc.Eval(ctx, script, []string{key}, holderID, ttl.Milliseconds(), now.UnixMilli()).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire lease: redis error: %w", err)
	}

	// Parse result array
	arr, ok := result.([]interface{})
	if !ok || len(arr) < 2 {
		return nil, fmt.Errorf("acquire lease: unexpected script result type")
	}
	status, ok := arr[0].(int64)
	if !ok {
		return nil, fmt.Errorf("acquire lease: unexpected status type")
	}
	acquiredAtMs, ok := arr[1].(int64)
	if !ok {
		acquiredAtMs = now.UnixMilli()
	}

	if status == 0 {
		return nil, ErrLeaseConflict
	}

	// Parse acquired time: preserve stored timestamp if valid, otherwise use now
	var acquiredAt time.Time
	if acquiredAtMs > 0 {
		acquiredAt = time.UnixMilli(acquiredAtMs)
	} else {
		acquiredAt = now
	}

	return &Lease{
		Repo:       repo,
		Branch:     branch,
		HolderID:   holderID,
		ExpiresAt:  expiry,
		AcquiredAt: acquiredAt,
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
	// Handles both old format (just holderID) and new format (holderID|acquiredAt).
	// Uses literal comparison to avoid pattern metacharacter issues.
	rc := lm.client.Unwrap()
	script := `
		local current = redis.call('GET', KEYS[1])
		if current == false then
			return 0
		end
		-- Extract holderID from value using literal comparison
		local pipe_pos = string.find(current, '|', 1, true)
		local stored_holder = current
		if pipe_pos then
			stored_holder = string.sub(current, 1, pipe_pos - 1)
		end
		if stored_holder == ARGV[1] then
			redis.call('DEL', KEYS[1])
			return 1
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
// Returns ErrLeaseNotFound if the lease doesn't exist.
// Returns ErrLeaseConflict if the lease is held by another holder.
func (lm *LeaseManager) Extend(ctx context.Context, repo, branch, holderID string, ttl time.Duration) (*Lease, error) {
	key, err := redis.BuildKey(repo, "lease", branch)
	if err != nil {
		return nil, fmt.Errorf("extend lease: %w", err)
	}

	// Use Lua script for atomic check-and-extend.
	// Returns: {1, acquired_at_ms} on success, {0} if missing, {-1} if held by another
	// Uses literal comparison to avoid pattern metacharacter issues.
	rc := lm.client.Unwrap()
	script := `
		local current = redis.call('GET', KEYS[1])
		if current == false then
			-- Lease does not exist
			return {0, 0}
		end
		-- Extract holderID from value using literal comparison
		local pipe_pos = string.find(current, '|', 1, true)
		local stored_holder = current
		local acquired_at = '0'
		if pipe_pos then
			stored_holder = string.sub(current, 1, pipe_pos - 1)
			acquired_at = string.sub(current, pipe_pos + 1)
		end
		if stored_holder == ARGV[1] then
			-- We hold it: extend TTL, return stored acquisition time
			redis.call('PEXPIRE', KEYS[1], ARGV[2])
			return {1, tonumber(acquired_at) or 0}
		else
			-- Held by another
			return {-1, 0}
		end
	`
	result, err := rc.Eval(ctx, script, []string{key}, holderID, ttl.Milliseconds()).Result()
	if err != nil {
		return nil, fmt.Errorf("extend lease: redis error: %w", err)
	}

	// Parse result array
	arr, ok := result.([]interface{})
	if !ok || len(arr) < 2 {
		return nil, fmt.Errorf("extend lease: unexpected script result type")
	}
	status, ok := arr[0].(int64)
	if !ok {
		return nil, fmt.Errorf("extend lease: unexpected status type")
	}
	acquiredAtMs, _ := arr[1].(int64)

	switch status {
	case 1:
		// Success - preserve original AcquiredAt
		var acquiredAt time.Time
		if acquiredAtMs > 0 {
			acquiredAt = time.UnixMilli(acquiredAtMs)
		} else {
			acquiredAt = time.Now() // fallback for leases without stored time
		}
		return &Lease{
			Repo:       repo,
			Branch:     branch,
			HolderID:   holderID,
			ExpiresAt:  time.Now().Add(ttl),
			AcquiredAt: acquiredAt,
		}, nil
	case 0:
		return nil, ErrLeaseNotFound
	case -1:
		return nil, ErrLeaseConflict
	default:
		return nil, fmt.Errorf("extend lease: unexpected script result %d", status)
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
	value, err := rc.Get(ctx, key).Result()
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

	// Parse value: either "holderID" (old format) or "holderID|acquiredAt" (new format)
	var holderID string
	var acquiredAt time.Time
	if pipeIdx := strings.IndexByte(value, '|'); pipeIdx >= 0 {
		holderID = value[:pipeIdx]
		// Try to parse acquisition time
		if acquiredAtMs, parseErr := parseInt64(value[pipeIdx+1:]); parseErr == nil && acquiredAtMs > 0 {
			acquiredAt = time.UnixMilli(acquiredAtMs)
		}
	} else {
		holderID = value // old format, no pipe character
	}

	return &Lease{
		Repo:       repo,
		Branch:     branch,
		HolderID:   holderID,
		ExpiresAt:  time.Now().Add(ttl),
		AcquiredAt: acquiredAt,
	}, nil
}

// parseInt64 parses a string to int64. Returns 0 and error on failure.
func parseInt64(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid digit")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
