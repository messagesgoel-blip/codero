package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	deliverypipeline "github.com/codero/codero/internal/delivery_pipeline"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/normalizer"
	"github.com/codero/codero/internal/state"
)

type feedbackWriter interface {
	WriteFEEDBACK(worktree string, feedback deliverypipeline.FeedbackPackage) error
}

type feedbackNotifier interface {
	Notify(worktree, notificationType, assignmentID string)
}

type defaultFeedbackWriter struct{}

func (defaultFeedbackWriter) WriteFEEDBACK(worktree string, feedback deliverypipeline.FeedbackPackage) error {
	return deliverypipeline.WriteFEEDBACK(worktree, feedback)
}

type defaultFeedbackNotifier struct{}

func (defaultFeedbackNotifier) Notify(worktree, notificationType, assignmentID string) {
	deliverypipeline.Notify(worktree, notificationType, assignmentID)
}

// WithFeedbackPush overrides the writer/notifier used for feedback delivery.
func (r *Reconciler) WithFeedbackPush(writer feedbackWriter, notifier feedbackNotifier) *Reconciler {
	if writer != nil {
		r.feedbackWriter = writer
	}
	if notifier != nil {
		r.feedbackNotifier = notifier
	}
	return r
}

func (r *Reconciler) maybePushFeedback(ctx context.Context, b state.BranchRecord) {
	if r.db == nil || r.feedbackWriter == nil || r.feedbackNotifier == nil {
		return
	}

	link, err := state.GetLinkByBranch(ctx, r.db, b.Repo, b.Branch)
	if err != nil {
		if !errors.Is(err, state.ErrGitHubLinkNotFound) {
			loglib.Warn("reconciler: lookup link failed",
				loglib.FieldComponent, "reconciler",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
		return
	}

	assignment, err := state.GetActiveAssignmentByTaskID(ctx, r.db, link.TaskID)
	if err != nil {
		if !errors.Is(err, state.ErrAgentAssignmentNotFound) {
			loglib.Warn("reconciler: lookup assignment failed",
				loglib.FieldComponent, "reconciler",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
		return
	}
	if assignment.EndedAt != nil || assignment.Worktree == "" {
		return
	}
	if assignment.Repo != "" && assignment.Repo != b.Repo {
		return
	}
	if assignment.Branch != "" && assignment.Branch != b.Branch {
		return
	}

	cache, err := state.GetFeedbackCacheByTaskID(ctx, r.db, link.TaskID)
	if err != nil {
		if !errors.Is(err, state.ErrFeedbackCacheNotFound) {
			loglib.Warn("reconciler: feedback cache lookup failed",
				loglib.FieldComponent, "reconciler",
				loglib.FieldRepo, b.Repo,
				loglib.FieldBranch, b.Branch,
				"error", err,
			)
		}
		return
	}
	if cache.AssignmentID != "" && cache.AssignmentID != assignment.ID {
		return
	}

	if cache.CacheHash != "" {
		if existingHash, err := readFeedbackCacheHash(assignment.Worktree); err == nil {
			if existingHash == cache.CacheHash {
				return
			}
		}
	}

	sources := sourcesFromFeedbackCache(cache)
	feedback, err := deliverypipeline.BuildFeedback(sources)
	if err != nil {
		loglib.Warn("reconciler: build feedback failed",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
		return
	}
	if feedback == nil {
		return
	}
	feedback.CacheHash = cache.CacheHash

	if err := r.feedbackWriter.WriteFEEDBACK(assignment.Worktree, *feedback); err != nil {
		loglib.Warn("reconciler: write feedback failed",
			loglib.FieldComponent, "reconciler",
			loglib.FieldRepo, b.Repo,
			loglib.FieldBranch, b.Branch,
			"error", err,
		)
		return
	}
	r.feedbackNotifier.Notify(assignment.Worktree, "feedback", assignment.ID)

	if isActionableFeedback(feedback.Sections) {
		if assignment.Substatus != state.AssignmentSubstatusNeedsRevision {
			if _, err := state.EmitAssignmentUpdate(ctx, r.db, assignment.ID, assignment.Version, state.AssignmentSubstatusNeedsRevision); err != nil {
				loglib.Warn("reconciler: update assignment substatus failed",
					loglib.FieldComponent, "reconciler",
					loglib.FieldRepo, b.Repo,
					loglib.FieldBranch, b.Branch,
					"error", err,
				)
			}
		}
	}
}

func readFeedbackCacheHash(worktree string) (string, error) {
	path := filepath.Join(worktree, ".codero", "feedback", "current.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var payload struct {
		CacheHash string `json:"cache_hash"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.CacheHash), nil
}

func sourcesFromFeedbackCache(cache *state.FeedbackCache) []deliverypipeline.FeedbackSource {
	if cache == nil {
		return nil
	}

	var sources []deliverypipeline.FeedbackSource
	if items := parseFeedbackItems(cache.HumanReviewSnapshot); len(items) > 0 {
		sources = append(sources, deliverypipeline.FeedbackSource{
			Type:     deliverypipeline.FeedbackSourceHuman,
			Findings: items,
		})
	}
	if items := parseFeedbackItems(cache.CoderabbitSnapshot); len(items) > 0 {
		sources = append(sources, deliverypipeline.FeedbackSource{
			Type:     deliverypipeline.FeedbackSourceCoderabbit,
			Findings: items,
		})
	}
	if items := parseFeedbackItems(cache.ComplianceSnapshot); len(items) > 0 {
		sources = append(sources, deliverypipeline.FeedbackSource{
			Type:     deliverypipeline.FeedbackSourceGate,
			Findings: items,
		})
	}
	if items := parseFeedbackItems(cache.CISnapshot); len(items) > 0 {
		sources = append(sources, deliverypipeline.FeedbackSource{
			Type:     deliverypipeline.FeedbackSourceCI,
			Findings: items,
		})
	}
	return sources
}

func parseFeedbackItems(raw string) []deliverypipeline.FeedbackItem {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var items []deliverypipeline.FeedbackItem
	if json.Unmarshal([]byte(raw), &items) == nil && len(items) > 0 {
		return items
	}

	var findings []normalizer.Finding
	if json.Unmarshal([]byte(raw), &findings) == nil && len(findings) > 0 {
		return feedbackItemsFromFindings(findings)
	}

	var stringItems []string
	if json.Unmarshal([]byte(raw), &stringItems) == nil && len(stringItems) > 0 {
		return feedbackItemsFromStrings(stringItems)
	}

	var envelope struct {
		Findings []deliverypipeline.FeedbackItem `json:"findings"`
	}
	if json.Unmarshal([]byte(raw), &envelope) == nil && len(envelope.Findings) > 0 {
		return envelope.Findings
	}

	var findingEnvelope struct {
		Findings []normalizer.Finding `json:"findings"`
	}
	if json.Unmarshal([]byte(raw), &findingEnvelope) == nil && len(findingEnvelope.Findings) > 0 {
		return feedbackItemsFromFindings(findingEnvelope.Findings)
	}

	var single struct {
		Message string `json:"message"`
		Body    string `json:"body"`
		Summary string `json:"summary"`
		Status  string `json:"status"`
		File    string `json:"file"`
		Line    int    `json:"line"`
	}
	if json.Unmarshal([]byte(raw), &single) == nil {
		msg := strings.TrimSpace(single.Message)
		if msg == "" {
			msg = strings.TrimSpace(single.Body)
		}
		if msg == "" {
			msg = strings.TrimSpace(single.Summary)
		}
		status := strings.ToLower(strings.TrimSpace(single.Status))
		if msg == "" {
			if status == "success" || status == "pass" || status == "approved" || status == "neutral" || status == "skipped" {
				return nil
			}
			if status != "" {
				msg = status
			}
		}
		if msg != "" {
			return []deliverypipeline.FeedbackItem{{
				File:    strings.TrimSpace(single.File),
				Line:    single.Line,
				Message: msg,
			}}
		}
	}

	return []deliverypipeline.FeedbackItem{{Message: raw}}
}

func feedbackItemsFromFindings(findings []normalizer.Finding) []deliverypipeline.FeedbackItem {
	items := make([]deliverypipeline.FeedbackItem, 0, len(findings))
	for _, finding := range findings {
		msg := strings.TrimSpace(finding.Message)
		if msg == "" {
			continue
		}
		items = append(items, deliverypipeline.FeedbackItem{
			File:    finding.File,
			Line:    finding.Line,
			Message: msg,
		})
	}
	return items
}

func feedbackItemsFromStrings(messages []string) []deliverypipeline.FeedbackItem {
	items := make([]deliverypipeline.FeedbackItem, 0, len(messages))
	for _, msg := range messages {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			continue
		}
		items = append(items, deliverypipeline.FeedbackItem{Message: msg})
	}
	return items
}

func isActionableFeedback(sections []deliverypipeline.FeedbackSection) bool {
	for _, section := range sections {
		if len(section.Items) == 0 {
			continue
		}
		switch deliverypipeline.FeedbackSourceType(section.Source) {
		case deliverypipeline.FeedbackSourceInformational:
			continue
		default:
			return true
		}
	}
	return false
}
