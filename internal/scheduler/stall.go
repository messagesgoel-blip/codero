package scheduler

import (
	"context"
	"database/sql"
	"fmt"
)

// StallDetector checks whether the dispatch queue is stalled.
// A queue is stalled when all eligible queued items are either:
// 1. Exhausted (queue empty)
// 2. Blocked by retry limits (retry_count >= max_retries)
type StallDetector struct {
	queue    *Queue
	db       *sql.DB
	maxRatio float64 // maximum ratio of blocked items to tolerate before declaring stalled
}

// NewStallDetector creates a stall detector for a repo.
func NewStallDetector(queue *Queue, db *sql.DB) *StallDetector {
	return &StallDetector{
		queue:    queue,
		db:       db,
		maxRatio: 1.0, // 100% blocked = stalled
	}
}

// StallStatus represents the current state of the queue with respect to stalling.
type StallStatus struct {
	QueueEmpty   bool    // true if queue has no items
	TotalItems   int64   // total items in queue
	BlockedItems int64   // items blocked by retry limit
	BlockedRatio float64 // ratio of blocked to total
	IsStalled    bool    // true if queue is stalled
}

// CheckStalled determines if the queue is stalled.
// Returns StallStatus with details about the queue state.
func (sd *StallDetector) CheckStalled(ctx context.Context, repo string) (*StallStatus, error) {
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

// isBranchBlocked checks if a branch has exceeded its retry limit.
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
