package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codero/codero/internal/redis"
	goredis "github.com/redis/go-redis/v9"
)

// QueuePriority represents the scheduling priority of a branch.
// Lower values = higher priority (will be processed first).
type QueuePriority float64

// QueueEntry represents a branch in the dispatch queue.
type QueueEntry struct {
	Repo       string
	Branch     string
	Priority   QueuePriority // lower = higher priority
	EnqueuedAt time.Time
	Weight     float64 // weight for fair scheduling (higher = more share)
}

// Queue manages branches waiting for dispatch.
// Uses a Redis sorted set (ZSET) for weighted-fair-queue scheduling.
type Queue struct {
	client *redis.Client
}

// NewQueue creates a queue manager.
func NewQueue(client *redis.Client) *Queue {
	return &Queue{client: client}
}

// ErrQueueEmpty is returned when attempting to dequeue from an empty queue.
var ErrQueueEmpty = errors.New("queue is empty")

// ErrAlreadyEnqueued is returned when attempting to enqueue a branch that is already in the queue.
var ErrAlreadyEnqueued = errors.New("branch already in queue")

// Enqueue adds a branch to the queue with the given priority.
// Priority is typically computed based on wait time and branch weight.
// Returns ErrAlreadyEnqueued if the branch is already in the queue.
func (q *Queue) Enqueue(ctx context.Context, entry QueueEntry) error {
	key, err := redis.BuildKey(entry.Repo, "queue", "pending")
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}

	rc := q.client.Unwrap()

	// Use ZADD NX to only add if not already present.
	// Score is the priority (lower = higher priority).
	added, err := rc.ZAddNX(ctx, key, goredis.Z{
		Score:  float64(entry.Priority),
		Member: entry.Branch,
	}).Result()
	if err != nil {
		return fmt.Errorf("enqueue: redis error: %w", err)
	}

	if added == 0 {
		return ErrAlreadyEnqueued
	}

	return nil
}

// Dequeue removes and returns the highest-priority (lowest score) entry.
// Returns ErrQueueEmpty if the queue is empty.
// On success, returns the dequeued entry.
func (q *Queue) Dequeue(ctx context.Context, repo string) (*QueueEntry, error) {
	key, err := redis.BuildKey(repo, "queue", "pending")
	if err != nil {
		return nil, fmt.Errorf("dequeue: %w", err)
	}

	rc := q.client.Unwrap()

	// Get the lowest-score member (highest priority).
	members, err := rc.ZPopMin(ctx, key, 1).Result()
	if err != nil {
		return nil, fmt.Errorf("dequeue: redis error: %w", err)
	}

	if len(members) == 0 {
		return nil, ErrQueueEmpty
	}

	return &QueueEntry{
		Repo:     repo,
		Branch:   members[0].Member.(string),
		Priority: QueuePriority(members[0].Score),
	}, nil
}

// Peek returns the highest-priority entry without removing it.
// Returns ErrQueueEmpty if the queue is empty.
func (q *Queue) Peek(ctx context.Context, repo string) (*QueueEntry, error) {
	key, err := redis.BuildKey(repo, "queue", "pending")
	if err != nil {
		return nil, fmt.Errorf("peek: %w", err)
	}

	rc := q.client.Unwrap()

	// Get the lowest-score member without removing it.
	members, err := rc.ZRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("peek: redis error: %w", err)
	}

	if len(members) == 0 {
		return nil, ErrQueueEmpty
	}

	return &QueueEntry{
		Repo:     repo,
		Branch:   members[0].Member.(string),
		Priority: QueuePriority(members[0].Score),
	}, nil
}

// Remove removes a specific branch from the queue.
// Returns nil even if the branch was not in the queue.
func (q *Queue) Remove(ctx context.Context, repo, branch string) error {
	key, err := redis.BuildKey(repo, "queue", "pending")
	if err != nil {
		return fmt.Errorf("remove from queue: %w", err)
	}

	rc := q.client.Unwrap()
	_, err = rc.ZRem(ctx, key, branch).Result()
	if err != nil {
		return fmt.Errorf("remove from queue: redis error: %w", err)
	}

	return nil
}

// Len returns the number of entries in the queue.
func (q *Queue) Len(ctx context.Context, repo string) (int64, error) {
	key, err := redis.BuildKey(repo, "queue", "pending")
	if err != nil {
		return 0, fmt.Errorf("queue length: %w", err)
	}

	rc := q.client.Unwrap()
	count, err := rc.ZCard(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("queue length: redis error: %w", err)
	}

	return count, nil
}

// List returns all entries in the queue, ordered by priority (highest first).
func (q *Queue) List(ctx context.Context, repo string) ([]QueueEntry, error) {
	key, err := redis.BuildKey(repo, "queue", "pending")
	if err != nil {
		return nil, fmt.Errorf("list queue: %w", err)
	}

	rc := q.client.Unwrap()
	members, err := rc.ZRangeWithScores(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("list queue: redis error: %w", err)
	}

	entries := make([]QueueEntry, len(members))
	for i, m := range members {
		entries[i] = QueueEntry{
			Repo:     repo,
			Branch:   m.Member.(string),
			Priority: QueuePriority(m.Score),
		}
	}

	return entries, nil
}

