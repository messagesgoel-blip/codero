package webhook

import (
	"context"
	"time"

	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

const (
	// PollingOnlyInterval is the reconciliation period when webhooks are disabled.
	// Per roadmap C.5: "reconciliation loop runs every 60 seconds in polling-only mode".
	PollingOnlyInterval = 60 * time.Second

	// WebhookModeInterval is the reconciliation period when webhooks are enabled.
	// Per roadmap C.5: "still runs every 5 minutes as catch-up and correctness backstop".
	WebhookModeInterval = 5 * time.Minute
)

// GitHubState represents the current GitHub-side state of a PR.
type GitHubState struct {
	Repo              string
	Branch            string
	HeadHash          string // current HEAD on GitHub
	PROpen            bool
	Approved          bool
	CIGreen           bool
	PendingEvents     int
	UnresolvedThreads int
}

// GitHubClient is the interface for querying GitHub PR state.
// In polling-only mode this is the only ingestion mechanism.
type GitHubClient interface {
	// GetPRState fetches the current GitHub state for a repo+branch.
	// Returns (nil, nil) if no PR exists for the branch.
	GetPRState(ctx context.Context, repo, branch string) (*GitHubState, error)
}

// StubGitHubClient is a no-op GitHub client used when no real GitHub
// integration is configured (tests, offline development).
type StubGitHubClient struct{}

func (s *StubGitHubClient) GetPRState(_ context.Context, repo, branch string) (*GitHubState, error) {
	return &GitHubState{
		Repo:              repo,
		Branch:            branch,
		PROpen:            true,
		Approved:          false,
		CIGreen:           false,
		PendingEvents:     0,
		UnresolvedThreads: 0,
	}, nil
}

// Reconciler periodically polls GitHub state and repairs drift against the
// durable branch records. It is the correctness backstop in all modes and the
// only ingestion mechanism in polling-only mode.
type Reconciler struct {
	db       *state.DB
	github   GitHubClient
	repos    []string
	interval time.Duration
}

// NewReconciler creates a Reconciler.
// If webhookEnabled is false, uses PollingOnlyInterval (60s).
// If webhookEnabled is true, uses WebhookModeInterval (5m).
func NewReconciler(db *state.DB, github GitHubClient, repos []string, webhookEnabled bool) *Reconciler {
	interval := PollingOnlyInterval
	if webhookEnabled {
		interval = WebhookModeInterval
	}
	return &Reconciler{
		db:       db,
		github:   github,
		repos:    repos,
		interval: interval,
	}
}

// RunOnce executes a single reconciliation cycle and returns.
// It is exported for use by the `codero poll` command and tests.
func (r *Reconciler) RunOnce(ctx context.Context) {
	r.runCycle(ctx)
}

// Run starts the reconciliation loop. It blocks until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) {
	loglib.Info("reconciler: starting",
		loglib.FieldEventType, loglib.EventStartup,
		loglib.FieldComponent, "reconciler",
		"interval", r.interval,
	)

	// Run once immediately on start to catch drift from any previous downtime.
	r.runCycle(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			loglib.Info("reconciler: stopped",
				loglib.FieldEventType, loglib.EventShutdown,
				loglib.FieldComponent, "reconciler",
			)
			return
		case <-ticker.C:
			r.runCycle(ctx)
		}
	}
}

// runCycle iterates over all active branches and reconciles each against GitHub.
func (r *Reconciler) runCycle(ctx context.Context) {
	branches, err := state.ListActiveBranches(r.db)
	if err != nil {
		loglib.Error("reconciler: list active branches failed",
			loglib.FieldComponent, "reconciler",
			"error", err,
		)
		return
	}

	for _, b := range branches {
		select {
		case <-ctx.Done():
			return
		default:
		}
		r.reconcileBranch(ctx, b)
	}
}

