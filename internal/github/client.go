// Package github provides a GitHub REST API client for codero.
// It implements webhook.GitHubClient and exposes additional methods used by
// the review runner provider. All calls use net/http with Bearer token auth
// (same pattern as internal/config/scopes.go — no external SDK dependency).
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
		http:   http.DefaultClient,
		apiURL: defaultAPIURL,
	}
}

// WithHTTPClient returns a copy of Client using the given *http.Client.
// Intended for tests that need a custom transport (e.g. httptest server).
func (c *Client) WithHTTPClient(hc *http.Client) *Client {
	cp := *c
	cp.http = hc
	return &cp
}

// PRInfo contains the fields codero needs from a GitHub pull request.
type PRInfo struct {
	Number  int
	HeadSHA string
	State   string // "open" or "closed"
}

// Review represents a single GitHub pull request review.
type Review struct {
	ID    int64
	User  string
	State string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED"
	Body  string
}

// ReviewComment is an inline pull request review comment.
type ReviewComment struct {
	ID       int64
	User     string
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

	approved, changesRequested := resolveApprovalStatus(reviews)
	unresolvedThreads := 0
	if changesRequested {
		unresolvedThreads = 1
	}

	runs, err := c.ListCheckRuns(ctx, repo, pr.HeadSHA)
	if err != nil {
		return nil, fmt.Errorf("list check runs: %w", err)
	}

	return &webhook.GitHubState{
		Repo:              repo,
		Branch:            branch,
		HeadHash:          pr.HeadSHA,
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
	headFilter := branch
	if !strings.Contains(branch, ":") {
		if owner, _, found := strings.Cut(repo, "/"); found {
			headFilter = owner + ":" + branch
		}
	}

	url := fmt.Sprintf("%s/repos/%s/pulls?state=open&head=%s&per_page=1",
		c.apiURL, repo, headFilter)

	var pulls []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.getJSON(ctx, url, &pulls); err != nil {
		return nil, fmt.Errorf("find open PR: %w", err)
	}
	if len(pulls) == 0 {
		return nil, nil
	}
	p := pulls[0]
	return &PRInfo{
		Number:  p.Number,
		HeadSHA: p.Head.SHA,
		State:   p.State,
	}, nil
}

// ListPRReviews returns all reviews submitted on a pull request.
func (c *Client) ListPRReviews(ctx context.Context, repo string, prNumber int) ([]Review, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews?per_page=100", c.apiURL, repo, prNumber)

	var raw []struct {
		ID   int64 `json:"id"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		State string `json:"state"`
		Body  string `json:"body"`
	}
	if err := c.getJSON(ctx, url, &raw); err != nil {
		return nil, fmt.Errorf("list PR reviews: %w", err)
	}

	reviews := make([]Review, len(raw))
	for i, r := range raw {
		reviews[i] = Review{
			ID:    r.ID,
			User:  r.User.Login,
			State: r.State,
			Body:  r.Body,
		}
	}
	return reviews, nil
}

// ListPRReviewComments returns all inline review comments on a pull request.
func (c *Client) ListPRReviewComments(ctx context.Context, repo string, prNumber int) ([]ReviewComment, error) {
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/comments?per_page=100", c.apiURL, repo, prNumber)

	var raw []struct {
		ID   int64 `json:"id"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Body     string `json:"body"`
		Path     string `json:"path"`
		Line     int    `json:"line"`
		CommitID string `json:"commit_id"`
	}
	if err := c.getJSON(ctx, url, &raw); err != nil {
		return nil, fmt.Errorf("list PR review comments: %w", err)
	}

	comments := make([]ReviewComment, len(raw))
	for i, r := range raw {
		comments[i] = ReviewComment{
			ID:       r.ID,
			User:     r.User.Login,
			Body:     r.Body,
			Path:     r.Path,
			Line:     r.Line,
			CommitID: r.CommitID,
		}
	}
	return comments, nil
}

// ListCheckRuns returns check runs for the given commit SHA.
func (c *Client) ListCheckRuns(ctx context.Context, repo, sha string) ([]CheckRun, error) {
	url := fmt.Sprintf("%s/repos/%s/commits/%s/check-runs?per_page=100", c.apiURL, repo, sha)

	var resp struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if err := c.getJSON(ctx, url, &resp); err != nil {
		return nil, fmt.Errorf("list check runs: %w", err)
	}

	runs := make([]CheckRun, len(resp.CheckRuns))
	for i, r := range resp.CheckRuns {
		runs[i] = CheckRun{
			Name:       r.Name,
			Status:     r.Status,
			Conclusion: r.Conclusion,
		}
	}
	return runs, nil
}

// getJSON performs an authenticated GET request and JSON-decodes the response.
func (c *Client) getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github API %s returned HTTP %d", url, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// resolveApprovalStatus scans reviews in submission order.
// The latest non-COMMENTED state from each reviewer wins, matching GitHub's
// own "current review state" behaviour.
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
