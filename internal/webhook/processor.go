package webhook

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/codero/codero/internal/delivery"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/state"
)

// EventProcessor is a stateful Processor that handles GitHub webhook events
// and applies corrections to the durable branch state. It replaces NopProcessor
// when the daemon is running in webhook mode.
//
// Handled event types:
//   - pull_request:        opened, closed, synchronize
//   - pull_request_review: submitted (approved / changes_requested)
//   - check_run:          completed
//
// Unknown event types are silently dropped — the reconciler provides the
// correctness backstop so EventProcessor only needs to handle the fast path.
//
// When a GitHubClient is injected via WithGitHubClient, the review and
// check_run handlers re-fetch the full aggregated PR state from GitHub
// instead of treating the single webhook event as authoritative. This
// prevents multi-reviewer overwrites and multi-check false positives.
type EventProcessor struct {
	db       *state.DB
	stream   *delivery.Stream
	github   GitHubClient    // optional; enables aggregated state on review/check events
	openclaw *OpenClawClient // optional; enables PTY delivery on review events
}

// NewEventProcessor creates an EventProcessor with the given dependencies.
func NewEventProcessor(db *state.DB, stream *delivery.Stream) *EventProcessor {
	return &EventProcessor{db: db, stream: stream}
}

// WithGitHubClient attaches a GitHub client used to fetch aggregated PR state
// on pull_request_review and check_run events, ensuring multi-reviewer and
// multi-check scenarios are handled correctly.
func (p *EventProcessor) WithGitHubClient(gh GitHubClient) *EventProcessor {
	p.github = gh
	return p
}

// WithOpenClawClient attaches an OpenClaw adapter client that delivers
// findings to agent PTYs when a pull_request_review webhook arrives.
func (p *EventProcessor) WithOpenClawClient(oc *OpenClawClient) *EventProcessor {
	p.openclaw = oc
	return p
}

// ProcessEvent implements Processor.
func (p *EventProcessor) ProcessEvent(ctx context.Context, ev GitHubEvent) error {
	loglib.Info("webhook: processing event",
		loglib.FieldComponent, "webhook",
		"delivery_id", ev.DeliveryID,
		"event_type", ev.EventType,
		loglib.FieldRepo, ev.Repo,
	)

	switch ev.EventType {
	case "pull_request":
		return p.handlePullRequest(ctx, ev)
	case "pull_request_review":
		return p.handlePullRequestReview(ctx, ev)
	case "check_run":
		return p.handleCheckRun(ctx, ev)
	default:
		return nil
	}
}