// UpdatePriority changes the priority of a branch in the queue.
// Returns nil even if the branch was not in the queue.
func (q *Queue) UpdatePriority(ctx context.Context, repo, branch string, priority QueuePriority) error {
	key, err := redis.BuildKey(repo, "queue", "pending")
	if err != nil {
		return fmt.Errorf("update priority: %w", err)
	}

	rc := q.client.Unwrap()
	_, err = rc.ZAdd(ctx, key, goredis.Z{
		Score:  float64(priority),
		Member: branch,
	}).Result()
	if err != nil {
		return fmt.Errorf("update priority: redis error: %w", err)
	}

	return nil
}

// ComputeWFQPriority calculates priority for weighted-fair-queue scheduling.
// Lower priority = processed first.
// The formula accounts for wait time and branch weight to ensure fairness.
//
// Priority = virtualTime + (1.0 / weight)
// Where virtualTime increases over time to prevent starvation.
func ComputeWFQPriority(virtualTime float64, weight float64) QueuePriority {
	if weight <= 0 {
		weight = 1.0 // default weight
	}
	return QueuePriority(virtualTime + (1.0 / weight))
}

// VirtualTime tracks global virtual time for WFQ scheduling.
// Higher weights get proportionally more service.
type VirtualTime struct {
	client *redis.Client
}

// NewVirtualTime creates a virtual time tracker.
func NewVirtualTime(client *redis.Client) *VirtualTime {
	return &VirtualTime{client: client}
}

// Get retrieves the current virtual time for a repo.
// Returns 0 if not set.
func (vt *VirtualTime) Get(ctx context.Context, repo string) (float64, error) {
	key, err := redis.BuildKey(repo, "queue", "vtime")
	if err != nil {
		return 0, fmt.Errorf("get virtual time: %w", err)
	}

	rc := vt.client.Unwrap()
	val, err := rc.Get(ctx, key).Float64()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return 0, nil
		}
		return 0, fmt.Errorf("get virtual time: redis error: %w", err)
	}

	return val, nil
}

// Advance increments the virtual time for a repo.
// The increment is based on the weight of the just-serviced branch.
// delta = 1.0 / weight (so higher weights advance time more slowly per service).
func (vt *VirtualTime) Advance(ctx context.Context, repo string, weight float64) error {
	key, err := redis.BuildKey(repo, "queue", "vtime")
	if err != nil {
		return fmt.Errorf("advance virtual time: %w", err)
	}

	if weight <= 0 {
		weight = 1.0
	}
	delta := 1.0 / weight

	rc := vt.client.Unwrap()
	_, err = rc.IncrByFloat(ctx, key, delta).Result()
	if err != nil {
		return fmt.Errorf("advance virtual time: redis error: %w", err)
	}

	return nil
}

// Reset sets virtual time back to 0 (e.g., for testing or reset).
func (vt *VirtualTime) Reset(ctx context.Context, repo string) error {
	key, err := redis.BuildKey(repo, "queue", "vtime")
	if err != nil {
		return fmt.Errorf("reset virtual time: %w", err)
	}

	rc := vt.client.Unwrap()
	_, err = rc.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("reset virtual time: redis error: %w", err)
	}

	return nil
}

// ComputeAgingPriority adds aging bonus to prevent starvation.
// Branches that have waited longer get lower (better) priority.
// basePriority + (waitTime.Seconds() * agingFactor)
func ComputeAgingPriority(basePriority QueuePriority, waitTime time.Duration, agingFactor float64) QueuePriority {
	if agingFactor <= 0 {
		agingFactor = 0.01 // default: small aging bonus
	}
	agingBonus := waitTime.Seconds() * agingFactor
	// Apply as a negative to improve priority (lower score = better)
	return QueuePriority(float64(basePriority) - agingBonus)
}

// Weight constants for different branch types.
const (
	WeightDefault      = 1.0
	WeightFeature      = 1.0
	WeightHotfix       = 2.0 // hotfixes get higher weight (more share)
	WeightRelease      = 1.5
	WeightExperimental = 0.5 // lower weight for experiments
)

// ClassifyWeight returns an appropriate weight for a branch based on naming.
// This is a heuristic; the caller can override with explicit weight.
func ClassifyWeight(branch string) float64 {
	// Simple prefix-based classification
	switch {
	case hasPrefix(branch, "hotfix/", "urgent/", "critical/"):
		return WeightHotfix
	case hasPrefix(branch, "release/", "rel/"):
		return WeightRelease
	case hasPrefix(branch, "experiment/", "exp/", "poc/"):
		return WeightExperimental
	default:
		return WeightDefault
	}
}

func hasPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}
