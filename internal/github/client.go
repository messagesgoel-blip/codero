// Package github provides a GitHub REST API client for codero.
// It implements webhook.GitHubClient and exposes additional methods used by
// the review runner provider. All calls use google/go-github v69.
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v69/github"

	"github.com/codero/codero/internal/webhook"
)

const defaultAPIURL = "https://api.github.com"

// Client is a GitHub REST API client.
// It implements webhook.GitHubClient and provides additional methods for
// fetching PR review comments (used by the GitHubProvider review backend).
type Client struct {
	token  string
	http   *http.Client
	apiURL string
}

// NewClient creates a Client authenticated with the given personal access token.
// An http.Client may be injected via WithHTTPClient for testing.
func NewClient(token string) *Client {
	return &Client{
		token:  token,
		http:   &http.Client{Timeout: 15 * time.Second},
		apiURL: defaultAPIURL,
	}
}

// WithHTTPClient returns a copy of Client using the given *http.Client.
// Intended for tests that need a custom transport (e.g. httptest server).
// A nil hc is a no-op; the original client is returned unchanged.
func (c *Client) WithHTTPClient(hc *http.Client) *Client {
	if hc == nil {
		return c
	}
	cp := *c
	cp.http = hc
	return &cp
}

// gh returns a go-github client configured with the Client's HTTP client,
// token, and base URL. A fresh instance is created each call so that tests
// which swap apiURL between calls always get the correct endpoint.
func (c *Client) gh() *gogithub.Client {
	gc := gogithub.NewClient(c.http).WithAuthToken(c.token)
	baseURL := c.apiURL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	u, _ := url.Parse(baseURL)
	gc.BaseURL = u
	return gc
}

// PRInfo contains the fields codero needs from a GitHub pull request.
type PRInfo struct {
	Number  int
	HeadSHA string
	State   string // "open" or "closed"
}

// Review represents a single GitHub pull request review.
type Review struct {
	ID       int64
	User     string
	IsBot    bool
	State    string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED"
	Body     string
	CommitID string
}

// ReviewComment is an inline pull request review comment.
type ReviewComment struct {
	ID       int64
	User     string
	IsBot    bool
	Body     string
	Path     string
	Line     int
	CommitID string
}

// CheckRun represents a GitHub Actions / Checks API run for a commit.
type CheckRun struct {
	Name       string
	Status     string // "queued", "in_progress", "completed"
	Conclusion string // "success", "failure", "neutral", "cancelled", "skipped", "timed_out", "action_required"
}

// GetPRState implements webhook.GitHubClient.
// It returns (nil, nil) when no open PR exists for the branch.
func (c *Client) GetPRState(ctx context.Context, repo, branch string) (*webhook.GitHubState, error) {
	pr, err := c.FindOpenPR(ctx, repo, branch)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, nil
	}

	reviews, err := c.ListPRReviews(ctx, repo, pr.Number)
	if err != nil {
		return nil, fmt.Errorf("list PR reviews: %w", err)
	}

	approved, _ := resolveApprovalStatus(reviews)
	// countChangesRequested is a conservative proxy for unresolved threads:
	// proper thread resolution requires the GitHub GraphQL review-threads API.
	unresolvedThreads := countChangesRequested(reviews)

	runs, err := c.ListCheckRuns(ctx, repo, pr.HeadSHA)
	if err != nil {
		return nil, fmt.Errorf("list check runs: %w", err)
	}

	return &webhook.GitHubState{
		Repo:              repo,
		Branch:            branch,
		HeadHash:          pr.HeadSHA,
		PRNumber:          pr.Number,
		PROpen:            true,
		Approved:          approved,
		CIGreen:           isCIGreen(runs),
		PendingEvents:     0,
		UnresolvedThreads: unresolvedThreads,
	}, nil
}

