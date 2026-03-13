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
		{"T02: coding -> local_review", StateCoding, StateLocalReview},
		{"T05: coding -> queued_cli", StateCoding, StateQueuedCLI},
		{"T03: local_review -> coding", StateLocalReview, StateCoding},
		{"T04: local_review -> queued_cli", StateLocalReview, StateQueuedCLI},
		{"T06: queued_cli -> cli_reviewing", StateQueuedCLI, StateCLIReviewing},
		{"T19: queued_cli -> paused", StateQueuedCLI, StatePaused},
		{"T07: cli_reviewing -> queued_cli", StateCLIReviewing, StateQueuedCLI},
		{"T08: cli_reviewing -> reviewed", StateCLIReviewing, StateReviewed},
		{"T09: reviewed -> coding", StateReviewed, StateCoding},
		{"T10: reviewed -> merge_ready", StateReviewed, StateMergeReady},
		{"T11: merge_ready -> coding", StateMergeReady, StateCoding},
		{"T12: merge_ready -> stale_branch", StateMergeReady, StateStaleBranch},
		{"T13: stale_branch -> queued_cli", StateStaleBranch, StateQueuedCLI},
		{"T14: coding -> abandoned", StateCoding, StateAbandoned},
		{"T15: abandoned -> queued_cli", StateAbandoned, StateQueuedCLI},
		{"T16: coding -> blocked", StateCoding, StateBlocked},
		{"T17: blocked -> queued_cli", StateBlocked, StateQueuedCLI},
		{"T18: coding -> closed", StateCoding, StateClosed},
		{"T20: paused -> queued_cli", StatePaused, StateQueuedCLI},
		{"T18: stale_branch -> closed", StateStaleBranch, StateClosed},
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
		{"coding -> reviewed", StateCoding, StateReviewed},
		{"merge_ready -> cli_reviewing", StateMergeReady, StateCLIReviewing},
		{"closed -> coding", StateClosed, StateCoding},
		{"paused -> cli_reviewing", StatePaused, StateCLIReviewing},
		{"stale_branch -> reviewed", StateStaleBranch, StateReviewed},
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
	if !IsTerminal(StateClosed) {
		t.Errorf("expected IsTerminal(StateClosed) to be true")
	}
	if IsTerminal(StateCoding) {
		t.Errorf("expected IsTerminal(StateCoding) to be false")
	}
}
