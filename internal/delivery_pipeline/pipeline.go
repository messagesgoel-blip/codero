package deliverypipeline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/looplab/fsm"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/event"
	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/gitops"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

const (
	defaultMonitorPollInterval = 30 * time.Second
	defaultMonitorTimeout      = 30 * time.Minute
)

const (
	stateIdle             = "idle"
	stateStaging          = "staging"
	stateGating           = "gating"
	stateCommitting       = "committing"
	statePushing          = "pushing"
	statePRManagement     = "pr_management"
	stateMonitoring       = "monitoring"
	stateFeedbackDelivery = "feedback_delivery"
	stateMergeEvaluation  = "merge_evaluation"
	stateMerging          = "merging"
	statePostMerge        = "post_merge"
	stateFailed           = "failed"
)

const defaultLockTimeout = 10 * time.Minute

var ErrPipelineBusy = errors.New("delivery pipeline already running")

// GitOps defines the git operations used by the delivery pipeline.
type GitOps interface {
	Stage(worktree string) error
	Commit(worktree string, opts gitops.CommitOpts) (string, error)
	Push(worktree, remote, branch string) error
}

// GateRunner executes the review gate pipeline.
type GateRunner interface {
	RunPipeline(ctx context.Context, worktree string, stagedFiles []string) (*gatecheck.Report, error)
}

// PRInfo holds the essential fields from a GitHub PR lookup.
type PRInfo struct {
	Number  int
	HeadSHA string
}

// CheckRunInfo holds one CI check run result.
type CheckRunInfo struct {
	Name       string
	Status     string // "completed", "in_progress", "queued"
	Conclusion string // "success", "failure", "neutral", etc.
}

// ReviewInfo holds one PR review result.
type ReviewInfo struct {
	State string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED"
	User  string
	IsBot bool
}

// GitHubClient defines the PR operations needed by the pipeline.
type GitHubClient interface {
	CreatePRIfEnabled(ctx context.Context, repo, head, base, title, body string) (int, bool, error)
	TriggerCodeRabbitReview(ctx context.Context, repo string, prNumber int) error
	FindOpenPR(ctx context.Context, repo, branch string) (*PRInfo, error)
	ListCheckRuns(ctx context.Context, repo, sha string) ([]CheckRunInfo, error)
	ListPRReviews(ctx context.Context, repo string, prNumber int) ([]ReviewInfo, error)
	MergePR(ctx context.Context, repo string, prNumber int, sha, mergeMethod string) error
}

// Writer writes TASK/FEEDBACK artifacts to the worktree.
type Writer interface {
	WriteTASK(worktree string, task Task) error
	WriteFEEDBACK(worktree string, feedback FeedbackPackage) error
	ClearFEEDBACK(worktree string) error
}

// Notifier dispatches delivery notifications.
type Notifier interface {
	Notify(worktree, notificationType, assignmentID string)
}

// EventSender delivers structured event envelopes to OpenClaw endpoints.
// BND-004: Codero emits structured payloads only; OpenClaw owns PTY injection timing.
type EventSender interface {
	Send(ctx context.Context, env event.Envelope) error
}

// PipelineDeps is the dependency set for the delivery pipeline.
type PipelineDeps struct {
	StateDB     *state.DB
	GitOps      GitOps
	GateRunner  GateRunner
	GitHub      GitHubClient
	Writer      Writer
	Notifier    Notifier
	EventSender EventSender
	StateHook   func(state string)
}

// Pipeline orchestrates the submit-to-merge sequence.
type Pipeline struct {
	deps     PipelineDeps
	mu       sync.Mutex
	inFlight map[string]struct{}
}

// NewPipeline constructs a delivery pipeline with default dependencies where nil.
func NewPipeline(deps PipelineDeps) *Pipeline {
	if deps.GitOps == nil {
		deps.GitOps = defaultGitOps{}
	}
	if deps.GateRunner == nil {
		deps.GateRunner = defaultGateRunner{}
	}
	if deps.Writer == nil {
		deps.Writer = defaultWriter{}
	}
	if deps.Notifier == nil {
		deps.Notifier = defaultNotifier{}
	}
	if deps.EventSender == nil {
		deps.EventSender = &defaultEventSender{
			replyTo: event.NewReplyToDirectClient(),
		}
	}
	return &Pipeline{
		deps:     deps,
		inFlight: make(map[string]struct{}),
	}
}

type defaultEventSender struct {
	replyTo event.ReplyToClient
}

