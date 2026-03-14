package scheduler

import (
	"context"
	"time"

	"github.com/codero/codero/internal/delivery"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

const (
	// SessionHeartbeatTTL is how long a branch can go without a session heartbeat
	// before it is considered abandoned (T14). Matches the roadmap spec (1800s).
	SessionHeartbeatTTL = 1800 * time.Second

	// LeaseAuditInterval is how often the lease audit goroutine runs.
	// Appendix G: "Lease audit goroutine runs every 30 seconds."
	LeaseAuditInterval = 30 * time.Second

	// SessionExpiryInterval is how often the session expiry goroutine runs.
	SessionExpiryInterval = 60 * time.Second
)

// ExpiryWorker runs two background routines:
//  1. Session heartbeat expiry: transitions abandoned sessions (T14).
//  2. Lease audit: detects cli_reviewing branches with expired durable leases
//     and re-queues them (T07), acting as a safety net when Redis lease key
//     expiry events are missed.
type ExpiryWorker struct {
	db     *state.DB
	queue  *Queue
	stream *delivery.Stream
}

// NewExpiryWorker creates an ExpiryWorker.
func NewExpiryWorker(db *state.DB, queue *Queue, stream *delivery.Stream) *ExpiryWorker {
	return &ExpiryWorker{db: db, queue: queue, stream: stream}
}

// Run starts both the session expiry and lease audit goroutines.
// It blocks until ctx is cancelled.
func (w *ExpiryWorker) Run(ctx context.Context) {
	w.RunWithIntervals(ctx, SessionExpiryInterval, LeaseAuditInterval)
}

// RunWithIntervals is like Run but accepts custom tick intervals.
// Exported for use in tests with short intervals.
func (w *ExpiryWorker) RunWithIntervals(ctx context.Context, sessionInterval, leaseInterval time.Duration) {
	loglib.Info("expiry: worker starting",
		loglib.FieldEventType, loglib.EventStartup,
		loglib.FieldComponent, "expiry",
	)

	sessionTicker := time.NewTicker(sessionInterval)
	leaseTicker := time.NewTicker(leaseInterval)
	defer sessionTicker.Stop()
	defer leaseTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			loglib.Info("expiry: worker stopped",
				loglib.FieldEventType, loglib.EventShutdown,
				loglib.FieldComponent, "expiry",
			)
			return
		case <-sessionTicker.C:
			w.runSessionExpiry(ctx)
		case <-leaseTicker.C:
			w.runLeaseAudit(ctx)
		}
	}
}

// RunSessionExpiryCycle runs one session expiry cycle. Exported for testing.
func (w *ExpiryWorker) RunSessionExpiryCycle(ctx context.Context) {
	w.runSessionExpiry(ctx)
}

// RunLeaseAuditCycle runs one lease audit cycle. Exported for testing.
func (w *ExpiryWorker) RunLeaseAuditCycle(ctx context.Context) {
	w.runLeaseAudit(ctx)
}

// runSessionExpiry detects branches whose owner_session_last_seen has passed
// SessionHeartbeatTTL and transitions them to abandoned (T14).
func (w *ExpiryWorker) runSessionExpiry(ctx context.Context) {
	expired, err := state.ListExpiredSessions(w.db, SessionHeartbeatTTL)
	if err != nil {
		loglib.Error("expiry: list expired sessions failed",
			loglib.FieldComponent, "expiry",
			"error", err,
		)
		return
	}

	for _, b := range expired {
		if err := w.expireSession(ctx, b); err != nil {
			loglib.Error("expiry: abandon session failed",
				loglib.FieldComponent, "expiry",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
	}
}

// expireSession transitions one branch to abandoned (T14) and delivers a system event.
func (w *ExpiryWorker) expireSession(ctx context.Context, b state.BranchRecord) error {
	if err := state.TransitionBranch(w.db, b.ID, b.State, state.StateAbandoned, "session_heartbeat_expired"); err != nil {
		return err
	}

	loglib.Warn("expiry: branch abandoned due to session heartbeat TTL",
		loglib.FieldEventType, loglib.EventTransition,
		loglib.FieldComponent, "expiry",
		loglib.FieldRepo, b.Repo,
		loglib.FieldBranch, b.Branch,
		loglib.FieldFromState, string(b.State),
		loglib.FieldToState, string(state.StateAbandoned),
	)

	_, _ = w.stream.AppendSystem(ctx, b.Repo, b.Branch, b.HeadHash,
		"session_expired",
		"branch abandoned due to session heartbeat TTL expiry; use 'codero reactivate' to restore",
	)
	return nil
}

// runLeaseAudit scans for cli_reviewing branches whose durable lease_expires_at
// has passed and re-queues or blocks them (T07 / T16 semantics). This is the
// safety-net path when Redis lease key expiry events are unavailable.
func (w *ExpiryWorker) runLeaseAudit(ctx context.Context) {
	expired, err := state.ListExpiredLeases(w.db)
	if err != nil {
		loglib.Error("expiry: list expired leases failed",
			loglib.FieldComponent, "expiry",
			"error", err,
		)
		return
	}

	for _, b := range expired {
		if err := w.auditExpiredLease(ctx, b); err != nil {
			loglib.Error("expiry: audit expired lease failed",
				loglib.FieldComponent, "expiry",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
	}
}

// auditExpiredLease handles a cli_reviewing branch whose lease has expired:
//   - increments retry_count
//   - transitions to blocked (T16) if max retries exceeded
//   - transitions to queued_cli (T07) and re-enqueues otherwise
//   - clears durable lease info and appends a system bundle
func (w *ExpiryWorker) auditExpiredLease(ctx context.Context, b state.BranchRecord) error {
	// Clear stale lease info.
	if err := state.ClearLeaseInfo(w.db, b.ID); err != nil {
		loglib.Warn("expiry: clear lease info failed",
			loglib.FieldComponent, "expiry",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
	}

	newCount, err := state.IncrementRetryCount(w.db, b.ID)
	if err != nil {
		return err
	}

	_, _ = w.stream.AppendSystem(ctx, b.Repo, b.Branch, b.HeadHash,
		"lease_expired",
		"review lease expired without completion; branch re-queued or blocked",
	)

	if newCount >= b.MaxRetries {
		// T16: blocked.
		if err := state.TransitionBranch(w.db, b.ID, state.StateCLIReviewing, state.StateBlocked, "lease_expired_max_retries"); err != nil {
			return err
		}
		loglib.Warn("expiry: branch blocked after lease expiry (max retries)",
			loglib.FieldEventType, loglib.EventTransition,
			loglib.FieldComponent, "expiry",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			loglib.FieldFromState, string(state.StateCLIReviewing),
			loglib.FieldToState, string(state.StateBlocked),
			"retry_count", newCount,
		)
	} else {
		// T07: queued_cli.
		if err := state.TransitionBranch(w.db, b.ID, state.StateCLIReviewing, state.StateQueuedCLI, "lease_expired_requeue"); err != nil {
			return err
		}
		if err := w.queue.Enqueue(ctx, QueueEntry{
			Repo:     b.Repo,
			Branch:   b.Branch,
			Priority: QueuePriority(b.QueuePriority),
		}); err != nil {
			loglib.Warn("expiry: re-enqueue failed",
				loglib.FieldComponent, "expiry",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
		loglib.Info("expiry: branch re-queued after lease expiry",
			loglib.FieldEventType, loglib.EventTransition,
			loglib.FieldComponent, "expiry",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			loglib.FieldFromState, string(state.StateCLIReviewing),
			loglib.FieldToState, string(state.StateQueuedCLI),
			"retry_count", newCount,
		)
	}

	return nil
}
