package runner

import (
	"context"
	"fmt"
	"strings"

	ghclient "github.com/codero/codero/internal/github"
	"github.com/codero/codero/internal/normalizer"
)

// codeRabbitUser is the GitHub login used by the CodeRabbit review bot.
const codeRabbitUser = "coderabbitai"

// githubReviewClient is the subset of ghclient.Client used by GitHubProvider.
// Declared as an interface so tests can inject a stub.
type githubReviewClient interface {
	FindOpenPR(ctx context.Context, repo, branch string) (*ghclient.PRInfo, error)
	ListPRReviewComments(ctx context.Context, repo string, prNumber int) ([]ghclient.ReviewComment, error)
	ListPRReviews(ctx context.Context, repo string, prNumber int) ([]ghclient.Review, error)
}

// GitHubProvider is a review Provider that fetches CodeRabbit review comments
// from GitHub pull requests and converts them into normalizer.RawFindings.
//
// It looks for inline comments and top-level review bodies submitted by the
// coderabbitai bot and maps each comment to one RawFinding. If no CodeRabbit
// review exists the provider returns an empty finding list (not an error).
type GitHubProvider struct {
	github githubReviewClient
}

// NewGitHubProvider creates a GitHubProvider backed by the given GitHub client.
func NewGitHubProvider(gh *ghclient.Client) *GitHubProvider {
	return &GitHubProvider{github: gh}
}

// Name implements Provider.
func (p *GitHubProvider) Name() string { return "coderabbit" }

// Review implements Provider. It fetches the open PR for req.Branch, collects
// all CodeRabbit review comments and review bodies, and returns them as
// RawFindings. Returns (empty, nil) if no PR or no CodeRabbit comments exist.
func (p *GitHubProvider) Review(ctx context.Context, req ReviewRequest) (*ReviewResponse, error) {
	pr, err := p.github.FindOpenPR(ctx, req.Repo, req.Branch)
	if err != nil {
		return nil, fmt.Errorf("find open PR: %w", err)
	}
	if pr == nil {
		// No open PR yet — return empty findings, not an error.
		return &ReviewResponse{}, nil
	}

	var findings []normalizer.RawFinding

	// Inline review comments from CodeRabbit.
	comments, err := p.github.ListPRReviewComments(ctx, req.Repo, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("list review comments: %w", err)
	}
	for _, c := range comments {
		if !strings.EqualFold(c.User, codeRabbitUser) {
			continue
		}
		// Skip comments from prior commits — only surface findings on the current head.
		if req.HeadHash != "" && c.CommitID != "" && c.CommitID != req.HeadHash {
			continue
		}
		findings = append(findings, normalizer.RawFinding{
			Severity: inferSeverity(c.Body),
			Category: "review",
			File:     c.Path,
			Line:     c.Line,
			Message:  c.Body,
			Source:   codeRabbitUser,
		})
	}

	// Top-level review bodies from CodeRabbit (summary comment on the review).
	reviews, err := p.github.ListPRReviews(ctx, req.Repo, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	latestSummary := normalizer.RawFinding{}
	latestSummaryID := int64(0)
	addedHeadMatchedSummary := false
	for _, r := range reviews {
		if !strings.EqualFold(r.User, codeRabbitUser) {
			continue
		}
		body := strings.TrimSpace(r.Body)
		if body == "" {
			continue
		}
		if r.ID > latestSummaryID {
			latestSummaryID = r.ID
			latestSummary = normalizer.RawFinding{
				Severity: inferSeverity(body),
				Category: "review_summary",
				File:     "",
				Line:     0,
				Message:  body,
				Source:   codeRabbitUser,
			}
		}
		if req.HeadHash != "" && r.CommitID != "" && r.CommitID != req.HeadHash {
			continue
		}
		addedHeadMatchedSummary = true
		findings = append(findings, normalizer.RawFinding{
			Severity: inferSeverity(body),
			Category: "review_summary",
			File:     "",
			Line:     0,
			Message:  body,
			Source:   codeRabbitUser,
		})
	}
	if req.HeadHash != "" && !addedHeadMatchedSummary && latestSummaryID != 0 {
		findings = append(findings, latestSummary)
	}

	return &ReviewResponse{Findings: findings}, nil
}

// inferSeverity applies a simple heuristic to map CodeRabbit comment text to a
// severity level. CodeRabbit prefixes actionable comments with emoji or
// keywords; we scan for the most common ones.
func inferSeverity(body string) string {
	lower := strings.ToLower(body)
	switch {
	case strings.Contains(lower, "critical") ||
		strings.Contains(lower, "security") ||
		strings.Contains(lower, "vulnerability") ||
		strings.Contains(lower, "bug") ||
		strings.Contains(lower, "error"):
		return "error"
	case strings.Contains(lower, "warning") ||
		strings.Contains(lower, "warn") ||
		strings.Contains(lower, "should") ||
		strings.Contains(lower, "consider") ||
		strings.Contains(lower, "improvement"):
		return "warning"
	default:
		return "info"
	}
}
