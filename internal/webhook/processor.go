package webhook

import (
	"context"
	"errors"
	"fmt"

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
	db     *state.DB
	stream *delivery.Stream
	github GitHubClient // optional; enables aggregated state on review/check events
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
// "closed" events transition the branch to closed (T18).
// "synchronize" events detect a new head hash (force-push / new commit) and
// transition to stale_branch (T12) so the runner re-reviews.
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
			return nil // branch not tracked — nothing to do
		}
		return fmt.Errorf("get branch %s/%s: %w", ev.Repo, branch, err)
	}

	switch action {
	case "closed":
		if rec.State == state.StateClosed {
			return nil
		}
		if err := state.TransitionBranch(p.db, rec.ID, rec.State, state.StateClosed, "pr_closed_webhook"); err != nil {
			loglib.Info("webhook: pr_closed transition skipped",
				loglib.FieldEventType, loglib.EventRejection,
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, ev.Repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
			return nil
		}
		_, _ = p.stream.AppendSystem(ctx, ev.Repo, branch, headHash, "pr_closed", "pull_request closed via webhook")
		loglib.Info("webhook: pr_closed transition applied",
			loglib.FieldEventType, loglib.EventTransition,
			loglib.FieldComponent, "webhook",
			loglib.FieldRepo, ev.Repo,
			loglib.FieldBranch, branch,
		)

	case "synchronize":
		if headHash == "" || headHash == rec.HeadHash {
			return nil
		}
		// New commits pushed — transition to stale_branch (T12).
		if err := state.UpdateHeadHashAndTransition(p.db, rec.ID, headHash, rec.State, state.StateStaleBranch, "synchronize_webhook"); err != nil {
			loglib.Info("webhook: synchronize transition skipped",
				loglib.FieldEventType, loglib.EventRejection,
				loglib.FieldComponent, "webhook",
				loglib.FieldRepo, ev.Repo,
				loglib.FieldBranch, branch,
				"error", err,
			)
			return nil
		}
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
