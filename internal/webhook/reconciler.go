package webhook

import (
	"context"
	"sync"
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
	PRNumber          int    // pull request number (0 if unknown)
	PROpen            bool
	Approved          bool
	CIGreen           bool
	PendingEvents     int
	UnresolvedThreads int
}

// AutoMerger can merge a pull request on GitHub once merge-ready conditions
// are confirmed. Implemented by internal/github.Client; can be stubbed in tests.
type AutoMerger interface {
	// MergePR merges the pull request identified by prNumber.
	// sha is the expected HEAD SHA; GitHub rejects the merge if it has changed.
	// mergeMethod must be "merge", "squash", or "rebase".
	MergePR(ctx context.Context, repo string, prNumber int, sha, mergeMethod string) error
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
	db           *state.DB
	github       GitHubClient
	repos        []string
	interval     time.Duration
	merger       AutoMerger // nil → auto-merge disabled
	mergeMethod  string     // "merge", "squash", or "rebase"
	healthMu     sync.RWMutex
	lastProbeAt  time.Time
	lastProbeErr string
	probed       bool
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

// WithAutoMerge enables automatic PR merging when a branch reaches merge_ready.
// merger is typically a *github.Client; method must be "merge", "squash", or
// "rebase" (defaults to "squash" if empty).
// Returns the same *Reconciler to allow method chaining.
func (r *Reconciler) WithAutoMerge(merger AutoMerger, method string) *Reconciler {
	if method == "" {
		method = "squash"
	}
	r.merger = merger
	r.mergeMethod = method
	return r
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

// GitHubProbeStatus returns the latest GitHub probe result observed by the reconciler.
func (r *Reconciler) GitHubProbeStatus() (checkedAt time.Time, healthy bool, errText string, ok bool) {
	r.healthMu.RLock()
	defer r.healthMu.RUnlock()

	if !r.probed {
		return time.Time{}, false, "", false
	}
	return r.lastProbeAt, r.lastProbeErr == "", r.lastProbeErr, true
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
		r.recordGitHubProbe(err)
		loglib.Error("reconciler: get PR state failed",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
		return
	}
	r.recordGitHubProbe(nil)

	if ghState == nil {
		// No PR exists. Skip pre-PR states where a PR is not yet expected.
		if b.State == state.StateCoding || b.State == state.StateLocalReview {
			return
		}
		r.maybeClose(b, "pr_not_found")
		return
	}

	// Persist the PR number as soon as GitHub reports it — before any early
	// returns so closed/stale branches still have their PR number recorded.
	if ghState.PRNumber > 0 {
		if err := state.UpdatePRNumber(ctx, r.db, b.Repo, b.Branch, ghState.PRNumber); err != nil {
			loglib.Warn("reconciler: update pr number failed",
				loglib.FieldComponent, "reconciler",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
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
	if err := state.ReconcileAgentAssignmentWaitingState(ctx, r.db, b.Repo, b.Branch); err != nil {
		loglib.Warn("reconciler: reconcile assignment waiting state failed",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
	}

	// Detect: merge_ready conditions revoked → T11.
	if b.State == state.StateMergeReady {
		if !ghState.Approved || !ghState.CIGreen ||
			ghState.PendingEvents > 0 || ghState.UnresolvedThreads > 0 {
			r.transitionIfValid(b, state.StateCoding, "merge_ready_conditions_revoked")
			return
		}
		// Conditions still met — attempt auto-merge (idempotent; no-op if disabled).
		r.maybeMerge(ctx, b, ghState)
		return
	}

	// Detect: reviewed branch now meets merge_ready conditions → T10.
	if b.State == state.StateReviewed {
		if ghState.Approved && ghState.CIGreen &&
			ghState.PendingEvents == 0 && ghState.UnresolvedThreads == 0 {
			r.transitionIfValid(b, state.StateMergeReady, "merge_ready_conditions_met")
			// Attempt auto-merge immediately after the merge_ready transition.
			r.maybeMerge(ctx, b, ghState)
			return
		}
	}
}

func (r *Reconciler) recordGitHubProbe(err error) {
	r.healthMu.Lock()
	defer r.healthMu.Unlock()

	r.probed = true
	r.lastProbeAt = time.Now().UTC()
	if err != nil {
		r.lastProbeErr = err.Error()
		return
	}
	r.lastProbeErr = ""
}

// maybeMerge calls the GitHub Merge API if auto-merge is configured and the
// PR number is known. It reloads the branch from the DB immediately before
// the external call to guard against stale snapshots: if another worker has
// revoked merge_ready or the head hash has changed since this cycle started,
// the merge is skipped rather than issued against outdated state.
// On success it transitions the branch to closed (T18). On failure it logs
// the error and leaves the branch in merge_ready for the next cycle.
func (r *Reconciler) maybeMerge(ctx context.Context, b state.BranchRecord, ghState *GitHubState) {
	if r.merger == nil || ghState.PRNumber == 0 {
		return
	}

	// Reload to get the current durable state — b may be a stale snapshot.
	current, err := state.GetBranch(r.db, b.Repo, b.Branch)
	if err != nil {
		loglib.Info("reconciler: auto-merge skipped (reload failed)",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
		return
	}
	if current.State != state.StateMergeReady {
		loglib.Info("reconciler: auto-merge skipped (state no longer merge_ready)",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			loglib.FieldFromState, string(current.State),
		)
		return
	}
	if current.HeadHash != ghState.HeadHash {
		loglib.Info("reconciler: auto-merge skipped (head hash changed)",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
		)
		return
	}

	if err := r.merger.MergePR(ctx, b.Repo, ghState.PRNumber, ghState.HeadHash, r.mergeMethod); err != nil {
		loglib.Error("reconciler: auto-merge failed",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"pr_number", ghState.PRNumber,
			"error", err,
		)
		return
	}
	loglib.Info("reconciler: auto-merge succeeded",
		loglib.FieldEventType, loglib.EventTransition,
		loglib.FieldComponent, "reconciler",
		loglib.FieldRepo, b.Repo,
		loglib.FieldBranch, b.Branch,
		"pr_number", ghState.PRNumber,
		"merge_method", r.mergeMethod,
	)
	// PR is merged on GitHub; mirror the terminal state locally (T18).
	r.transitionIfValid(*current, state.StateClosed, "auto_merged")
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
