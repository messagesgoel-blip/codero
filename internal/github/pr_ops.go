package github

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// PRAutoCreate returns true if CODERO_PR_AUTO_CREATE is "true" or unset (default true).
func PRAutoCreate() bool {
	v := os.Getenv("CODERO_PR_AUTO_CREATE")
	return v == "" || v == "true" || v == "1"
}

// CodeRabbitAutoReview returns true if CODERO_CODERABBIT_AUTO_REVIEW is "true" or unset (default true).
func CodeRabbitAutoReview() bool {
	v := os.Getenv("CODERO_CODERABBIT_AUTO_REVIEW")
	return v == "" || v == "true" || v == "1"
}

// CreatePRIfEnabled creates a PR only if CODERO_PR_AUTO_CREATE is enabled.
// Returns (prNumber, created, error). created is false if the flag is disabled.
func (c *Client) CreatePRIfEnabled(ctx context.Context, repo, head, base, title, body string) (int, bool, error) {
	if !PRAutoCreate() {
		return 0, false, nil
	}
	num, err := c.CreatePR(ctx, repo, head, base, title, body)
	if err != nil {
		// Check for 422 (PR already exists)
		if strings.Contains(err.Error(), "422") || strings.Contains(err.Error(), "already exists") {
			return 0, false, fmt.Errorf("PR already exists for %s → %s: %w", head, base, err)
		}
		return 0, false, err
	}
	return num, true, nil
}

// PostReviewComment posts a review-triggering comment on a PR.
// The standard CodeRabbit trigger is "@coderabbitai review".
func (c *Client) PostReviewComment(ctx context.Context, repo string, prNumber int, body string) error {
	return c.PostComment(ctx, repo, prNumber, body)
}

// TriggerCodeRabbitReview posts "@coderabbitai review" on a PR if
// CODERO_CODERABBIT_AUTO_REVIEW is enabled.
func (c *Client) TriggerCodeRabbitReview(ctx context.Context, repo string, prNumber int) error {
	if !CodeRabbitAutoReview() {
		return nil
	}
	return c.PostReviewComment(ctx, repo, prNumber, "@coderabbitai review")
}
