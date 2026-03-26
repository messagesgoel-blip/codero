package state

import (
	"fmt"

	"github.com/looplab/fsm"
)

// AssignmentFSM wraps looplab/fsm for assignment lifecycle validation.
type AssignmentFSM struct {
	*fsm.FSM
}

// NewAssignmentFSM creates an assignment FSM starting from the provided state.
func NewAssignmentFSM(initial assignmentLifecycleState) *AssignmentFSM {
	if initial == "" {
		initial = assignmentStateActive
	}
	return &AssignmentFSM{
		FSM: fsm.NewFSM(
			string(initial),
			buildAssignmentFSMEvents(),
			fsm.Callbacks{},
		),
	}
}

func buildAssignmentFSMEvents() fsm.Events {
	return fsm.Events{
		{Name: assignmentEventName(assignmentStateActive, assignmentStateBlocked), Src: []string{string(assignmentStateActive)}, Dst: string(assignmentStateBlocked)},
		{Name: assignmentEventName(assignmentStateActive, assignmentStateCompleted), Src: []string{string(assignmentStateActive)}, Dst: string(assignmentStateCompleted)},
		{Name: assignmentEventName(assignmentStateActive, assignmentStateCancelled), Src: []string{string(assignmentStateActive)}, Dst: string(assignmentStateCancelled)},
		{Name: assignmentEventName(assignmentStateActive, assignmentStateSuperseded), Src: []string{string(assignmentStateActive)}, Dst: string(assignmentStateSuperseded)},
		{Name: assignmentEventName(assignmentStateActive, assignmentStateLost), Src: []string{string(assignmentStateActive)}, Dst: string(assignmentStateLost)},

		{Name: assignmentEventName(assignmentStateBlocked, assignmentStateActive), Src: []string{string(assignmentStateBlocked)}, Dst: string(assignmentStateActive)},
		{Name: assignmentEventName(assignmentStateBlocked, assignmentStateCompleted), Src: []string{string(assignmentStateBlocked)}, Dst: string(assignmentStateCompleted)},
		{Name: assignmentEventName(assignmentStateBlocked, assignmentStateCancelled), Src: []string{string(assignmentStateBlocked)}, Dst: string(assignmentStateCancelled)},
		{Name: assignmentEventName(assignmentStateBlocked, assignmentStateSuperseded), Src: []string{string(assignmentStateBlocked)}, Dst: string(assignmentStateSuperseded)},
		{Name: assignmentEventName(assignmentStateBlocked, assignmentStateLost), Src: []string{string(assignmentStateBlocked)}, Dst: string(assignmentStateLost)},
	}
}

func assignmentEventName(from, to assignmentLifecycleState) string {
	return fmt.Sprintf("transition_%s_to_%s", from, to)
}

// ValidateAssignmentStateTransition checks if a transition is valid.
func ValidateAssignmentStateTransition(from, to assignmentLifecycleState) error {
	f := NewAssignmentFSM(from)
	event := assignmentEventName(from, to)
	if !f.Can(event) {
		return fmt.Errorf("%w: %q -> %q", ErrInvalidAssignmentSubstatus, from, to)
	}
	return nil
}

// ValidateAssignmentSubstatus ensures substatus aligns with assignment state.
func ValidateAssignmentSubstatus(state assignmentLifecycleState, substatus string) error {
	normalized := normalizeAssignmentSubstatus(substatus)

	switch state {
	case assignmentStateActive:
		if normalized == "" {
			return nil
		}
		if _, ok := activeAssignmentSubstatusSet[normalized]; ok {
			return nil
		}
	case assignmentStateBlocked:
		if _, ok := blockedAssignmentSubstatusSet[normalized]; ok {
			return nil
		}
	case assignmentStateCompleted:
		if _, ok := completedAssignmentSubstatusSet[normalized]; ok {
			return nil
		}
	case assignmentStateCancelled:
		if normalized == AssignmentSubstatusTerminalCancelled {
			return nil
		}
	case assignmentStateSuperseded:
		if normalized == AssignmentSubstatusTerminalWaitingNextTask {
			return nil
		}
	case assignmentStateLost:
		if normalized == AssignmentSubstatusTerminalLost || normalized == AssignmentSubstatusTerminalStuckAbandoned {
			return nil
		}
	}

	return fmt.Errorf("%w: %q for state %q", ErrInvalidAssignmentSubstatus, normalized, state)
}

// Visualize returns a DOT representation of the assignment FSM.
func (f *AssignmentFSM) Visualize() string {
	return `digraph AssignmentFSM {
rankdir=LR;
node [shape=circle];

active -> blocked;
active -> completed;
active -> cancelled;
active -> superseded;
active -> lost;

blocked -> active;
blocked -> completed;
blocked -> cancelled;
blocked -> superseded;
blocked -> lost;

completed [style=filled, fillcolor=lightgray];
cancelled [style=filled, fillcolor=lightgray];
superseded [style=filled, fillcolor=lightgray];
lost [style=filled, fillcolor=lightgray];
}`
}
