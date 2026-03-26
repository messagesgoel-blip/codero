package state

import (
	"errors"
	"fmt"
)

// State represents the canonical branch lifecycle state.
type State string

const (
	StateSubmitted      State = "submitted"
	StateWaiting        State = "waiting"
	StateQueuedCLI      State = "queued_cli"
	StateCLIReviewing   State = "cli_reviewing"
	StateReviewApproved State = "review_approved"
	StateMergeReady     State = "merge_ready"
	StateStale          State = "stale"
	StateAbandoned      State = "abandoned"
	StateBlocked        State = "blocked"
	StateExpired        State = "expired"
	StateMerged         State = "merged"
)

// ErrInvalidTransition is returned when an illegal state transition is attempted.
var ErrInvalidTransition = errors.New("invalid state transition")

// transitions defines all 20 valid transitions from the canonical state machine.
// Map key: source state; Map value: set of allowed destination states.
var transitions = map[State]map[State]bool{
	StateSubmitted: {
		StateWaiting:   true, // T02
		StateQueuedCLI: true, // T05
		StateStale:     true, // T12 (any active)
		StateAbandoned: true, // T14 (any active)
		StateBlocked:   true, // T16 (any active)
		StateMerged:    true, // T18 (any)
	},
	StateWaiting: {
		StateSubmitted: true, // T03
		StateQueuedCLI: true, // T04
		StateStale:     true, // T12 (any active)
		StateAbandoned: true, // T14 (any active)
		StateBlocked:   true, // T16 (any active)
		StateMerged:    true, // T18 (any)
	},
	StateQueuedCLI: {
		StateCLIReviewing: true, // T06
		StateExpired:      true, // T19
		StateStale:        true, // T12 (any active)
		StateAbandoned:    true, // T14 (any active)
		StateBlocked:      true, // T16 (any active)
		StateMerged:       true, // T18 (any)
	},
	StateCLIReviewing: {
		StateQueuedCLI:      true, // T07
		StateReviewApproved: true, // T08
		StateStale:          true, // T12 (any active)
		StateAbandoned:      true, // T14 (any active)
		StateBlocked:        true, // T16 (any active)
		StateMerged:         true, // T18 (any)
	},
	StateReviewApproved: {
		StateSubmitted:  true, // T09
		StateMergeReady: true, // T10
		StateStale:      true, // T12 (any active)
		StateAbandoned:  true, // T14 (any active)
		StateBlocked:    true, // T16 (any active)
		StateMerged:     true, // T18 (any)
	},
	StateMergeReady: {
		StateSubmitted: true, // T11
		StateStale:     true, // T12 (any active)
		StateAbandoned: true, // T14 (any active)
		StateBlocked:   true, // T16 (any active)
		StateMerged:    true, // T18 (any)
	},
	StateStale: {
		StateQueuedCLI: true, // T13
		StateMerged:    true, // T18 (any)
	},
	StateAbandoned: {
		StateQueuedCLI: true, // T15
		StateMerged:    true, // T18 (any)
	},
	StateBlocked: {
		StateQueuedCLI: true, // T17
		StateMerged:    true, // T18 (any)
	},
	StateExpired: {
		StateQueuedCLI: true, // T20
		StateMerged:    true, // T18 (any)
	},
	StateMerged: {}, // Terminal state, no outbound transitions
}

// ValidateTransition checks if a transition from "from" to "to" is legal.
// Returns nil if legal, or ErrInvalidTransition with details.
func ValidateTransition(from, to State) error {
	if allowed, ok := transitions[from]; ok {
		if allowed[to] {
			return nil
		}
	}
	return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
}

// ActiveStates contains all non-terminal states where a branch is actively
// progressing through the lifecycle. The SQL IN clauses in ListActiveBranches
// and ListExpiredSessions must be kept in sync with this list.
var ActiveStates = []State{
	StateSubmitted,
	StateWaiting,
	StateQueuedCLI,
	StateCLIReviewing,
	StateReviewApproved,
	StateMergeReady,
}

// IsTerminal reports whether the state is a terminal state (merged).
func IsTerminal(s State) bool {
	return s == StateMerged
}
