package state

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/looplab/fsm"
)

// BranchFSM wraps looplab/fsm for branch lifecycle validation.
type BranchFSM struct {
	*fsm.FSM
}

// NewBranchFSM creates a branch FSM starting from the provided state.
func NewBranchFSM(initial State) *BranchFSM {
	if initial == "" {
		initial = StateCoding
	}
	return &BranchFSM{
		FSM: fsm.NewFSM(
			string(initial),
			buildBranchFSMEvents(),
			fsm.Callbacks{},
		),
	}
}

func buildBranchFSMEvents() fsm.Events {
	events := make(fsm.Events, 0, len(transitions))
	for from, allowed := range transitions {
		for to := range allowed {
			name := branchTransitionEventName(from, to)
			events = append(events, fsm.EventDesc{
				Name: name,
				Src:  []string{string(from)},
				Dst:  string(to),
			})
		}
	}
	return events
}

func branchTransitionEventName(from, to State) string {
	if allowed, ok := transitions[from]; ok && allowed[to] {
		return fmt.Sprintf("transition_%s_to_%s", from, to)
	}
	return ""
}

func validateBranchFSMTransition(from, to State) error {
	if err := ValidateTransition(from, to); err != nil {
		return err
	}
	eventName := branchTransitionEventName(from, to)
	if eventName == "" {
		return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
	}
	f := NewBranchFSM(from)
	if !f.Can(eventName) {
		return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
	}
	if err := f.Event(context.Background(), eventName); err != nil {
		return fmt.Errorf("%w: %q -> %q", ErrInvalidTransition, from, to)
	}
	return nil
}

// Visualize returns a DOT representation of the branch FSM.
func (f *BranchFSM) Visualize() string {
	var edges []string
	for from, allowed := range transitions {
		for to := range allowed {
			edges = append(edges, fmt.Sprintf("%s -> %s", from, to))
		}
	}
	sort.Strings(edges)

	var b strings.Builder
	b.WriteString("digraph BranchFSM {\nrankdir=LR;\nnode [shape=circle];\n\n")
	for _, edge := range edges {
		parts := strings.Split(edge, " -> ")
		if len(parts) != 2 {
			continue
		}
		b.WriteString(fmt.Sprintf("%s -> %s;\n", parts[0], parts[1]))
	}
	b.WriteString("}\n")
	return b.String()
}
