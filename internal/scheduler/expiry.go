package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/codero/codero/internal/delivery"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

var (
	// SessionHeartbeatTTL is how long a branch can go without a session heartbeat
	// before it is considered abandoned (T14). Defaults to the roadmap spec
	// (1800s), but allows a shorter pilot override for integration testing.
	SessionHeartbeatTTL = sessionHeartbeatTTLFromEnv()
)

const (
	// LeaseAuditInterval is how often the lease audit goroutine runs.
	// Appendix G: "Lease audit goroutine runs every 30 seconds."
	LeaseAuditInterval = 30 * time.Second

	// SessionExpiryInterval is how often the session expiry goroutine runs.
	SessionExpiryInterval = 60 * time.Second

	// BranchHoldWarningTTL is the default age where Codero warns on prolonged
	// branch ownership.
	BranchHoldWarningTTL = 72 * time.Hour

	// BranchHoldReleaseTTL is the default hard cutoff where Codero cancels the
	// active assignment and releases branch ownership.
	BranchHoldReleaseTTL = BranchHoldWarningTTL + (BranchHoldWarningTTL / 2)
)

func sessionHeartbeatTTLFromEnv() time.Duration {
	const defaultSeconds = 1800

	raw := os.Getenv("CODERO_SESSION_HEARTBEAT_TTL_SECONDS")
	if raw == "" {
		return defaultSeconds * time.Second
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultSeconds * time.Second
	}

	return time.Duration(seconds) * time.Second
}

// ExpiryWorker runs two background routines:
//  1. Session heartbeat expiry: expires agent sessions and legacy branch
//     ownership records (T14).
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

