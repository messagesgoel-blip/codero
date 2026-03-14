package scheduler

import (
	"context"
	"database/sql"
	"fmt"
)

// StallDetector checks whether the dispatch queue is stalled.
// A queue is stalled when all eligible queued items are blocked by retry limits
// (retry_count >= max_retries). An empty queue is NOT considered stalled.
//
// This implements the queue_stalled contract from codero-roadmap-v5.md:
// queue_stalled fires when all eligible queued items are blocked by retry limits.
// When queue_stalled fires, dispatch halts and an event is emitted for operator intervention.
type StallDetector struct {
	queue    *Queue  // The WFQ queue to monitor
	db       *sql.DB // SQLite state store for retry count queries
	maxRatio float64 // Maximum ratio of blocked items to tolerate before declaring stalled (default 1.0 = 100%)
}

// NewStallDetector creates a stall detector for monitoring a repository's dispatch queue.
// The detector checks if all eligible queued items are blocked by retry limits,
// which indicates a stalled queue requiring operator intervention.
func NewStallDetector(queue *Queue, db *sql.DB) *StallDetector {
	return &StallDetector{
		queue:    queue,
		db:       db,
		maxRatio: 1.0, // 100% blocked = stalled
	}
}

// StallStatus represents the current state of the queue with respect to stalling.
// It provides details about queue size, blocked items, and whether the queue is stalled.
type StallStatus struct {
	QueueEmpty   bool    // true if queue has no items
	TotalItems   int64   // total number of items currently in the queue
	BlockedItems int64   // number of items blocked due to exceeding retry limits
	BlockedRatio float64 // ratio of blocked items to total items (0.0 to 1.0)
	IsStalled    bool    // true if queue is stalled (all eligible items blocked)
}

// CheckStalled determines if the queue is stalled.
// Returns StallStatus with details about the queue state.
func (sd *StallDetector) CheckStalled(ctx context.Context, repo string) (*StallStatus, error) {
	if sd == nil {
		return nil, fmt.Errorf("stall check: detector is nil")
	}
	if sd.queue == nil {
		return nil, fmt.Errorf("stall check: detector queue not configured")
	}
	if sd.db == nil {
		return nil, fmt.Errorf("stall check: detector db not configured")
	}

	// Get all items in queue
	entries, err := sd.queue.List(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("stall check: list queue: %w", err)
	}

	status := &StallStatus{
		TotalItems:   int64(len(entries)),
		QueueEmpty:   len(entries) == 0,
		BlockedItems: 0,
		BlockedRatio: 0,
		IsStalled:    false,
	}

	if len(entries) == 0 {
		// Empty queue is not stalled - it's just empty
		// Queue is only stalled when there ARE items but they're all blocked
		status.IsStalled = false
		return status, nil
	}

	// Count blocked items (retry_count >= max_retries)
	for _, entry := range entries {
		blocked, err := sd.isBranchBlocked(ctx, repo, entry.Branch)
		if err != nil {
			return nil, fmt.Errorf("stall check: check branch %s: %w", entry.Branch, err)
		}
		if blocked {
			status.BlockedItems++
		}
	}

	status.BlockedRatio = float64(status.BlockedItems) / float64(status.TotalItems)

	// Queue is stalled when all eligible items are blocked
	// (i.e., blocked ratio >= maxRatio, meaning 100% blocked)
	status.IsStalled = status.BlockedRatio >= sd.maxRatio && status.TotalItems > 0

	return status, nil
}

// isBranchBlocked queries the SQLite state store to determine if a branch has
// exceeded its maximum retry count. Returns true if retry_count >= max_retries.
func (sd *StallDetector) isBranchBlocked(ctx context.Context, repo, branch string) (bool, error) {
	var retryCount, maxRetries int
	err := sd.db.QueryRowContext(ctx,
		"SELECT retry_count, max_retries FROM branch_states WHERE repo = ? AND branch = ?",
		repo, branch,
	).Scan(&retryCount, &maxRetries)

	if err == sql.ErrNoRows {
		// Branch not in state DB - not blocked
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query branch state: %w", err)
	}

	return retryCount >= maxRetries, nil
}

// MaxRetriesDefault is the default max retry count.
const MaxRetriesDefault = 3
