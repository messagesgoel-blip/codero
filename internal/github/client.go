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
	"net/url"
	"strings"
	"time"

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
	headFilter := branch
	if !strings.Contains(branch, ":") {
		if owner, _, found := strings.Cut(repo, "/"); found {
			headFilter = owner + ":" + branch
		}
	}

	q := url.Values{}
	q.Set("state", "open")
	q.Set("head", headFilter)
	q.Set("per_page", "1")
	endpoint := fmt.Sprintf("%s/repos/%s/pulls?%s", c.apiURL, repo, q.Encode())

	var pulls []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := c.getJSON(ctx, endpoint, &pulls); err != nil {
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

// ListPRReviews returns all reviews submitted on a pull request, following
// GitHub pagination until no further pages remain.
func (c *Client) ListPRReviews(ctx context.Context, repo string, prNumber int) ([]Review, error) {
	nextURL := fmt.Sprintf("%s/repos/%s/pulls/%d/reviews?per_page=100", c.apiURL, repo, prNumber)
	var all []Review
	for nextURL != "" {
		var raw []struct {
			ID   int64 `json:"id"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
			State string `json:"state"`
			Body  string `json:"body"`
		}
		headers, err := c.getJSONPage(ctx, nextURL, &raw)
		if err != nil {
			return nil, fmt.Errorf("list PR reviews: %w", err)
		}
		for _, r := range raw {
			all = append(all, Review{ID: r.ID, User: r.User.Login, State: r.State, Body: r.Body})
		}
		nextURL = parseLinkNext(headers.Get("Link"))
	}
	return all, nil
}

// ListPRReviewComments returns all inline review comments on a pull request,
// following GitHub pagination until no further pages remain.
func (c *Client) ListPRReviewComments(ctx context.Context, repo string, prNumber int) ([]ReviewComment, error) {
	nextURL := fmt.Sprintf("%s/repos/%s/pulls/%d/comments?per_page=100", c.apiURL, repo, prNumber)
	var all []ReviewComment
	for nextURL != "" {
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
		headers, err := c.getJSONPage(ctx, nextURL, &raw)
		if err != nil {
			return nil, fmt.Errorf("list PR review comments: %w", err)
		}
		for _, r := range raw {
			all = append(all, ReviewComment{
				ID: r.ID, User: r.User.Login, Body: r.Body,
				Path: r.Path, Line: r.Line, CommitID: r.CommitID,
			})
		}
		nextURL = parseLinkNext(headers.Get("Link"))
	}
	return all, nil
}

// ListCheckRuns returns check runs for the given commit SHA, following GitHub
// pagination until no further pages remain.
func (c *Client) ListCheckRuns(ctx context.Context, repo, sha string) ([]CheckRun, error) {
	nextURL := fmt.Sprintf("%s/repos/%s/commits/%s/check-runs?per_page=100", c.apiURL, repo, sha)
	var all []CheckRun
	for nextURL != "" {
		var resp struct {
			CheckRuns []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		headers, err := c.getJSONPage(ctx, nextURL, &resp)
		if err != nil {
			return nil, fmt.Errorf("list check runs: %w", err)
		}
		for _, r := range resp.CheckRuns {
			all = append(all, CheckRun{Name: r.Name, Status: r.Status, Conclusion: r.Conclusion})
		}
		nextURL = parseLinkNext(headers.Get("Link"))
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
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/merge", c.apiURL, repo, prNumber)
	body := struct {
		SHA         string `json:"sha"`
		MergeMethod string `json:"merge_method"`
	}{
		SHA:         sha,
		MergeMethod: mergeMethod,
	}
	return c.putJSON(ctx, url, body)
}

// putJSON performs an authenticated PUT request with a JSON body and discards
// the response body on success.
func (c *Client) putJSON(ctx context.Context, url string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(string(b)))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http put: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusMethodNotAllowed:
		return fmt.Errorf("PR not mergeable (405)")
	case http.StatusConflict:
		return fmt.Errorf("merge conflict or SHA mismatch (409)")
	default:
		return fmt.Errorf("github API %s returned HTTP %d", url, resp.StatusCode)
	}
}

// getJSON performs an authenticated GET request and JSON-decodes the response.
func (c *Client) getJSON(ctx context.Context, url string, dst any) error {
	_, err := c.getJSONPage(ctx, url, dst)
	return err
}

// getJSONPage is like getJSON but also returns the response headers so callers
// can follow Link: rel="next" pagination.
func (c *Client) getJSONPage(ctx context.Context, url string, dst any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github API %s returned HTTP %d", url, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return resp.Header, nil
}

// parseLinkNext parses a GitHub Link response header and returns the URL for
// rel="next", or "" when no next page exists.
func parseLinkNext(header string) string {
	for _, part := range strings.Split(header, ",") {
		seg := strings.SplitN(strings.TrimSpace(part), ";", 2)
		if len(seg) != 2 {
			continue
		}
		if !strings.Contains(seg[1], `rel="next"`) {
			continue
		}
		u := strings.TrimSpace(seg[0])
		if len(u) >= 2 && u[0] == '<' && u[len(u)-1] == '>' {
			return u[1 : len(u)-1]
		}
	}
	return ""
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