// runSessionExpiry expires agent sessions when the new table exists, then
// falls back to the legacy branch ownership expiry path so old fixtures and
// branch-state-only deployments still behave correctly.
func (w *ExpiryWorker) runSessionExpiry(ctx context.Context) {
	hasAgentSessions, err := hasTable(ctx, w.db, "agent_sessions")
	if err != nil {
		loglib.Error("expiry: table lookup failed",
			loglib.FieldComponent, "expiry",
			"error", err,
		)
		hasAgentSessions = false
	}

	if hasAgentSessions {
		expired, err := state.ListExpiredAgentSessions(ctx, w.db, SessionHeartbeatTTL)
		if err != nil {
			loglib.Error("expiry: list expired agent sessions failed",
				loglib.FieldComponent, "expiry",
				"error", err,
			)
		} else {
			for _, session := range expired {
				if err := w.expireAgentSession(ctx, session); err != nil {
					loglib.Error("expiry: expire agent session failed",
						loglib.FieldComponent, "expiry",
						"session_id", session.SessionID,
						"agent_id", session.AgentID,
						"error", err,
					)
				}
			}
		}

		if err := w.auditAssignmentHolds(ctx); err != nil {
			loglib.Error("expiry: assignment hold audit failed",
				loglib.FieldComponent, "expiry",
				"error", err,
			)
		}
	}

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

func (w *ExpiryWorker) auditAssignmentHolds(ctx context.Context) error {
	assignments, err := state.ListActiveAgentAssignments(ctx, w.db)
	if err != nil {
		return fmt.Errorf("audit assignment holds: list active assignments: %w", err)
	}

	now := time.Now().UTC()
	for _, assignment := range assignments {
		age := now.Sub(assignment.StartedAt)
		if age >= BranchHoldReleaseTTL {
			if err := w.releaseHeldAssignment(ctx, &assignment, age); err != nil {
				return fmt.Errorf("audit assignment holds: release %s: %w", assignment.ID, err)
			}
			continue
		}
		if age >= BranchHoldWarningTTL {
			if err := w.warnHeldAssignment(ctx, &assignment, age); err != nil {
				return fmt.Errorf("audit assignment holds: warn %s: %w", assignment.ID, err)
			}
		}
	}

	return nil
}

// expireAgentSession marks a durable agent session and its active assignment
// as expired in the new session tables.
func (w *ExpiryWorker) expireAgentSession(ctx context.Context, session state.AgentSession) error {
	if err := state.ExpireAgentSession(ctx, w.db, session.SessionID, "expired"); err != nil {
		return fmt.Errorf("expire agent session %s: %w", session.SessionID, err)
	}

	loglib.Warn("expiry: agent session expired",
		loglib.FieldEventType, loglib.EventTransition,
		loglib.FieldComponent, "expiry",
		loglib.FieldSession, session.SessionID,
		"agent_id", session.AgentID,
		"mode", session.Mode,
	)

	return nil
}

func (w *ExpiryWorker) warnHeldAssignment(ctx context.Context, assignment *state.AgentAssignment, age time.Duration) error {
	check, err := state.GetAssignmentRuleCheck(ctx, w.db, assignment.ID, "RULE-003")
	if err != nil {
		return fmt.Errorf("warn held assignment: get rule check: %w", err)
	}
	if check.Result == "warn" || check.Result == "fail" {
		return nil
	}

	detail, err := marshalHoldTTLDetail("hold_ttl_warning", age, BranchHoldWarningTTL, BranchHoldReleaseTTL)
	if err != nil {
		return fmt.Errorf("warn held assignment: marshal detail: %w", err)
	}
	if err := state.WarnAssignmentHoldTTL(ctx, w.db, assignment, detail); err != nil {
		return fmt.Errorf("warn held assignment: %w", err)
	}

	loglib.Warn("expiry: assignment hold ttl warning",
		loglib.FieldComponent, "expiry",
		loglib.FieldSession, assignment.SessionID,
		loglib.FieldRepo, assignment.Repo,
		loglib.FieldBranch, assignment.Branch,
		"assignment_id", assignment.ID,
		"age_hours", age.Hours(),
	)
	return nil
}

func (w *ExpiryWorker) releaseHeldAssignment(ctx context.Context, assignment *state.AgentAssignment, age time.Duration) error {
	check, err := state.GetAssignmentRuleCheck(ctx, w.db, assignment.ID, "RULE-003")
	if err != nil {
		return fmt.Errorf("release held assignment: get rule check: %w", err)
	}
	if check.Result == "fail" {
		return nil
	}

	detail, err := marshalHoldTTLDetail("hold_ttl_released", age, BranchHoldWarningTTL, BranchHoldReleaseTTL)
	if err != nil {
		return fmt.Errorf("release held assignment: marshal detail: %w", err)
	}
	if err := state.ReleaseAssignmentForHoldTTL(ctx, w.db, assignment, detail); err != nil {
		return fmt.Errorf("release held assignment: %w", err)
	}

	loglib.Warn("expiry: assignment released after hold ttl exceeded",
		loglib.FieldComponent, "expiry",
		loglib.FieldSession, assignment.SessionID,
		loglib.FieldRepo, assignment.Repo,
		loglib.FieldBranch, assignment.Branch,
		"assignment_id", assignment.ID,
		"age_hours", age.Hours(),
	)
	return nil
}

func marshalHoldTTLDetail(source string, age, warningTTL, releaseTTL time.Duration) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"source":              source,
		"age_seconds":         int(age.Seconds()),
		"warning_ttl_seconds": int(warningTTL.Seconds()),
		"release_ttl_seconds": int(releaseTTL.Seconds()),
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// expireSession transitions one branch to abandoned (T14) and delivers a system event.
func (w *ExpiryWorker) expireSession(ctx context.Context, b state.BranchRecord) error {
	if err := state.TransitionBranch(w.db, b.ID, b.State, state.StateAbandoned, "session_heartbeat_expired"); err != nil {
		return fmt.Errorf("expire session %s: %w", b.ID, err)
	}

	loglib.Warn("expiry: branch abandoned due to session heartbeat TTL",
		loglib.FieldEventType, loglib.EventTransition,
		loglib.FieldComponent, "expiry",
		loglib.FieldRepo, b.Repo,
		loglib.FieldBranch, b.Branch,
		loglib.FieldFromState, string(b.State),
		loglib.FieldToState, string(state.StateAbandoned),
	)

	if _, err := w.stream.AppendSystem(ctx, b.Repo, b.Branch, b.HeadHash,
		"session_expired",
		"branch abandoned due to session heartbeat TTL expiry; use 'codero reactivate' to restore",
	); err != nil {
		loglib.Warn("expiry: append session_expired event failed",
			loglib.FieldComponent, "expiry",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
	}
	return nil
}

func hasTable(ctx context.Context, db *state.DB, name string) (bool, error) {
	var found string
	err := db.Unwrap().QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
		name,
	).Scan(&found)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check table %s: %w", name, err)
	}
	return true, nil
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
//   - enqueues then clears durable lease info and transitions to queued_cli (T07)
//
// The enqueue step is done before ClearLeaseInfo in the re-queue path so that
// on Redis failure the lease_expires_at remains set and the next audit cycle
// will automatically retry without losing the branch.
func (w *ExpiryWorker) auditExpiredLease(ctx context.Context, b state.BranchRecord) error {
	newCount, err := state.IncrementRetryCount(w.db, b.ID)
	if err != nil {
		return fmt.Errorf("audit expired lease %s: increment retry: %w", b.ID, err)
	}

	if _, err := w.stream.AppendSystem(ctx, b.Repo, b.Branch, b.HeadHash,
		"lease_expired",
		"review lease expired without completion; branch re-queued or blocked",
	); err != nil {
		loglib.Warn("expiry: append lease_expired event failed",
			loglib.FieldComponent, "expiry",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
	}

	if newCount >= b.MaxRetries {
		// T16: blocked — clear lease then transition.
		if err := state.ClearLeaseInfo(w.db, b.ID); err != nil {
			loglib.Warn("expiry: clear lease info failed",
				loglib.FieldComponent, "expiry",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
		if err := state.TransitionBranch(w.db, b.ID, state.StateCLIReviewing, state.StateBlocked, "lease_expired_max_retries"); err != nil {
			return fmt.Errorf("audit expired lease %s: transition to blocked: %w", b.ID, err)
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
		// T07: queued_cli — enqueue first so that on Redis failure the branch
		// remains in cli_reviewing with lease_expires_at intact for the next cycle.
		if err := w.queue.Enqueue(ctx, QueueEntry{
			Repo:     b.Repo,
			Branch:   b.Branch,
			Priority: QueuePriority(b.QueuePriority),
		}); err != nil {
			loglib.Warn("expiry: re-enqueue failed; branch stays in cli_reviewing for next audit cycle",
				loglib.FieldComponent, "expiry",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
			return fmt.Errorf("audit expired lease %s: re-enqueue: %w", b.ID, err)
		}
		if err := state.ClearLeaseInfo(w.db, b.ID); err != nil {
			loglib.Warn("expiry: clear lease info failed",
				loglib.FieldComponent, "expiry",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
		if err := state.TransitionBranch(w.db, b.ID, state.StateCLIReviewing, state.StateQueuedCLI, "lease_expired_requeue"); err != nil {
			return fmt.Errorf("audit expired lease %s: transition to queued_cli: %w", b.ID, err)
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
