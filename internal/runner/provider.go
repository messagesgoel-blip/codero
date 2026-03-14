// Package runner implements the PR review execution engine.
// It consumes queued_cli branches, acquires leases, runs the review provider,
// normalizes findings, delivers results to the feedback stream, and records
// deterministic state transitions.
package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/codero/codero/internal/normalizer"
)

// ReviewRequest is the input to a review provider.
type ReviewRequest struct {
	Repo     string
	Branch   string
	HeadHash string
}

// ReviewResponse is the raw output from a review provider.
type ReviewResponse struct {
	Findings []normalizer.RawFinding
}

// Provider is the interface all review backends must implement.
// Implementations must be safe for concurrent use.
type Provider interface {
	// Name returns a stable identifier for this provider (e.g., "stub", "coderabbit").
	Name() string

	// Review executes a review for the given request.
	// It must respect ctx cancellation and return promptly on cancellation.
	// Errors are treated as retriable failures; the runner increments retry_count.
	Review(ctx context.Context, req ReviewRequest) (*ReviewResponse, error)
}

// StubProvider is a no-op provider used in tests and polling-only mode.
// It returns a small set of deterministic canned findings.
type StubProvider struct {
	delay time.Duration // optional artificial delay for testing
}

// NewStubProvider creates a StubProvider. Pass 0 delay for immediate response.
func NewStubProvider(delay time.Duration) *StubProvider {
	return &StubProvider{delay: delay}
}

// Name implements Provider.
func (s *StubProvider) Name() string { return "stub" }

// Review implements Provider. Returns canned findings deterministically.
func (s *StubProvider) Review(ctx context.Context, req ReviewRequest) (*ReviewResponse, error) {
	if s.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("stub review cancelled: %w", ctx.Err())
		case <-time.After(s.delay):
		}
	}

	return &ReviewResponse{
		Findings: []normalizer.RawFinding{
			{
				Severity: "info",
				Category: "style",
				File:     "",
				Line:     0,
				Message:  fmt.Sprintf("stub review completed for %s/%s@%s", req.Repo, req.Branch, req.HeadHash),
				Source:   "stub",
				RuleID:   "STUB-001",
			},
		},
	}, nil
}