// FindOpenPR returns the first open PR whose head branch matches branch.
// Returns (nil, nil) if no open PR exists.
// branch may be bare ("my-feature") or qualified ("owner:my-feature");
// bare branches are auto-qualified using the repo owner.
func (c *Client) FindOpenPR(ctx context.Context, repo, branch string) (*PRInfo, error) {
	owner, repoName, _ := strings.Cut(repo, "/")
	headFilter := branch
	if !strings.Contains(branch, ":") {
		headFilter = owner + ":" + branch
	}
	pulls, _, err := c.gh().PullRequests.List(ctx, owner, repoName, &gogithub.PullRequestListOptions{
		State:       "open",
		Head:        headFilter,
		ListOptions: gogithub.ListOptions{PerPage: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("find open PR: %w", err)
	}
	if len(pulls) == 0 {
		return nil, nil
	}
	p := pulls[0]
	return &PRInfo{
		Number:  p.GetNumber(),
		HeadSHA: p.GetHead().GetSHA(),
		State:   p.GetState(),
	}, nil
}

// ListPRReviews returns all reviews submitted on a pull request, following
// GitHub pagination until no further pages remain.
func (c *Client) ListPRReviews(ctx context.Context, repo string, prNumber int) ([]Review, error) {
	owner, repoName, _ := strings.Cut(repo, "/")
	opts := &gogithub.ListOptions{Page: 1, PerPage: 100}
	var all []Review
	for {
		reviews, resp, err := c.gh().PullRequests.ListReviews(ctx, owner, repoName, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("list PR reviews: %w", err)
		}
		for _, r := range reviews {
			login := r.GetUser().GetLogin()
			all = append(all, Review{
				ID:       r.GetID(),
				User:     login,
				IsBot:    IsBot(login),
				State:    r.GetState(),
				Body:     r.GetBody(),
				CommitID: r.GetCommitID(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// ListPRReviewComments returns all inline review comments on a pull request,
// following GitHub pagination until no further pages remain.
func (c *Client) ListPRReviewComments(ctx context.Context, repo string, prNumber int) ([]ReviewComment, error) {
	owner, repoName, _ := strings.Cut(repo, "/")
	opts := &gogithub.PullRequestListCommentsOptions{
		ListOptions: gogithub.ListOptions{Page: 1, PerPage: 100},
	}
	var all []ReviewComment
	for {
		comments, resp, err := c.gh().PullRequests.ListComments(ctx, owner, repoName, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("list PR review comments: %w", err)
		}
		for _, r := range comments {
			login := r.GetUser().GetLogin()
			all = append(all, ReviewComment{
				ID:       r.GetID(),
				User:     login,
				IsBot:    IsBot(login),
				Body:     r.GetBody(),
				Path:     r.GetPath(),
				Line:     r.GetLine(),
				CommitID: r.GetCommitID(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// ListCheckRuns returns check runs for the given commit SHA, following GitHub
// pagination until no further pages remain.
func (c *Client) ListCheckRuns(ctx context.Context, repo, sha string) ([]CheckRun, error) {
	owner, repoName, _ := strings.Cut(repo, "/")
	opts := &gogithub.ListCheckRunsOptions{
		ListOptions: gogithub.ListOptions{Page: 1, PerPage: 100},
	}
	var all []CheckRun
	for {
		result, resp, err := c.gh().Checks.ListCheckRunsForRef(ctx, owner, repoName, sha, opts)
		if err != nil {
			return nil, fmt.Errorf("list check runs: %w", err)
		}
		for _, r := range result.CheckRuns {
			all = append(all, CheckRun{
				Name:       r.GetName(),
				Status:     r.GetStatus(),
				Conclusion: r.GetConclusion(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// MergePR merges a pull request via the GitHub Merge API.
// mergeMethod must be "merge", "squash", or "rebase"; defaults to "squash" if empty.
// sha is the expected HEAD commit SHA — GitHub rejects the call with 409 if it
// has changed since the caller last fetched state.
//
// Implements webhook.AutoMerger.
func (c *Client) MergePR(ctx context.Context, repo string, prNumber int, sha, mergeMethod string) error {
	if mergeMethod == "" {
		mergeMethod = "squash"
	}
	owner, repoName, _ := strings.Cut(repo, "/")
	_, _, err := c.gh().PullRequests.Merge(ctx, owner, repoName, prNumber, "", &gogithub.PullRequestOptions{
		SHA:         sha,
		MergeMethod: mergeMethod,
	})
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "405") {
			return fmt.Errorf("PR not mergeable (405)")
		}
		if strings.Contains(errStr, "409") {
			return fmt.Errorf("merge conflict or SHA mismatch (409)")
		}
		return fmt.Errorf("merge PR: %w", err)
	}
	return nil
}

// CreatePR creates a pull request and returns the PR number.
func (c *Client) CreatePR(ctx context.Context, repo, head, base, title, body string) (int, error) {
	owner, repoName, _ := strings.Cut(repo, "/")
	pr, _, err := c.gh().PullRequests.Create(ctx, owner, repoName, &gogithub.NewPullRequest{
		Title: gogithub.Ptr(title),
		Head:  gogithub.Ptr(head),
		Base:  gogithub.Ptr(base),
		Body:  gogithub.Ptr(body),
	})
	if err != nil {
		return 0, fmt.Errorf("create PR: %w", err)
	}
	return pr.GetNumber(), nil
}

// PostComment posts a comment on a pull request.
func (c *Client) PostComment(ctx context.Context, repo string, prNumber int, body string) error {
	owner, repoName, _ := strings.Cut(repo, "/")
	_, _, err := c.gh().Issues.CreateComment(ctx, owner, repoName, prNumber, &gogithub.IssueComment{
		Body: gogithub.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("post comment: %w", err)
	}
	return nil
}

// GetBranchProtection returns branch protection rules for the given branch.
func (c *Client) GetBranchProtection(ctx context.Context, repo, branch string) (*gogithub.Protection, error) {
	owner, repoName, _ := strings.Cut(repo, "/")
	prot, _, err := c.gh().Repositories.GetBranchProtection(ctx, owner, repoName, branch)
	if err != nil {
		return nil, fmt.Errorf("get branch protection: %w", err)
	}
	return prot, nil
}

// countChangesRequested returns the number of distinct reviewers whose current
// state is CHANGES_REQUESTED. Used as a conservative proxy for unresolved
// threads; the actual count requires the GitHub GraphQL review-threads API.
func countChangesRequested(reviews []Review) int {
	latest := make(map[string]string)
	for _, r := range reviews {
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
			latest[r.User] = r.State
		}
	}
	count := 0
	for _, st := range latest {
		if st == "CHANGES_REQUESTED" {
			count++
		}
	}
	return count
}

// resolveApprovalStatus scans reviews in submission order.
// The latest non-COMMENTED state from each reviewer wins, matching GitHub's
// own "current review state" behaviour.
// IsBot returns true if the given login should be classified as a bot account.
// Classification happens once at fetch time per I-50; downstream code trusts
// the cached result without re-checking.
func IsBot(login string) bool {
	if strings.HasSuffix(login, "[bot]") {
		return true
	}
	lower := strings.ToLower(login)
	switch lower {
	case "coderabbitai", "dependabot", "renovate", "github-actions",
		"codecov", "sonarcloud", "mergify", "snyk-bot":
		return true
	}
	return false
}

func resolveApprovalStatus(reviews []Review) (approved, changesRequested bool) {
	latest := make(map[string]string)
	for _, r := range reviews {
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
			latest[r.User] = r.State
		}
	}
	for _, st := range latest {
		switch st {
		case "APPROVED":
			approved = true
		case "CHANGES_REQUESTED":
			changesRequested = true
		}
	}
	return approved, changesRequested
}

// isCIGreen returns true when every check run has completed with an acceptable
// conclusion. Returns false if any run is still in progress or has failed.
func isCIGreen(runs []CheckRun) bool {
	if len(runs) == 0 {
		return false
	}
	for _, r := range runs {
		if r.Status != "completed" {
			return false
		}
		switch r.Conclusion {
		case "success", "neutral", "skipped":
			// acceptable outcomes
		default:
			return false
		}
	}
	return true
}
