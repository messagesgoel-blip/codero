package github

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// MergeReadiness captures all conditions that must be satisfied before merging.
type MergeReadiness struct {
	RuleChecksPassing         bool
	NoUnresolvedCoderabbit    bool
	CISuccess                 bool
	NoBlockingHumanReview     bool
	NoMergeConflicts          bool
	BranchUpToDate            bool
	GitHubProtectionSatisfied bool
}

// MergeReady returns true only when ALL conditions are met.
func (m *MergeReadiness) MergeReady() bool {
	return m.RuleChecksPassing &&
		m.NoUnresolvedCoderabbit &&
		m.CISuccess &&
		m.NoBlockingHumanReview &&
		m.NoMergeConflicts &&
		m.BranchUpToDate &&
		m.GitHubProtectionSatisfied
}

// BlockingReasons returns human-readable reasons for any unmet conditions.
func (m *MergeReadiness) BlockingReasons() []string {
	var reasons []string
	if !m.RuleChecksPassing {
		reasons = append(reasons, "rule checks are not passing")
	}
	if !m.NoUnresolvedCoderabbit {
		reasons = append(reasons, "unresolved CodeRabbit review threads")
	}
	if !m.CISuccess {
		reasons = append(reasons, "CI status is not success")
	}
	if !m.NoBlockingHumanReview {
		reasons = append(reasons, "blocking human review (changes requested)")
	}
	if !m.NoMergeConflicts {
		reasons = append(reasons, "merge conflicts detected")
	}
	if !m.BranchUpToDate {
		reasons = append(reasons, "branch is not up to date with base")
	}
	if !m.GitHubProtectionSatisfied {
		reasons = append(reasons, "branch protection rules not satisfied")
	}
	return reasons
}

// MergeMethod returns the merge method from CODERO_MERGE_METHOD env var.
// Defaults to "squash" if unset. Valid values: "merge", "squash", "rebase".
func MergeMethod() string {
	v := os.Getenv("CODERO_MERGE_METHOD")
	switch v {
	case "merge", "squash", "rebase":
		return v
	default:
		return "squash"
	}
}

// EvaluateMergeReadiness fetches all relevant state from GitHub and evaluates
// whether a PR is ready to merge.
func (c *Client) EvaluateMergeReadiness(ctx context.Context, repo string, prNumber int) (*MergeReadiness, error) {
	owner, repoName, _ := strings.Cut(repo, "/")

	readiness := &MergeReadiness{
		RuleChecksPassing:         true, // assumed true unless we have rule check data
		GitHubProtectionSatisfied: true, // assumed true unless protection check fails
	}

	// Fetch PR details to check mergeable state and head SHA
	pr, _, err := c.gh().PullRequests.Get(ctx, owner, repoName, prNumber)
	if err != nil {
		return nil, fmt.Errorf("get PR: %w", err)
	}

	// Check merge conflicts
	mergeable := pr.GetMergeable()
	mergeableState := pr.GetMergeableState()
	readiness.NoMergeConflicts = mergeable

	// Branch up to date: GitHub's mergeable_state tells us
	// "behind" means branch is not up to date; "clean" or "unstable" means it is
	readiness.BranchUpToDate = mergeableState != "behind"

	// Fetch CI status via check runs
	headSHA := pr.GetHead().GetSHA()
	runs, err := c.ListCheckRuns(ctx, repo, headSHA)
	if err != nil {
		return nil, fmt.Errorf("list check runs: %w", err)
	}
	readiness.CISuccess = isCIGreen(runs)

	// Fetch reviews for approval/blocking status
	reviews, err := c.ListPRReviews(ctx, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}

	_, changesRequested := resolveApprovalStatus(reviews)
	readiness.NoBlockingHumanReview = !changesRequested

	// Check for unresolved CodeRabbit reviews
	coderabbitUnresolved := false
	for _, r := range reviews {
		if IsBot(r.User) && r.State == "CHANGES_REQUESTED" {
			coderabbitUnresolved = true
			break
		}
	}
	readiness.NoUnresolvedCoderabbit = !coderabbitUnresolved

	// Check branch protection
	_, err = c.GetBranchProtection(ctx, repo, pr.GetBase().GetRef())
	if err != nil {
		// If we can't fetch protection, it might not exist (which is fine)
		// or we don't have permissions. Assume satisfied if 404.
		if !strings.Contains(err.Error(), "404") {
			readiness.GitHubProtectionSatisfied = false
		}
	}

	return readiness, nil
}

// MergePRWithMethod merges a PR using the CODERO_MERGE_METHOD setting.
// It validates the SHA matches before merging.
func (c *Client) MergePRWithMethod(ctx context.Context, repo string, prNumber int, expectedSHA string) error {
	// Verify current HEAD SHA matches expected
	owner, repoName, _ := strings.Cut(repo, "/")
	pr, _, err := c.gh().PullRequests.Get(ctx, owner, repoName, prNumber)
	if err != nil {
		return fmt.Errorf("get PR for SHA check: %w", err)
	}

	currentSHA := pr.GetHead().GetSHA()
	if currentSHA != expectedSHA {
		return fmt.Errorf("stale SHA: expected %s, current is %s", expectedSHA, currentSHA)
	}

	method := MergeMethod()
	return c.MergePR(ctx, repo, prNumber, expectedSHA, method)
}
