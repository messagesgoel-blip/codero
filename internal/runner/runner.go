package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/codero/codero/internal/delivery"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/normalizer"
	"github.com/codero/codero/internal/scheduler"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
	"github.com/google/uuid"
)

const (
	defaultPollInterval      = 10 * time.Second
	defaultLeaseTTL          = 30 * time.Second
	defaultHeartbeatInterval = 10 * time.Second
)

// Config configures the ReviewRunner.
type Config struct {
	Repos             []string
	PollInterval      time.Duration
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
	MaxConcurrent     int // max concurrent reviews (0 = 1)
}

// defaults fills in zero-value fields.
func (c *Config) defaults() {
	if c.PollInterval <= 0 {
		c.PollInterval = defaultPollInterval
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = defaultLeaseTTL
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = defaultHeartbeatInterval
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 1
	}
}

// ReviewRunner consumes eligible queued_cli branches, acquires leases, executes
// review workflows, and records deterministic state transitions. It is the
// central dispatch engine for Sprint 5.
type ReviewRunner struct {
	db              *state.DB
	queue           *scheduler.Queue
	leaseMgr        *scheduler.LeaseManager
	stream          *delivery.Stream
	provider        Provider
	cfg             Config
	sessionStore    *session.Store
	sessionID       string
	sessionAgentID  string
	sessionMode     string
	sessionWorktree string
	sessionTaskID   string
}

// New creates a ReviewRunner with the given dependencies.
func New(
	db *state.DB,
	queue *scheduler.Queue,
	leaseMgr *scheduler.LeaseManager,
	stream *delivery.Stream,
	provider Provider,
	cfg Config,
) *ReviewRunner {
	cfg.defaults()
	agentID := resolveAgentID()
	return &ReviewRunner{
		db:              db,
		queue:           queue,
		leaseMgr:        leaseMgr,
		stream:          stream,
		provider:        provider,
		cfg:             cfg,
		sessionStore:    session.NewStore(db),
		sessionID:       resolveSessionID(),
		sessionAgentID:  agentID,
		sessionMode:     resolveSessionMode(),
		sessionWorktree: resolveWorktree(),
		sessionTaskID:   resolveTaskID(),
	}
}

// Run starts the dispatch loop. It polls all configured repos for queued_cli
// branches and processes them. Run blocks until ctx is cancelled.
func (r *ReviewRunner) Run(ctx context.Context) {
	loglib.Info("runner: dispatch loop starting",
		loglib.FieldEventType, loglib.EventStartup,
		loglib.FieldComponent, "runner",
		"repos", r.cfg.Repos,
		"poll_interval", r.cfg.PollInterval,
	)

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	r.registerSession(ctx)

	var sessionTick <-chan time.Time
	if r.sessionStore != nil && r.sessionID != "" {
		sessionTicker := time.NewTicker(r.cfg.HeartbeatInterval)
		defer sessionTicker.Stop()
		sessionTick = sessionTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			loglib.Info("runner: dispatch loop stopped",
				loglib.FieldEventType, loglib.EventShutdown,
				loglib.FieldComponent, "runner",
			)
			return
		case <-sessionTick:
			r.emitSessionHeartbeat(ctx)
		case <-ticker.C:
			for _, repo := range r.cfg.Repos {
				r.dispatchRepo(ctx, repo)
			}
		}
	}
}

// dispatchRepo dequeues and processes one branch from the given repo.
func (r *ReviewRunner) dispatchRepo(ctx context.Context, repo string) {
	entry, err := r.queue.Dequeue(ctx, repo)
	if err != nil {
		// ErrQueueEmpty is normal; any other error is notable.
		if err != scheduler.ErrQueueEmpty {
			loglib.Error("runner: dequeue failed",
				loglib.FieldComponent, "runner",
				loglib.FieldRepo, repo,
				"error", err,
			)
		}
		return
	}

	loglib.Info("runner: dispatching branch",
		loglib.FieldComponent, "runner",
		loglib.FieldRepo, repo,
		loglib.FieldBranch, entry.Branch,
	)

	if err := r.processEntry(ctx, repo, entry.Branch); err != nil {
		loglib.Error("runner: process entry failed",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, entry.Branch,
			"error", err,
		)
	}
}

