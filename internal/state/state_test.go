package state

import (
	"errors"
	"testing"
)

func TestValidateTransition_Valid(t *testing.T) {
	tests := []struct {
		name string
		from State
		to   State
	}{
		{"T02: submitted -> waiting", StateSubmitted, StateWaiting},
		{"T05: submitted -> queued_cli", StateSubmitted, StateQueuedCLI},
		{"T03: waiting -> submitted", StateWaiting, StateSubmitted},
		{"T04: waiting -> queued_cli", StateWaiting, StateQueuedCLI},
		{"T06: queued_cli -> cli_reviewing", StateQueuedCLI, StateCLIReviewing},
		{"T19: queued_cli -> expired", StateQueuedCLI, StateExpired},
		{"T07: cli_reviewing -> queued_cli", StateCLIReviewing, StateQueuedCLI},
		{"T08: cli_reviewing -> review_approved", StateCLIReviewing, StateReviewApproved},
		{"T09: review_approved -> submitted", StateReviewApproved, StateSubmitted},
		{"T10: review_approved -> merge_ready", StateReviewApproved, StateMergeReady},
		{"T11: merge_ready -> submitted", StateMergeReady, StateSubmitted},
		{"T12: merge_ready -> stale", StateMergeReady, StateStale},
		{"T13: stale -> queued_cli", StateStale, StateQueuedCLI},
		{"T14: submitted -> abandoned", StateSubmitted, StateAbandoned},
		{"T15: abandoned -> queued_cli", StateAbandoned, StateQueuedCLI},
		{"T16: submitted -> blocked", StateSubmitted, StateBlocked},
		{"T17: blocked -> queued_cli", StateBlocked, StateQueuedCLI},
		{"T18: submitted -> merged", StateSubmitted, StateMerged},
		{"T20: expired -> queued_cli", StateExpired, StateQueuedCLI},
		{"T18: stale -> merged", StateStale, StateMerged},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateTransition(tt.from, tt.to); err != nil {
				t.Errorf("ValidateTransition(%q, %q) unexpected error: %v", tt.from, tt.to, err)
			}
		})
	}
}

func TestValidateTransition_Invalid(t *testing.T) {
	tests := []struct {
		name string
		from State
		to   State
	}{
		{"submitted -> review_approved", StateSubmitted, StateReviewApproved},
		{"merge_ready -> cli_reviewing", StateMergeReady, StateCLIReviewing},
		{"merged -> submitted", StateMerged, StateSubmitted},
		{"expired -> cli_reviewing", StateExpired, StateCLIReviewing},
		{"stale -> review_approved", StateStale, StateReviewApproved},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransition(tt.from, tt.to)
			if err == nil {
				t.Errorf("ValidateTransition(%q, %q) expected error, got nil", tt.from, tt.to)
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("expected ErrInvalidTransition, got: %v", err)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	if !IsTerminal(StateMerged) {
		t.Errorf("expected IsTerminal(StateMerged) to be true")
	}
	if IsTerminal(StateSubmitted) {
		t.Errorf("expected IsTerminal(StateSubmitted) to be false")
	}
}
