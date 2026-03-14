package scheduler

import (
	"context"
	"fmt"

	"github.com/codero/codero/internal/redis"
	goredis "github.com/redis/go-redis/v9"
)

// SlotCounter manages atomic slot allocation for concurrent dispatch.
// It ensures that the number of concurrent dispatch operations does not exceed
// a configurable limit per repository. Uses Redis INCR/DECR via Lua scripts
// for atomic count operations to prevent race conditions.
type SlotCounter struct {
	client *redis.Client // Redis client for atomic operations
}

// NewSlotCounter creates a slot counter manager.
func NewSlotCounter(client *redis.Client) *SlotCounter {
	return &SlotCounter{client: client}
}

// SlotKey returns the Redis key for a repo's slot counter.
func slotKey(repo string) string {
	// Uses the redis package's key building logic
	key, _ := redis.BuildKey(repo, "dispatch", "slots")
	return key
}

// AcquireSlot atomically increments the slot counter if under the limit.
// Returns true if slot acquired, false if at capacity.
func (sc *SlotCounter) AcquireSlot(ctx context.Context, repo string, limit int64) (bool, error) {
	key := slotKey(repo)
	rc := sc.client.Unwrap()

	// Use Lua script for atomic check-and-increment
	// This avoids race condition between GET and INCR
	script := `
		local current = redis.call('GET', KEYS[1])
		local limit = tonumber(ARGV[1])
		local count = 0
		if current ~= false then
			count = tonumber(current)
		end
		if count >= limit then
			return 0
		end
		return redis.call('INCR', KEYS[1])
	`

	result, err := rc.Eval(ctx, script, []string{key}, limit).Int()
	if err != nil {
		return false, fmt.Errorf("acquire slot: redis error: %w", err)
	}

	return result > 0, nil
}

// ReleaseSlot atomically decrements the slot counter.
// Safe to call even if counter is at 0.
func (sc *SlotCounter) ReleaseSlot(ctx context.Context, repo string) error {
	key := slotKey(repo)
	rc := sc.client.Unwrap()

	// Use Lua to ensure we don't go below zero
	script := `
		local current = redis.call('GET', KEYS[1])
		if current == false then
			return 0
		end
		local count = tonumber(current)
		if count <= 0 then
			return 0
		end
		return redis.call('DECR', KEYS[1])
	`

	_, err := rc.Eval(ctx, script, []string{key}).Result()
	if err != nil {
		return fmt.Errorf("release slot: redis error: %w", err)
	}

	return nil
}

// GetSlotCount returns the current slot count.
// Returns 0 if no counter exists.
func (sc *SlotCounter) GetSlotCount(ctx context.Context, repo string) (int64, error) {
	key := slotKey(repo)
	rc := sc.client.Unwrap()

	val, err := rc.Get(ctx, key).Int64()
	if err == goredis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get slot count: redis error: %w", err)
	}

	return val, nil
}

// SetSlotCount sets the slot counter to a specific value (for initialization or reset).
func (sc *SlotCounter) SetSlotCount(ctx context.Context, repo string, count int64) error {
	key := slotKey(repo)
	rc := sc.client.Unwrap()

	if count <= 0 {
		// Delete the key if setting to 0 or negative
		_, err := rc.Del(ctx, key).Result()
		return err
	}

	_, err := rc.Set(ctx, key, count, 0).Result()
	if err != nil {
		return fmt.Errorf("set slot count: redis error: %w", err)
	}

	return nil
}

// ResetSlotCount deletes the slot counter (for testing or recovery).
func (sc *SlotCounter) ResetSlotCount(ctx context.Context, repo string) error {
	key := slotKey(repo)
	rc := sc.client.Unwrap()

	_, err := rc.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("reset slot count: redis error: %w", err)
	}

	return nil
}