// reconcileBranch fetches GitHub state for one branch and applies corrections.
func (r *Reconciler) reconcileBranch(ctx context.Context, b state.BranchRecord) {
	ghState, err := r.github.GetPRState(ctx, b.Repo, b.Branch)
	if err != nil {
		loglib.Error("reconciler: get PR state failed",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
		return
	}

	if ghState == nil {
		// No PR exists. Skip pre-PR states where a PR is not yet expected.
		if b.State == state.StateCoding || b.State == state.StateLocalReview {
			return
		}
		r.maybeClose(b, "pr_not_found")
		return
	}

	// Detect: PR closed → T18.
	if !ghState.PROpen {
		r.maybeClose(b, "pr_closed")
		return
	}

	// Detect: stale HEAD / force-push → T12.
	if ghState.HeadHash != "" && b.HeadHash != "" && ghState.HeadHash != b.HeadHash {
		r.maybeStaleBranch(b, ghState.HeadHash)
		return
	}

	// Update merge-readiness fields.
	if err := state.UpdateMergeReadiness(r.db, b.ID,
		ghState.Approved, ghState.CIGreen,
		ghState.PendingEvents, ghState.UnresolvedThreads,
	); err != nil {
		loglib.Error("reconciler: update merge readiness failed",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
		return
	}

	// Detect: merge_ready conditions revoked → T11.
	if b.State == state.StateMergeReady {
		if !ghState.Approved || !ghState.CIGreen ||
			ghState.PendingEvents > 0 || ghState.UnresolvedThreads > 0 {
			r.transitionIfValid(b, state.StateCoding, "merge_ready_conditions_revoked")
			return
		}
	}

	// Detect: reviewed branch now meets merge_ready conditions → T10.
	if b.State == state.StateReviewed {
		if ghState.Approved && ghState.CIGreen &&
			ghState.PendingEvents == 0 && ghState.UnresolvedThreads == 0 {
			r.transitionIfValid(b, state.StateMergeReady, "merge_ready_conditions_met")
			return
		}
	}
}

func (r *Reconciler) maybeClose(b state.BranchRecord, trigger string) {
	// T18: any → closed (terminal).
	if b.State == state.StateClosed {
		return // already terminal
	}
	r.transitionIfValid(b, state.StateClosed, trigger)
}

func (r *Reconciler) maybeStaleBranch(b state.BranchRecord, newHeadHash string) {
	// T12: any active → stale_branch. Update head_hash and transition atomically
	// to prevent a race where the hash update succeeds but the transition fails.
	if err := state.UpdateHeadHashAndTransition(r.db, b.ID, newHeadHash, b.State, state.StateStaleBranch, "head_hash_mismatch"); err != nil {
		loglib.Info("reconciler: stale branch transition skipped",
			loglib.FieldEventType, loglib.EventRejection,
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			loglib.FieldFromState, string(b.State),
			loglib.FieldToState, string(state.StateStaleBranch),
			"trigger", "head_hash_mismatch",
			"error", err,
		)
		return
	}
	loglib.Info("reconciler: transition applied",
		loglib.FieldEventType, loglib.EventTransition,
		loglib.FieldComponent, "reconciler",
		loglib.FieldRepo, b.Repo,
		loglib.FieldBranch, b.Branch,
		loglib.FieldFromState, string(b.State),
		loglib.FieldToState, string(state.StateStaleBranch),
		"trigger", "head_hash_mismatch",
	)
}

func (r *Reconciler) transitionIfValid(b state.BranchRecord, to state.State, trigger string) {
	if err := state.TransitionBranch(r.db, b.ID, b.State, to, trigger); err != nil {
		// Log invalid transitions as rejections—they are expected when concurrent
		// processes race (e.g., runner transitions while reconciler checks).
		loglib.Info("reconciler: transition skipped",
			loglib.FieldEventType, loglib.EventRejection,
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			loglib.FieldFromState, string(b.State),
			loglib.FieldToState, string(to),
			"trigger", trigger,
			"error", err,
		)
		return
	}

	loglib.Info("reconciler: transition applied",
		loglib.FieldEventType, loglib.EventTransition,
		loglib.FieldComponent, "reconciler",
		loglib.FieldRepo, b.Repo,
		loglib.FieldBranch, b.Branch,
		loglib.FieldFromState, string(b.State),
		loglib.FieldToState, string(to),
		"trigger", trigger,
	)
}

// NopProcessor is a Processor that discards all events (used when no
// stateful event handling is needed, e.g., in polling-only mode).
type NopProcessor struct{}

func (n *NopProcessor) ProcessEvent(_ context.Context, ev GitHubEvent) error {
	loglib.Info("webhook: event received (nop processor)",
		loglib.FieldComponent, "webhook",
		"delivery_id", ev.DeliveryID,
		"event_type", ev.EventType,
		loglib.FieldRepo, ev.Repo,
	)
	return nil
}