// processEntry runs the full review lifecycle for one branch.
// All state transitions are deterministic and audit-logged.
func (r *ReviewRunner) processEntry(ctx context.Context, repo, branch string) error {
	// Look up branch record.
	rec, err := state.GetBranch(r.db, repo, branch)
	if err != nil {
		return fmt.Errorf("get branch %s/%s: %w", repo, branch, err)
	}

	// Validate precondition: must be in queued_cli.
	if rec.State != state.StateQueuedCLI {
		loglib.Warn("runner: branch not in queued_cli, skipping",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"state", rec.State,
		)
		return nil
	}

	// Acquire lease atomically.
	holderID := newHolderID()
	lease, err := r.leaseMgr.AcquireWithTTL(ctx, repo, branch, holderID, r.cfg.LeaseTTL)
	if err != nil {
		if errors.Is(err, scheduler.ErrLeaseConflict) {
			loglib.Info("runner: lease conflict, skipping",
				loglib.FieldComponent, "runner",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
			)
			return nil
		}
		return fmt.Errorf("acquire lease: %w", err)
	}

	// Transition: queued_cli → cli_reviewing (T06).
	if err := state.TransitionBranch(r.db, rec.ID, state.StateQueuedCLI, state.StateCLIReviewing, "runner_dispatch"); err != nil {
		_ = r.leaseMgr.Release(ctx, repo, branch, holderID)
		return fmt.Errorf("transition to cli_reviewing: %w", err)
	}

	r.attachSessionAssignment(ctx, repo, branch)

	// Record the agent identity so the dashboard can display it.
	if agentID := r.sessionAgentID; agentID != "" {
		if err := state.UpdateOwnerAgent(ctx, r.db, repo, branch, agentID); err != nil {
			loglib.Warn("runner: failed to record owner agent",
				loglib.FieldComponent, "runner",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		}
	}

	// Record lease info in durable store for crash recovery.
	if err := state.UpdateLeaseInfo(r.db, rec.ID, holderID, lease.ExpiresAt); err != nil {
		loglib.Warn("runner: failed to record lease info",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}

	// Start heartbeat to keep lease alive during review.
	hbCfg := scheduler.HeartbeatConfig{
		Interval:  r.cfg.HeartbeatInterval,
		LeaseTTL:  r.cfg.LeaseTTL,
		MaxMisses: 3,
	}
	hb, err := r.leaseMgr.StartHeartbeat(ctx, lease, hbCfg)
	if err != nil {
		_ = r.leaseMgr.Release(ctx, repo, branch, holderID)
		return fmt.Errorf("start heartbeat: %w", err)
	}

	// Create review run record.
	runID := uuid.New().String()
	now := time.Now()
	run := &state.ReviewRun{
		ID:        runID,
		Repo:      repo,
		Branch:    branch,
		HeadHash:  rec.HeadHash,
		Provider:  r.provider.Name(),
		Status:    "running",
		StartedAt: &now,
	}
	if err := state.CreateReviewRun(r.db, run); err != nil {
		loglib.Warn("runner: failed to record review run",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}

	// Execute review.
	req := ReviewRequest{
		Repo:     repo,
		Branch:   branch,
		HeadHash: rec.HeadHash,
	}
	resp, reviewErr := r.provider.Review(ctx, req)

	// Stop heartbeat before state transition.
	hb.Stop()

	// Clear durable lease info.
	_ = state.ClearLeaseInfo(r.db, rec.ID)

	if reviewErr != nil {
		return r.handleReviewFailure(ctx, rec, repo, branch, holderID, runID, reviewErr)
	}
	return r.handleReviewSuccess(ctx, rec, repo, branch, holderID, runID, resp)
}

// handleReviewSuccess normalizes findings, delivers them, and transitions to reviewed (T08).
func (r *ReviewRunner) handleReviewSuccess(
	ctx context.Context,
	rec *state.BranchRecord,
	repo, branch, holderID, runID string,
	resp *ReviewResponse,
) error {
	defer func() { _ = r.leaseMgr.Release(ctx, repo, branch, holderID) }()

	now := time.Now()

	// Normalize findings.
	findings, normErrs := normalizer.NormalizeAll(resp.Findings, r.provider.Name(), now)
	for _, e := range normErrs {
		loglib.Warn("runner: normalizer error (finding skipped)",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", e,
		)
	}

	// Persist normalized findings in a single transaction.
	findingRecords := make([]*state.FindingRecord, 0, len(findings))
	for _, f := range findings {
		findingRecords = append(findingRecords, &state.FindingRecord{
			ID:        uuid.New().String(),
			RunID:     runID,
			Repo:      repo,
			Branch:    branch,
			Severity:  string(f.Severity),
			Category:  f.Category,
			File:      f.File,
			Line:      f.Line,
			Message:   f.Message,
			Source:    f.Source,
			RuleID:    f.RuleID,
			Timestamp: f.Timestamp,
		})
	}
	if err := state.InsertFindings(r.db, findingRecords); err != nil {
		loglib.Warn("runner: failed to persist findings",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}

	// Update review run as completed.
	finishedAt := time.Now()
	if err := state.UpdateReviewRun(r.db, runID, "completed", "", finishedAt); err != nil {
		loglib.Warn("runner: failed to update review run",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}

	// Deliver findings bundle to stream.
	_, streamErr := r.stream.AppendFindingBundle(ctx, repo, branch, rec.HeadHash, delivery.FindingBundlePayload{
		RunID:    runID,
		Provider: r.provider.Name(),
		Findings: findings,
	})
	if streamErr != nil {
		loglib.Warn("runner: delivery stream append failed",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", streamErr,
		)
	}

	// Transition: cli_reviewing → reviewed (T08).
	if err := state.TransitionBranch(r.db, rec.ID, state.StateCLIReviewing, state.StateReviewed, "review_completed"); err != nil {
		return fmt.Errorf("transition to reviewed: %w", err)
	}

	loglib.Info("runner: review completed",
		loglib.FieldEventType, loglib.EventTransition,
		loglib.FieldComponent, "runner",
		loglib.FieldRepo, repo,
		loglib.FieldBranch, branch,
		loglib.FieldFromState, string(state.StateCLIReviewing),
		loglib.FieldToState, string(state.StateReviewed),
		"findings", len(findings),
		"run_id", runID,
	)

	return nil
}

// handleReviewFailure increments retry count and transitions to queued_cli
// (T07) or blocked (T16) depending on retry count.
func (r *ReviewRunner) handleReviewFailure(
	ctx context.Context,
	rec *state.BranchRecord,
	repo, branch, holderID, runID string,
	reviewErr error,
) error {
	defer func() { _ = r.leaseMgr.Release(ctx, repo, branch, holderID) }()

	// Update review run as failed.
	finishedAt := time.Now()
	if err := state.UpdateReviewRun(r.db, runID, "failed", reviewErr.Error(), finishedAt); err != nil {
		loglib.Warn("runner: failed to update review run (failure path)",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}

	loglib.Warn("runner: review failed",
		loglib.FieldComponent, "runner",
		loglib.FieldRepo, repo,
		loglib.FieldBranch, branch,
		"error", reviewErr,
	)

	// Increment retry count.
	newCount, err := state.IncrementRetryCount(r.db, rec.ID)
	if err != nil {
		loglib.Error("runner: failed to increment retry count",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}

	// Append system event to delivery stream.
	_, _ = r.stream.AppendSystem(ctx, repo, branch, rec.HeadHash,
		"review_failed",
		fmt.Sprintf("retry_count=%d error=%v", newCount, reviewErr),
	)

	// Determine next state.
	if newCount >= rec.MaxRetries {
		// T16: cli_reviewing → blocked.
		if err := state.TransitionBranch(r.db, rec.ID, state.StateCLIReviewing, state.StateBlocked, "max_retries_exceeded"); err != nil {
			return fmt.Errorf("transition to blocked: %w", err)
		}
		loglib.Warn("runner: branch blocked (max retries)",
			loglib.FieldEventType, loglib.EventTransition,
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			loglib.FieldFromState, string(state.StateCLIReviewing),
			loglib.FieldToState, string(state.StateBlocked),
			"retry_count", newCount,
		)
	} else {
		// T07: cli_reviewing → queued_cli (re-queue for retry).
		if err := state.TransitionBranch(r.db, rec.ID, state.StateCLIReviewing, state.StateQueuedCLI, "review_failed_requeue"); err != nil {
			return fmt.Errorf("transition to queued_cli: %w", err)
		}
		// Re-enqueue with same priority for retry.
		if err := r.queue.Enqueue(ctx, scheduler.QueueEntry{
			Repo:     repo,
			Branch:   branch,
			Priority: scheduler.QueuePriority(rec.QueuePriority),
		}); err != nil {
			loglib.Warn("runner: re-enqueue failed",
				loglib.FieldComponent, "runner",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		}
		loglib.Info("runner: branch re-queued after failure",
			loglib.FieldEventType, loglib.EventTransition,
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			loglib.FieldFromState, string(state.StateCLIReviewing),
			loglib.FieldToState, string(state.StateQueuedCLI),
			"retry_count", newCount,
		)
	}

	return nil
}

// newHolderID generates a unique holder ID for this runner instance + attempt.
func newHolderID() string {
	return "runner-" + uuid.New().String()
}

// resolveAgentID returns the agent identifier for the current process.
// It reads CODERO_AGENT_ID; falls back to $USER; then to the hostname.
func resolveAgentID() string {
	if v := os.Getenv("CODERO_AGENT_ID"); v != "" {
		return v
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	if host, err := os.Hostname(); err == nil {
		return host
	}
	return ""
}

func resolveSessionID() string {
	if v := os.Getenv("CODERO_SESSION_ID"); v != "" {
		return v
	}
	if v := os.Getenv("CODERO_AGENT_SESSION_ID"); v != "" {
		return v
	}
	return ""
}

func resolveSessionMode() string {
	if v := os.Getenv("CODERO_SESSION_MODE"); v != "" {
		return v
	}
	return "runner"
}

func resolveWorktree() string {
	if v := os.Getenv("CODERO_WORKTREE"); v != "" {
		return v
	}
	return ""
}

func resolveTaskID() string {
	if v := os.Getenv("CODERO_TASK_ID"); v != "" {
		return v
	}
	return ""
}

func (r *ReviewRunner) registerSession(ctx context.Context) {
	if r.sessionStore == nil || r.sessionID == "" {
		return
	}
	if err := r.sessionStore.Register(ctx, r.sessionID, r.sessionAgentID, r.sessionMode); err != nil {
		loglib.Warn("runner: session register failed",
			loglib.FieldComponent, "runner",
			"session_id", r.sessionID,
			"error", err,
		)
	}
}

func (r *ReviewRunner) emitSessionHeartbeat(ctx context.Context) {
	if r.sessionStore == nil || r.sessionID == "" {
		return
	}
	if err := r.sessionStore.Heartbeat(ctx, r.sessionID, false); err != nil {
		loglib.Warn("runner: session heartbeat failed",
			loglib.FieldComponent, "runner",
			"session_id", r.sessionID,
			"error", err,
		)
	}
}

func (r *ReviewRunner) attachSessionAssignment(ctx context.Context, repo, branch string) {
	if r.sessionStore == nil || r.sessionID == "" {
		return
	}
	if err := r.sessionStore.AttachAssignment(
		ctx,
		r.sessionID,
		r.sessionAgentID,
		repo,
		branch,
		r.sessionWorktree,
		r.sessionMode,
		r.sessionTaskID,
		"",
	); err != nil {
		loglib.Warn("runner: session attach failed",
			loglib.FieldComponent, "runner",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"session_id", r.sessionID,
			"error", err,
		)
	}
}