// handlePullRequest processes pull_request events.
// "closed" events transition the branch to merged/abandoned (T18).
// "synchronize" events detect a new head hash (force-push / new commit) and
// transition to stale (T12) so the runner re-reviews.
func (p *EventProcessor) handlePullRequest(ctx context.Context, ev GitHubEvent) error {
	action, _ := ev.Payload["action"].(string)
	branch := prBranch(ev.Payload)
	headHash := prHeadSHA(ev.Payload)

	if branch == "" {
		return nil
	}

	rec, err := state.GetBranch(p.db, ev.Repo, branch)
	if err != nil {
		if errors.Is(err, state.ErrBranchNotFound) {
			// Branch not tracked in branch_states. Still attempt link/cache
			// updates since a github_link may exist for this branch.
			p.updateLinkAndInvalidateCache(ctx, ev.Repo, branch, action, headHash, prNumber(ev.Payload))
			return nil
		}
		return fmt.Errorf("get branch %s/%s: %w", ev.Repo, branch, err)
	}

	switch action {
	case "closed":
		targetState := state.StateAbandoned
		trigger := "pr_closed_webhook"
		if prMerged(ev.Payload) {
			targetState = state.StateMerged
			trigger = "pr_merged_webhook"
		}
		if rec.State != targetState {
			if err := state.TransitionBranch(p.db, rec.ID, rec.State, targetState, trigger); err != nil {
				loglib.Info("webhook: pr_closed transition skipped",
					loglib.FieldEventType, loglib.EventRejection,
					loglib.FieldComponent, "webhook",
					loglib.FieldRepo, ev.Repo,
					loglib.FieldBranch, branch,
					"error", err,
				)
			} else {
				eventReason := "pr_closed"
				if targetState == state.StateMerged {
					eventReason = "pr_merged"
				}
				_, _ = p.stream.AppendSystem(ctx, ev.Repo, branch, headHash, eventReason, "pull_request closed via webhook")
				loglib.Info("webhook: pr_closed transition applied",
					loglib.FieldEventType, loglib.EventTransition,
					loglib.FieldComponent, "webhook",
					loglib.FieldRepo, ev.Repo,
					loglib.FieldBranch, branch,
				)
			}
		}

	case "synchronize":
		if headHash != "" && headHash != rec.HeadHash {
			if err := state.UpdateHeadHashAndTransition(p.db, rec.ID, headHash, rec.State, state.StateStale, "synchronize_webhook"); err != nil {
				loglib.Info("webhook: synchronize transition skipped",
					loglib.FieldEventType, loglib.EventRejection,
					loglib.FieldComponent, "webhook",
					loglib.FieldRepo, ev.Repo,
					loglib.FieldBranch, branch,
					"error", err,
				)
			} else {
				_, _ = p.stream.AppendSystem(ctx, ev.Repo, branch, headHash, "head_updated",
					fmt.Sprintf("new head %s via synchronize webhook", headHash))
				loglib.Info("webhook: synchronize transition applied",
					loglib.FieldEventType, loglib.EventTransition,
					loglib.FieldComponent, "webhook",
					loglib.FieldRepo, ev.Repo,
					loglib.FieldBranch, branch,
					"new_head", headHash,
				)
			}
		}
	}

	// Best-effort: update github link and invalidate feedback cache.
	p.updateLinkAndInvalidateCache(ctx, ev.Repo, branch, action, headHash, prNumber(ev.Payload))

	return nil
}

