package state

import (
	"errors"
	"fmt"
)

// State represents the canonical branch lifecycle state.
type State string

const (
	StateCoding       State = "coding"
	StateLocalReview  State = "local_review"
	StateQueuedCLI    State = "queued_cli"
	StateCLIReviewing State = "cli_reviewing"
	StateReviewed     State = "reviewed"
	StateMergeReady   State = "merge_ready"
	StateStaleBranch  State = "stale_branch"
	StateAbandoned    State = "abandoned"
	StateBlocked      State = "blocked"
	StatePaused       State = "paused"
	StateClosed       State = "closed"
)

// ErrInvalidTransition is returned when an illegal state transition is attempted.
var ErrInvalidTransition = errors.New("invalid state transition")

// transitions defines all 20 valid transitions from the canonical state machine.
// Map key: source state; Map value: set of allowed destination states.
var transitions = map[State]map[State]bool{
	StateCoding: {
		StateLocalReview: true, // T02
		StateQueuedCLI:   true, // T05
		StateStaleBranch: true, // T12 (any active)
		StateAbandoned:   true, // T14 (any active)
		StateBlocked:     true, // T16 (any active)
		StateClosed:      true, // T18 (any)
	},
	StateLocalReview: {
		StateCoding:      true, // T03
		StateQueuedCLI:   true, // T04
		StateStaleBranch: true, // T12 (any active)
		StateAbandoned:   true, // T14 (any active)
		StateBlocked:     true, // T16 (any active)
		StateClosed:      true, // T18 (any)
	},
	StateQueuedCLI: {
		StateCLIReviewing: true, // T06
		StatePaused:       true, // T19
		StateStaleBranch:  true, // T12 (any active)
		StateAbandoned:    true, // T14 (any active)
		StateBlocked:      true, // T16 (any active)
		StateClosed:       true, // T18 (any)
	},
	StateCLIReviewing: {
		StateQueuedCLI:   true, // T07
		StateReviewed:    true, // T08
		StateStaleBranch: true, // T12 (any active)
		StateAbandoned:   true, // T14 (any active)
		StateBlocked:     true, // T16 (any active)
		StateClosed:      true, // T18 (any)
	},
	StateReviewed: {
		StateCoding:      true, // T09
		StateMergeReady:  true, // T10
		StateStaleBranch: true, // T12 (any active)
		StateAbandoned:   true, // T14 (any active)
		StateBlocked:     true, // T16 (any active)
		StateClosed:      true, // T18 (any)
	},
	StateMergeReady: {
		StateCoding:      true, // T11
		StateStaleBranch: true, // T12 (any active)
		StateAbandoned:   true, // T14 (any active)
		StateBlocked:     true, // T16 (any active)
		StateClosed:      true, // T18 (any)
	},
	StateStaleBranch: {
		StateQueuedCLI: true, // T13
		StateClosed:    true, // T18 (any)
	},
	StateAbandoned: {
		StateQueuedCLI: true, // T15
		StateClosed:    true, // T18 (any)
	},
	StateBlocked: {
		StateQueuedCLI: true, // T17
		StateClosed:    true, // T18 (any)
	},
	StatePaused: {
		StateQueuedCLI: true, // T20
		StateClosed:    true, // T18 (any)
	},
	StateClosed: {}, // Terminal state, no outbound transitions
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
	StateCoding,
	StateLocalReview,
	StateQueuedCLI,
	StateCLIReviewing,
	StateReviewed,
	StateMergeReady,
}

// IsTerminal reports whether the state is a terminal state (closed).
func IsTerminal(s State) bool {
	return s == StateClosed
}