func (s *defaultEventSender) Send(ctx context.Context, env event.Envelope) error {
	return s.replyTo.Deliver(ctx, env)
}

// Submit runs the full delivery pipeline for an assignment/worktree.
func (p *Pipeline) Submit(ctx context.Context, assignmentID, worktree string) error {
	if ctx == nil {
		return fmt.Errorf("pipeline: context required")
	}
	if strings.TrimSpace(assignmentID) == "" {
		return fmt.Errorf("pipeline: assignment id required")
	}
	if p.deps.StateDB == nil {
		return fmt.Errorf("pipeline: state DB required")
	}

	assignment, err := state.GetAgentAssignmentByID(ctx, p.deps.StateDB, assignmentID)
	if err != nil {
		return fmt.Errorf("pipeline: load assignment: %w", err)
	}
	if worktree == "" {
		worktree = assignment.Worktree
	}
	if strings.TrimSpace(worktree) == "" {
		return fmt.Errorf("pipeline: worktree required")
	}

	if !p.acquire(worktree) {
		return ErrPipelineBusy
	}
	defer p.release(worktree)

	if IsLocked(worktree) {
		return ErrPipelineBusy
	}
	if err := Lock(worktree, assignment.SessionID, assignmentID); err != nil {
		if strings.Contains(err.Error(), "already") {
			return ErrPipelineBusy
		}
		return fmt.Errorf("pipeline: lock: %w", err)
	}
	defer func() {
		if err := Unlock(worktree); err != nil {
			loglib.Warn("delivery pipeline: unlock failed",
				loglib.FieldComponent, "delivery_pipeline",
				"error", err.Error(),
			)
		}
	}()
	p.deps.Notifier.Notify(worktree, "task", assignmentID)

	fsmState := newPipelineFSM()

	if err := fsmState.Event(ctx, "submit"); err != nil {
		return fmt.Errorf("pipeline: submit transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, stateStaging); err != nil {
		return err
	}
	version, err := p.bumpRevision(ctx, assignmentID)
	if err != nil {
		return err
	}

	if err := p.deps.GitOps.Stage(worktree); err != nil {
		return p.fail(ctx, assignmentID, worktree, fsmState, fmt.Errorf("stage: %w", err), nil)
	}

	if err := fsmState.Event(ctx, "gate"); err != nil {
		return fmt.Errorf("pipeline: gate transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, stateGating); err != nil {
		return err
	}
	report, err := p.deps.GateRunner.RunPipeline(ctx, worktree, nil)
	if err != nil {
		return p.fail(ctx, assignmentID, worktree, fsmState, fmt.Errorf("gate: %w", err), nil)
	}
	gateResult := string(report.Result)
	if err := p.setGateResult(ctx, assignmentID, gateResult); err != nil {
		return err
	}
	if report.Result == gatecheck.StatusFail {
		return p.fail(ctx, assignmentID, worktree, fsmState, errors.New("gate failed"), report)
	}

	if err := fsmState.Event(ctx, "commit"); err != nil {
		return fmt.Errorf("pipeline: commit transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, stateCommitting); err != nil {
		return err
	}
	commitSHA, err := p.deps.GitOps.Commit(worktree, commitOpts(assignment, version))
	if err != nil {
		return p.fail(ctx, assignmentID, worktree, fsmState, fmt.Errorf("commit: %w", err), nil)
	}
	if err := p.setCommitSHA(ctx, assignmentID, commitSHA); err != nil {
		return err
	}

	if err := fsmState.Event(ctx, "push"); err != nil {
		return fmt.Errorf("pipeline: push transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, statePushing); err != nil {
		return err
	}
	if err := p.deps.GitOps.Push(worktree, "origin", assignment.Branch); err != nil {
		return p.fail(ctx, assignmentID, worktree, fsmState, fmt.Errorf("push: %w", err), nil)
	}
	if err := p.setPushAt(ctx, assignmentID, time.Now().UTC()); err != nil {
		return err
	}

	if err := fsmState.Event(ctx, "pr"); err != nil {
		return fmt.Errorf("pipeline: pr transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, statePRManagement); err != nil {
		return err
	}

	if p.deps.GitHub != nil {
		title := commitMessage(assignment.TaskID, version, "update")
		body := fmt.Sprintf("Automated delivery for %s", assignment.TaskID)
		base := defaultBaseBranch()
		prNumber, created, err := p.deps.GitHub.CreatePRIfEnabled(ctx, assignment.Repo, assignment.Branch, base, title, body)
		if err != nil && !prExists(err) {
			return p.fail(ctx, assignmentID, worktree, fsmState, fmt.Errorf("create PR: %w", err), nil)
		}
		if created {
			if err := p.deps.GitHub.TriggerCodeRabbitReview(ctx, assignment.Repo, prNumber); err != nil {
				loglib.Warn("delivery pipeline: CodeRabbit trigger failed",
					loglib.FieldComponent, "delivery_pipeline",
					"error", err.Error(),
				)
			}
		}
	}

	// --- Monitoring phase: poll CI and reviews until resolved ---
	if err := fsmState.Event(ctx, "monitor"); err != nil {
		return fmt.Errorf("pipeline: monitor transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, stateMonitoring); err != nil {
		return err
	}

	var monitorResult monitorOutcome
	if p.deps.GitHub != nil {
		var err error
		monitorResult, err = p.monitor(ctx, assignment)
		if err != nil {
			return p.fail(ctx, assignmentID, worktree, fsmState, fmt.Errorf("monitor: %w", err), nil)
		}
	}

	// --- Feedback delivery phase: write aggregated feedback to worktree ---
	if err := fsmState.Event(ctx, "feedback"); err != nil {
		return fmt.Errorf("pipeline: feedback transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, stateFeedbackDelivery); err != nil {
		return err
	}

	if monitorResult.hasActionableFeedback() {
		fb, err := BuildFeedback(monitorResult.feedbackSources())
		if err != nil {
			loglib.Warn("delivery pipeline: build feedback failed",
				loglib.FieldComponent, "delivery_pipeline",
				"error", err.Error(),
				"assignment", assignmentID,
			)
		} else if fb != nil {
			if writeErr := p.deps.Writer.WriteFEEDBACK(worktree, *fb); writeErr != nil {
				loglib.Warn("delivery pipeline: write feedback failed",
					loglib.FieldComponent, "delivery_pipeline",
					"error", writeErr.Error(),
				)
			}
			sendErr := p.sendFeedbackEvent(ctx, assignment, fb)
			p.deps.Notifier.Notify(worktree, "feedback", assignmentID)
			if sendErr != nil {
				_ = fsmState.Event(ctx, "fail")
				_ = p.setDeliveryState(ctx, assignmentID, stateFailed)
				_ = fsmState.Event(ctx, "recover")
				_ = p.setDeliveryState(ctx, assignmentID, stateIdle)
				return fmt.Errorf("pipeline: feedback delivery: %w", sendErr)
			}
		}
	}

	// --- Merge evaluation phase: check all merge predicates ---
	if err := fsmState.Event(ctx, "evaluate"); err != nil {
		return fmt.Errorf("pipeline: evaluate transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, stateMergeEvaluation); err != nil {
		return err
	}

	mergeReady := monitorResult.ciPassed && monitorResult.approved && !monitorResult.changesRequested
	if p.deps.StateDB != nil {
		branchStateID, err := p.lookupBranchStateID(ctx, assignment.Repo, assignment.Branch)
		if err == nil && branchStateID != "" {
			result, err := state.EvaluateMergeReadiness(ctx, p.deps.StateDB, branchStateID)
			if err == nil {
				mergeReady = mergeReady && result.Ready
			}
		}
	}

	// --- Merging phase: execute merge if ready ---
	if err := fsmState.Event(ctx, "merge"); err != nil {
		return fmt.Errorf("pipeline: merge transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, stateMerging); err != nil {
		return err
	}

	merged := false
	if mergeReady && p.deps.GitHub != nil && monitorResult.prNumber > 0 {
		mergeMethod := mergeMethodFromEnv()
		if err := p.deps.GitHub.MergePR(ctx, assignment.Repo, monitorResult.prNumber, monitorResult.headSHA, mergeMethod); err != nil {
			loglib.Warn("delivery pipeline: merge failed",
				loglib.FieldComponent, "delivery_pipeline",
				"error", err.Error(),
				"pr", monitorResult.prNumber,
			)
		} else {
			merged = true
		}
	}

	// --- Post-merge phase: archive session, dispatch next task ---
	if err := fsmState.Event(ctx, "post"); err != nil {
		return fmt.Errorf("pipeline: post transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, statePostMerge); err != nil {
		return err
	}

	p.archiveSession(ctx, assignment, merged)

	// Reset to idle
	if err := fsmState.Event(ctx, "reset"); err != nil {
		return fmt.Errorf("pipeline: reset transition: %w", err)
	}
	if err := p.setDeliveryState(ctx, assignmentID, stateIdle); err != nil {
		return err
	}
	return nil
}

// monitorOutcome captures the result of the monitoring phase.
type monitorOutcome struct {
	prNumber         int
	headSHA          string
	ciPassed         bool
	ciPending        bool
	approved         bool
	changesRequested bool
	reviewFindings   []FeedbackItem
}

func (m monitorOutcome) hasActionableFeedback() bool {
	return m.changesRequested || len(m.reviewFindings) > 0 || (!m.ciPassed && !m.ciPending)
}

func (m monitorOutcome) feedbackSources() []FeedbackSource {
	var sources []FeedbackSource
	if len(m.reviewFindings) > 0 {
		sources = append(sources, FeedbackSource{
			Type:     FeedbackSourceCoderabbit,
			Findings: m.reviewFindings,
		})
	}
	if m.changesRequested && len(m.reviewFindings) == 0 {
		sources = append(sources, FeedbackSource{
			Type:     FeedbackSourceCoderabbit,
			Findings: []FeedbackItem{{Message: "Changes requested by reviewer"}},
		})
	}
	if !m.ciPassed && !m.ciPending {
		sources = append(sources, FeedbackSource{
			Type:     FeedbackSourceCI,
			Findings: []FeedbackItem{{Message: "CI checks failed"}},
		})
	}
	return sources
}

// monitor polls GitHub for CI status and review state until resolved or timeout.
func (p *Pipeline) monitor(ctx context.Context, assignment *state.AgentAssignment) (monitorOutcome, error) {
	pollInterval := monitorPollInterval()
	timeout := monitorTimeout()
	deadline := time.Now().Add(timeout)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if time.Now().After(deadline) {
			return monitorOutcome{}, fmt.Errorf("monitoring timed out after %s", timeout)
		}

		prInfo, err := p.deps.GitHub.FindOpenPR(ctx, assignment.Repo, assignment.Branch)
		if err != nil {
			return monitorOutcome{}, fmt.Errorf("find PR: %w", err)
		}
		if prInfo == nil || prInfo.Number == 0 {
			return monitorOutcome{}, nil // No PR exists
		}

		runs, err := p.deps.GitHub.ListCheckRuns(ctx, assignment.Repo, prInfo.HeadSHA)
		if err != nil {
			return monitorOutcome{}, fmt.Errorf("list check runs: %w", err)
		}
		ciPassed, ciPending := interpretCheckRuns(runs)

		reviews, err := p.deps.GitHub.ListPRReviews(ctx, assignment.Repo, prInfo.Number)
		if err != nil {
			loglib.Warn("delivery pipeline: list reviews failed",
				loglib.FieldComponent, "delivery_pipeline",
				"error", err.Error(),
			)
		}
		approved, changesRequested := interpretReviews(reviews)

		// Proceed once CI is resolved AND review state is decided (approved or changes_requested).
		// If CI is still pending, always keep polling.
		reviewResolved := approved || changesRequested
		if !ciPending && reviewResolved {
			return monitorOutcome{
				prNumber:         prInfo.Number,
				headSHA:          prInfo.HeadSHA,
				ciPassed:         ciPassed,
				approved:         approved,
				changesRequested: changesRequested,
			}, nil
		}
		// Also exit if CI failed — no point waiting for review.
		if !ciPending && !ciPassed {
			return monitorOutcome{
				prNumber:         prInfo.Number,
				headSHA:          prInfo.HeadSHA,
				ciPassed:         false,
				approved:         approved,
				changesRequested: changesRequested,
			}, nil
		}

		select {
		case <-ctx.Done():
			return monitorOutcome{}, ctx.Err()
		case <-ticker.C:
			continue
		}
	}
}

// interpretCheckRuns returns (allPassed, anyPending) from a list of check runs.
func interpretCheckRuns(runs []CheckRunInfo) (bool, bool) {
	if len(runs) == 0 {
		return false, true // No checks yet = pending
	}
	allPassed := true
	anyPending := false
	for _, run := range runs {
		if run.Status != "completed" {
			anyPending = true
			allPassed = false
		} else if run.Conclusion != "success" && run.Conclusion != "neutral" && run.Conclusion != "skipped" {
			allPassed = false
		}
	}
	return allPassed, anyPending
}

// interpretReviews returns (approved, changesRequested) from a list of reviews.
// Reviews are processed in order (oldest first, as returned by the GitHub API),
// so later reviews from the same user override earlier ones.
func interpretReviews(reviews []ReviewInfo) (bool, bool) {
	// Track each reviewer's latest actionable state.
	latest := make(map[string]string) // user → "APPROVED" | "CHANGES_REQUESTED"
	for _, r := range reviews {
		if r.IsBot {
			continue
		}
		state := strings.ToUpper(r.State)
		if state == "APPROVED" || state == "CHANGES_REQUESTED" {
			latest[r.User] = state
		}
	}
	approved := false
	changesRequested := false
	for _, state := range latest {
		switch state {
		case "APPROVED":
			approved = true
		case "CHANGES_REQUESTED":
			changesRequested = true
		}
	}
	return approved, changesRequested
}

func (p *Pipeline) lookupBranchStateID(ctx context.Context, repo, branch string) (string, error) {
	if p.deps.StateDB == nil {
		return "", fmt.Errorf("no state DB")
	}
	var id string
	err := p.deps.StateDB.Unwrap().QueryRowContext(ctx,
		`SELECT id FROM branch_states WHERE repo = ? AND branch = ? LIMIT 1`,
		repo, branch,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (p *Pipeline) archiveSession(ctx context.Context, assignment *state.AgentAssignment, merged bool) {
	if p.deps.StateDB == nil || assignment == nil {
		return
	}
	result := "abandoned"
	if merged {
		result = "merged"
	}
	now := time.Now().UTC()
	archiveID := fmt.Sprintf("%s-archive-%d", assignment.ID, now.UnixNano())
	_, err := p.deps.StateDB.Unwrap().ExecContext(ctx,
		`INSERT OR IGNORE INTO session_archives
			(archive_id, session_id, agent_id, task_id, repo, branch, result, started_at, ended_at, archived_at)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		archiveID, assignment.SessionID, assignment.AgentID,
		assignment.TaskID, assignment.Repo, assignment.Branch,
		result, assignment.StartedAt, now, now,
	)
	if err != nil {
		loglib.Warn("delivery pipeline: archive session failed",
			loglib.FieldComponent, "delivery_pipeline",
			"error", err.Error(),
		)
	}
}

func monitorPollInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CODERO_MONITOR_POLL_INTERVAL"))
	if raw == "" {
		return defaultMonitorPollInterval
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return defaultMonitorPollInterval
}

func monitorTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CODERO_MONITOR_TIMEOUT"))
	if raw == "" {
		return defaultMonitorTimeout
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return defaultMonitorTimeout
}

func mergeMethodFromEnv() string {
	if m := strings.TrimSpace(os.Getenv("CODERO_MERGE_METHOD")); m != "" {
		return m
	}
	return "squash"
}

// ClearStaleLocks removes delivery.lock files older than the configured timeout.
func (p *Pipeline) ClearStaleLocks(ctx context.Context) error {
	if p.deps.StateDB == nil {
		return nil
	}
	timeout := deliveryLockTimeout()
	worktrees, err := listWorktrees(ctx, p.deps.StateDB.Unwrap())
	if err != nil {
		return err
	}
	for _, worktree := range worktrees {
		cleared, err := CheckTimeout(worktree, timeout)
		if err != nil {
			loglib.Warn("delivery pipeline: stale lock check failed",
				loglib.FieldComponent, "delivery_pipeline",
				"error", err.Error(),
				"worktree", worktree,
			)
			continue
		}
		if cleared {
			loglib.Info("delivery pipeline: cleared stale delivery lock",
				loglib.FieldComponent, "delivery_pipeline",
				"worktree", worktree,
			)
		}
	}
	return nil
}

func (p *Pipeline) fail(ctx context.Context, assignmentID, worktree string, fsmState *fsm.FSM, err error, report *gatecheck.Report) error {
	if fsmState != nil {
		_ = fsmState.Event(ctx, "fail")
	}
	_ = p.setDeliveryState(ctx, assignmentID, stateFailed)

	feedback := FeedbackPackage{
		GeneratedAt: time.Now().UTC(),
	}
	if report != nil {
		feedback.GateFindings = findingsFromGate(report)
	} else {
		feedback.CIFailures = []FeedbackItem{{Message: err.Error()}}
	}
	if writeErr := p.deps.Writer.WriteFEEDBACK(worktree, feedback); writeErr != nil {
		loglib.Warn("delivery pipeline: write feedback failed",
			loglib.FieldComponent, "delivery_pipeline",
			"error", writeErr.Error(),
		)
	}
	var sendErr error
	assignment, loadErr := state.GetAgentAssignmentByID(ctx, p.deps.StateDB, assignmentID)
	if loadErr != nil {
		sendErr = fmt.Errorf("load assignment for feedback delivery: %w", loadErr)
	} else {
		sendErr = p.sendFeedbackEvent(ctx, assignment, &feedback)
	}
	p.deps.Notifier.Notify(worktree, "feedback", assignmentID)

	if fsmState != nil {
		_ = fsmState.Event(ctx, "recover")
	}
	_ = p.setDeliveryState(ctx, assignmentID, stateIdle)
	if sendErr != nil {
		return fmt.Errorf("pipeline: feedback delivery after %v: %w", err, sendErr)
	}
	return nil
}

func (p *Pipeline) sendFeedbackEvent(ctx context.Context, assignment *state.AgentAssignment, fb *FeedbackPackage) error {
	if fb == nil || p.deps.EventSender == nil {
		return nil
	}
	if assignment == nil {
		return fmt.Errorf("feedback assignment is required")
	}

	// BND-004: emit structured event envelope for feedback delivery.
	// Codero emits structured payloads only; OpenClaw owns PTY injection timing.
	replyTo := p.buildReplyToEndpoint(ctx, assignment)
	findings := fbItemsToEventItems(fb.CIFailures)
	findings = append(findings, fbItemsToEventItems(fb.GateFindings)...)
	findings = append(findings, fbItemsToEventItems(fb.CodeReview)...)

	payload := event.FeedbackInjectPayload{
		AssignmentID: assignment.ID,
		SessionID:    assignment.SessionID,
		Findings:     findings,
		GateFindings: fbItemsToEventItems(fb.GateFindings),
		ReviewNotes:  fbItemsToEventItems(fb.CodeReview),
	}

	env, err := event.NewFeedbackInject(uuid.New().String(), replyTo, payload)
	if err != nil {
		loglib.Warn("delivery pipeline: build feedback envelope failed",
			loglib.FieldComponent, "delivery_pipeline",
			"error", err.Error(),
			"assignment", assignment.ID,
		)
		return err
	}

	if err := p.deps.EventSender.Send(ctx, env); err != nil {
		loglib.Warn("delivery pipeline: send feedback envelope failed",
			loglib.FieldComponent, "delivery_pipeline",
			"error", err.Error(),
			"assignment", assignment.ID,
		)
		return fmt.Errorf("send feedback envelope: %w", err)
	}
	return nil
}

func (p *Pipeline) acquire(worktree string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.inFlight[worktree]; ok {
		return false
	}
	p.inFlight[worktree] = struct{}{}
	return true
}

func (p *Pipeline) release(worktree string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.inFlight, worktree)
}

func (p *Pipeline) bumpRevision(ctx context.Context, assignmentID string) (int, error) {
	now := time.Now().UTC()
	if _, err := p.deps.StateDB.Unwrap().ExecContext(ctx, `
		UPDATE agent_assignments
		SET last_submit_at = ?, revision_count = revision_count + 1
		WHERE assignment_id = ?`,
		now, assignmentID,
	); err != nil {
		return 0, fmt.Errorf("pipeline: update submit: %w", err)
	}
	var version int
	if err := p.deps.StateDB.Unwrap().QueryRowContext(ctx,
		`SELECT revision_count FROM agent_assignments WHERE assignment_id = ?`,
		assignmentID,
	).Scan(&version); err != nil {
		return 0, fmt.Errorf("pipeline: read revision_count: %w", err)
	}
	return version, nil
}

func (p *Pipeline) setDeliveryState(ctx context.Context, assignmentID, state string) error {
	if _, err := p.deps.StateDB.Unwrap().ExecContext(ctx,
		`UPDATE agent_assignments SET delivery_state = ? WHERE assignment_id = ?`,
		state, assignmentID,
	); err != nil {
		return fmt.Errorf("pipeline: set delivery_state=%s: %w", state, err)
	}
	if p.deps.StateHook != nil {
		p.deps.StateHook(state)
	}
	return nil
}

func (p *Pipeline) setGateResult(ctx context.Context, assignmentID, result string) error {
	if _, err := p.deps.StateDB.Unwrap().ExecContext(ctx,
		`UPDATE agent_assignments SET last_gate_result = ? WHERE assignment_id = ?`,
		result, assignmentID,
	); err != nil {
		return fmt.Errorf("pipeline: set gate result: %w", err)
	}
	return nil
}

func (p *Pipeline) setCommitSHA(ctx context.Context, assignmentID, sha string) error {
	if _, err := p.deps.StateDB.Unwrap().ExecContext(ctx,
		`UPDATE agent_assignments SET last_commit_sha = ? WHERE assignment_id = ?`,
		sha, assignmentID,
	); err != nil {
		return fmt.Errorf("pipeline: set commit sha: %w", err)
	}
	return nil
}

func (p *Pipeline) setPushAt(ctx context.Context, assignmentID string, pushedAt time.Time) error {
	if _, err := p.deps.StateDB.Unwrap().ExecContext(ctx,
		`UPDATE agent_assignments SET last_push_at = ? WHERE assignment_id = ?`,
		pushedAt, assignmentID,
	); err != nil {
		return fmt.Errorf("pipeline: set push time: %w", err)
	}
	return nil
}

func findingsFromGate(report *gatecheck.Report) []FeedbackItem {
	var items []FeedbackItem
	for _, check := range report.Checks {
		for _, finding := range check.Findings {
			item := FeedbackItem{
				File:    finding.File,
				Line:    finding.Line,
				Message: finding.Message,
			}
			if item.Message == "" {
				item.Message = fmt.Sprintf("%s reported an issue", check.Name)
			}
			items = append(items, item)
		}
		if len(check.Findings) == 0 && check.Status == gatecheck.StatusFail {
			msg := check.Reason
			if msg == "" {
				msg = fmt.Sprintf("%s failed", check.Name)
			}
			items = append(items, FeedbackItem{Message: msg})
		}
	}
	if len(items) == 0 {
		items = append(items, FeedbackItem{Message: "Gate failed with no findings"})
	}
	return items
}

func commitOpts(a *state.AgentAssignment, version int) gitops.CommitOpts {
	authorName, authorEmail := commitAuthor(a)
	committerName, committerEmail := commitCommitter()
	return gitops.CommitOpts{
		Message:        commitMessage(a.TaskID, version, "update"),
		AuthorName:     authorName,
		AuthorEmail:    authorEmail,
		CommitterName:  committerName,
		CommitterEmail: committerEmail,
	}
}

func commitAuthor(a *state.AgentAssignment) (string, string) {
	name := strings.TrimSpace(a.AgentID)
	if name == "" {
		name = "agent"
	}
	email := strings.TrimSpace(a.AgentID)
	if email == "" {
		email = "agent"
	}
	email = fmt.Sprintf("%s@codero.local", email)
	return name, email
}

func commitCommitter() (string, string) {
	return "codero", "codero@local"
}

func commitMessage(taskID string, version int, summary string) string {
	format := os.Getenv("CODERO_COMMIT_MSG_FORMAT")
	if strings.TrimSpace(format) == "" {
		format = "[codero] {task_id} v{version}: {summary}"
	}
	if summary = strings.TrimSpace(summary); summary == "" {
		summary = "update"
	}
	msg := strings.ReplaceAll(format, "{task_id}", taskID)
	msg = strings.ReplaceAll(msg, "{version}", strconv.Itoa(version))
	msg = strings.ReplaceAll(msg, "{summary}", summary)
	return msg
}

func defaultBaseBranch() string {
	if v := strings.TrimSpace(os.Getenv("CODERO_BASE_BRANCH")); v != "" {
		return v
	}
	return "main"
}

func prExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "422")
}

func listWorktrees(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT worktree
		FROM agent_assignments
		WHERE worktree IS NOT NULL AND worktree != ''`)
	if err != nil {
		return nil, fmt.Errorf("pipeline: list worktrees: %w", err)
	}
	defer rows.Close()

	var worktrees []string
	for rows.Next() {
		var worktree string
		if err := rows.Scan(&worktree); err != nil {
			return nil, fmt.Errorf("pipeline: scan worktree: %w", err)
		}
		worktree = strings.TrimSpace(worktree)
		if worktree != "" {
			worktrees = append(worktrees, worktree)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pipeline: list worktrees: %w", err)
	}
	return worktrees, nil
}

func deliveryLockTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CODERO_DELIVERY_LOCK_TIMEOUT"))
	if raw == "" {
		return defaultLockTimeout
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d > 0 {
			return d
		}
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return defaultLockTimeout
}

func newPipelineFSM() *fsm.FSM {
	events := fsm.Events{
		{Name: "submit", Src: []string{stateIdle}, Dst: stateStaging},
		{Name: "gate", Src: []string{stateStaging}, Dst: stateGating},
		{Name: "commit", Src: []string{stateGating}, Dst: stateCommitting},
		{Name: "push", Src: []string{stateCommitting}, Dst: statePushing},
		{Name: "pr", Src: []string{statePushing}, Dst: statePRManagement},
		{Name: "monitor", Src: []string{statePRManagement}, Dst: stateMonitoring},
		{Name: "feedback", Src: []string{stateMonitoring}, Dst: stateFeedbackDelivery},
		{Name: "evaluate", Src: []string{stateFeedbackDelivery}, Dst: stateMergeEvaluation},
		{Name: "merge", Src: []string{stateMergeEvaluation}, Dst: stateMerging},
		{Name: "post", Src: []string{stateMerging}, Dst: statePostMerge},
		{Name: "reset", Src: []string{statePostMerge}, Dst: stateIdle},
		{Name: "fail", Src: []string{
			stateIdle, stateStaging, stateGating, stateCommitting, statePushing,
			statePRManagement, stateMonitoring, stateFeedbackDelivery, stateMergeEvaluation,
			stateMerging, statePostMerge,
		}, Dst: stateFailed},
		{Name: "recover", Src: []string{stateFailed}, Dst: stateIdle},
	}
	return fsm.NewFSM(stateIdle, events, fsm.Callbacks{})
}

type defaultGitOps struct{}

func (defaultGitOps) Stage(worktree string) error {
	return gitops.Stage(worktree)
}

func (defaultGitOps) Commit(worktree string, opts gitops.CommitOpts) (string, error) {
	return gitops.Commit(worktree, opts)
}

func (defaultGitOps) Push(worktree, remote, branch string) error {
	return gitops.Push(worktree, remote, branch)
}

type defaultGateRunner struct{}

func (defaultGateRunner) RunPipeline(ctx context.Context, worktree string, stagedFiles []string) (*gatecheck.Report, error) {
	cfg := gatecheck.LoadEngineConfig()
	cfg.Invocation = "codero"
	engine := gatecheck.NewEngine(cfg)
	return engine.RunPipeline(ctx, worktree, stagedFiles)
}

type defaultWriter struct{}

func (defaultWriter) WriteTASK(worktree string, task Task) error {
	return WriteTASK(worktree, task)
}

func (defaultWriter) WriteFEEDBACK(worktree string, feedback FeedbackPackage) error {
	return WriteFEEDBACK(worktree, feedback)
}

func (defaultWriter) ClearFEEDBACK(worktree string) error {
	return ClearFEEDBACK(worktree)
}

type defaultNotifier struct{}

func (defaultNotifier) Notify(worktree, notificationType, assignmentID string) {
	Notify(worktree, notificationType, assignmentID)
}

// buildReplyToEndpoint constructs the OpenClaw reply_to endpoint for an assignment.
// BND-004: reply_to is an OpenClaw endpoint, not a PTY path.
func (p *Pipeline) buildReplyToEndpoint(ctx context.Context, assignment *state.AgentAssignment) event.ReplyToEndpoint {
	ep := event.ReplyToEndpoint{
		Type:      "openclaw_session",
		SessionID: assignment.SessionID,
	}

	// Fetch additional routing fields (TmuxName, AgentKind) from the durable session.
	if p.deps.StateDB != nil {
		sess, err := state.GetAgentSession(ctx, p.deps.StateDB, assignment.SessionID)
		if err == nil && sess != nil {
			ep.TmuxName = sess.TmuxSessionName
			// agent_id remains the durable profile ID; derive the upstream CLI family from it.
			ep.AgentKind = config.InferAgentKind(sess.AgentID, "")
		}
	}

	return ep
}

// fbItemsToEventItems converts FeedbackItem to event.FeedbackItem.
func fbItemsToEventItems(items []FeedbackItem) []event.FeedbackItem {
	out := make([]event.FeedbackItem, 0, len(items))
	for _, item := range items {
		out = append(out, event.FeedbackItem{
			File:    item.File,
			Line:    item.Line,
			Message: item.Message,
		})
	}
	return out
}