// handlePullRequestReview processes pull_request_review submitted events.
// When a GitHubClient is available it re-fetches the full aggregated PR state
// (all reviewers via resolveApprovalStatus) to avoid multi-reviewer overwrites.
// Without a client it falls back to applying the single-event delta.
func (p *EventProcessor) handlePullRequestReview(ctx context.Context, ev GitHubEvent) error {
	// Only process review submissions; ignore edits and dismissals here —
	// the reconciler backstop will catch those within its polling interval.
	action, _ := ev.Payload["action"].(string)
	if action != "submitted" {
		return nil
	}

	reviewState, _ := ev.Payload["review"].(map[string]any)
	if reviewState == nil {
		return nil
	}
	stateStr, _ := reviewState["state"].(string)

	branch := prBranch(ev.Payload)
	headHash := prHeadSHA(ev.Payload)
	if branch == "" {
		return nil
	}

	rec, err := state.GetBranch(p.db, ev.Repo, branch)
	if err != nil {
		if errors.Is(err, state.ErrBranchNotFound) {
			return nil
		}
		return fmt.Errorf("get branch %s/%s: %w", ev.Repo, branch, err)
	}

	var approved bool
	var unresolvedThreads int

	if p.github != nil {
		// Re-fetch full state to aggregate all reviewers correctly.
		ghState, err := p.github.GetPRState(ctx, ev.Repo, branch)
		if err != nil || ghState == nil {
			// Best-effort: skip — reconciler will correct within its interval.
			loglib.Info("webhook: review handler skipping (GetPRState unavailable)",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, ev.Repo,
				loglib.FieldBranch, branch,
			)
			return nil
		}
		approved = ghState.Approved
		unresolvedThreads = ghState.UnresolvedThreads
	} else {
		// Fallback: apply single-event delta. Correct for single-reviewer PRs;
		// the reconciler corrects multi-reviewer edge cases within its interval.
		switch stateStr {
		case "approved":
			approved = true
		case "changes_requested":
			approved = false
			unresolvedThreads = 1
		default:
			return nil // COMMENTED — nothing to update
		}
	}

	if err := state.UpdateMergeReadiness(p.db, rec.ID,
		approved, rec.CIGreen,
		rec.PendingEvents, unresolvedThreads,
	); err != nil {
		loglib.Error("webhook: update merge readiness failed",
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, ev.Repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
		return nil
	}

	_, _ = p.stream.AppendSystem(ctx, ev.Repo, branch, headHash,
		"review_"+stateStr, "pull_request_review webhook")

	loglib.Info("webhook: merge readiness updated",
		loglib.FieldComponent, "webhook",
		loglib.FieldRepo, ev.Repo,
		loglib.FieldBranch, branch,
		"approved", approved,
	)

	// Best-effort: invalidate feedback cache for the linked task.
	p.invalidateCacheForBranch(ctx, ev.Repo, branch)

	// OCL-022: Normalize review body into findings and attempt PTY delivery.
	reviewBody, _ := reviewState["body"].(string)
	var reviewSource string
	if user, _ := reviewState["user"].(map[string]any); user != nil {
		reviewSource, _ = user["login"].(string)
	}
	if reviewSource == "" {
		reviewSource = "unknown-reviewer"
	}

	runID := ev.DeliveryID
	findings := normalizeReviewFindings(ev.Repo, branch, reviewBody, stateStr, reviewSource, runID)
	if len(findings) > 0 {
		// Create a review_runs parent row so findings FK is satisfied.
		if err := state.InsertReviewRun(ctx, p.db, runID, ev.Repo, branch, headHash, "webhook-review", "completed"); err != nil {
			loglib.Warn("webhook: insert review run failed",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, ev.Repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		} else if err := state.InsertFindings(p.db, findings); err != nil {
			loglib.Warn("webhook: insert findings failed",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, ev.Repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		} else {
			p.maybeDeliverFindings(ctx, ev.Repo, branch, findings)
		}
	}

	return nil
}

// handleCheckRun processes check_run completed events.
// Updates ci_green and may transition to merge_ready or revoke it.
func (p *EventProcessor) handleCheckRun(ctx context.Context, ev GitHubEvent) error {
	cr, _ := ev.Payload["check_run"].(map[string]any)
	if cr == nil {
		return nil
	}
	status, _ := cr["status"].(string)
	if status != "completed" {
		return nil
	}
	conclusion, _ := cr["conclusion"].(string)

	// Extract branch from check_run → check_suite → head_branch.
	branch := checkRunBranch(ev.Payload)
	headHash := checkRunHeadSHA(ev.Payload)
	if branch == "" {
		return nil
	}

	rec, err := state.GetBranch(p.db, ev.Repo, branch)
	if err != nil {
		if errors.Is(err, state.ErrBranchNotFound) {
			return nil
		}
		return fmt.Errorf("get branch %s/%s: %w", ev.Repo, branch, err)
	}

	if p.github != nil {
		// Re-fetch full state so all sibling check-runs are considered, not
		// just this single event. A single "success" check does not mean CI
		// is green when other checks are still failing or in progress.
		ghState, err := p.github.GetPRState(ctx, ev.Repo, branch)
		if err != nil || ghState == nil {
			loglib.Info("webhook: check_run handler skipping (GetPRState unavailable)",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, ev.Repo,
				loglib.FieldBranch, branch,
			)
			return nil
		}
		if err := state.UpdateMergeReadiness(p.db, rec.ID,
			ghState.Approved, ghState.CIGreen,
			ghState.PendingEvents, ghState.UnresolvedThreads,
		); err != nil {
			loglib.Error("webhook: update merge readiness failed",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, ev.Repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
			return nil
		}
		_, _ = p.stream.AppendSystem(ctx, ev.Repo, branch, headHash,
			"check_run_"+conclusion, "check_run webhook")
		loglib.Info("webhook: ci status updated",
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, ev.Repo,
			loglib.FieldBranch, branch,
			"ci_green", ghState.CIGreen,
		)

		// Best-effort: update CI run ID on link and invalidate feedback cache.
		p.updateCILinkAndInvalidateCache(ctx, ev.Repo, branch, checkRunID(ev.Payload))

		return nil
	}

	// Fallback: single-event delta (no GitHub client).
	ciGreen := false
	switch conclusion {
	case "success", "neutral", "skipped":
		ciGreen = true
	}

	if err := state.UpdateMergeReadiness(p.db, rec.ID,
		rec.Approved, ciGreen,
		rec.PendingEvents, rec.UnresolvedThreads,
	); err != nil {
		loglib.Error("webhook: update merge readiness failed",
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, ev.Repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
		return nil
	}

	_, _ = p.stream.AppendSystem(ctx, ev.Repo, branch, headHash,
		"check_run_"+conclusion, "check_run webhook")

	loglib.Info("webhook: ci status updated",
		loglib.FieldComponent, "webhook",
		loglib.FieldRepo, ev.Repo,
		loglib.FieldBranch, branch,
		"ci_green", ciGreen,
		"conclusion", conclusion,
	)

	// Best-effort: update CI run ID on link and invalidate feedback cache.
	p.updateCILinkAndInvalidateCache(ctx, ev.Repo, branch, checkRunID(ev.Payload))

	return nil
}

// prBranch extracts the head branch name from a pull_request payload.
func prBranch(payload map[string]any) string {
	pr, _ := payload["pull_request"].(map[string]any)
	if pr == nil {
		return ""
	}
	head, _ := pr["head"].(map[string]any)
	if head == nil {
		return ""
	}
	ref, _ := head["ref"].(string)
	return ref
}

// prHeadSHA extracts the head commit SHA from a pull_request payload.
func prHeadSHA(payload map[string]any) string {
	pr, _ := payload["pull_request"].(map[string]any)
	if pr == nil {
		return ""
	}
	head, _ := pr["head"].(map[string]any)
	if head == nil {
		return ""
	}
	sha, _ := head["sha"].(string)
	return sha
}

// prMerged extracts the merged boolean from a pull_request payload.
func prMerged(payload map[string]any) bool {
	pr, _ := payload["pull_request"].(map[string]any)
	if pr == nil {
		return false
	}
	merged, _ := pr["merged"].(bool)
	return merged
}

// checkRunBranch extracts the branch from a check_run payload via check_suite.
func checkRunBranch(payload map[string]any) string {
	cr, _ := payload["check_run"].(map[string]any)
	if cr == nil {
		return ""
	}
	cs, _ := cr["check_suite"].(map[string]any)
	if cs == nil {
		return ""
	}
	branch, _ := cs["head_branch"].(string)
	return branch
}

// checkRunHeadSHA extracts the HEAD sha from a check_run payload.
func checkRunHeadSHA(payload map[string]any) string {
	cr, _ := payload["check_run"].(map[string]any)
	if cr == nil {
		return ""
	}
	sha, _ := cr["head_sha"].(string)
	return sha
}

// prNumber extracts the pull request number from a pull_request payload.
func prNumber(payload map[string]any) int {
	pr, _ := payload["pull_request"].(map[string]any)
	if pr == nil {
		return 0
	}
	num, _ := pr["number"].(float64) // JSON numbers decode as float64
	return int(num)
}

// checkRunID extracts the check_run ID from a check_run payload as a string.
func checkRunID(payload map[string]any) string {
	cr, _ := payload["check_run"].(map[string]any)
	if cr == nil {
		return ""
	}
	id, ok := cr["id"].(float64) // JSON numbers decode as float64
	if !ok {
		return ""
	}
	return strconv.FormatInt(int64(id), 10)
}

// updateLinkAndInvalidateCache is the best-effort post-processing for
// pull_request events. It looks up the github link by (repo, branch) and:
//   - For "opened": sets pr_number + pr_state "open"
//   - For "closed": sets pr_state "closed"
//   - For "synchronize": updates head_sha
//
// After any link update it invalidates the feedback cache for the linked task.
func (p *EventProcessor) updateLinkAndInvalidateCache(ctx context.Context, repo, branch, action, headHash string, prNum int) {
	link, err := state.GetLinkByBranch(ctx, p.db, repo, branch)
	if err != nil {
		if !errors.Is(err, state.ErrGitHubLinkNotFound) {
			loglib.Info("webhook: get link by branch failed (best-effort)",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		}
		return
	}

	switch action {
	case "opened":
		if prNum > 0 {
			link.PRNumber = prNum
		}
		link.PRState = "open"
		if err := state.UpsertGitHubLink(ctx, p.db, link); err != nil {
			loglib.Info("webhook: upsert link for PR opened failed (best-effort)",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		}
	case "closed":
		if err := state.UpdateLinkPRState(ctx, p.db, link.LinkID, "closed"); err != nil {
			loglib.Info("webhook: update link pr_state closed failed (best-effort)",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		}
	case "synchronize":
		if headHash != "" {
			if err := state.UpdateLinkHeadSHA(ctx, p.db, link.LinkID, headHash); err != nil {
				loglib.Info("webhook: update link head_sha failed (best-effort)",
					loglib.FieldComponent, "webhook",
					loglib.FieldRepo, repo,
					loglib.FieldBranch, branch,
					"error", err,
				)
			}
		}
	}

	// Invalidate feedback cache for the linked task.
	if err := state.InvalidateFeedbackCacheByTaskID(ctx, p.db, link.TaskID); err != nil {
		loglib.Info("webhook: invalidate feedback cache failed (best-effort)",
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}
}

// invalidateCacheForBranch looks up the github link by (repo, branch) and
// invalidates the feedback cache for the linked task. Best-effort — errors
// are logged at Info level and do not fail the event.
func (p *EventProcessor) invalidateCacheForBranch(ctx context.Context, repo, branch string) {
	link, err := state.GetLinkByBranch(ctx, p.db, repo, branch)
	if err != nil {
		if !errors.Is(err, state.ErrGitHubLinkNotFound) {
			loglib.Info("webhook: get link by branch failed (best-effort)",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		}
		return
	}

	if err := state.InvalidateFeedbackCacheByTaskID(ctx, p.db, link.TaskID); err != nil {
		loglib.Info("webhook: invalidate feedback cache failed (best-effort)",
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}
}

// updateCILinkAndInvalidateCache looks up the github link by (repo, branch),
// updates the last_ci_run_id, and invalidates the feedback cache. Best-effort.
func (p *EventProcessor) updateCILinkAndInvalidateCache(ctx context.Context, repo, branch, ciRunID string) {
	link, err := state.GetLinkByBranch(ctx, p.db, repo, branch)
	if err != nil {
		if !errors.Is(err, state.ErrGitHubLinkNotFound) {
			loglib.Info("webhook: get link by branch failed (best-effort)",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		}
		return
	}

	if ciRunID != "" {
		if err := state.UpdateLinkCIRunID(ctx, p.db, link.LinkID, ciRunID); err != nil {
			loglib.Info("webhook: update link ci_run_id failed (best-effort)",
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
		}
	}

	if err := state.InvalidateFeedbackCacheByTaskID(ctx, p.db, link.TaskID); err != nil {
		loglib.Info("webhook: invalidate feedback cache failed (best-effort)",
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
	}
}

// maybeDeliverFindings looks up an active session for the repo/branch
// and sends findings to the OpenClaw adapter for PTY delivery.
// Best-effort: errors are logged but never returned.
func (p *EventProcessor) maybeDeliverFindings(ctx context.Context, repo, branch string, findings []*state.FindingRecord) {
	if p.openclaw == nil || len(findings) == 0 {
		return
	}

	session, err := state.FindActiveSessionForBranch(ctx, p.db, repo, branch)
	if err != nil {
		loglib.Warn("webhook: session lookup for delivery failed",
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
			"error", err,
		)
		return
	}
	if session == nil {
		loglib.Info("webhook: no active session for branch, skipping delivery",
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, repo,
			loglib.FieldBranch, branch,
		)
		return
	}

	_ = p.openclaw.Deliver(ctx, session.SessionID, findings, "codero-webhook")
}
