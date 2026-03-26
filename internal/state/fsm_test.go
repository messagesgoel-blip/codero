package state

import (
	"context"
	"strings"
	"testing"
)

func TestBranchFSM_AllValidTransitions(t *testing.T) {
	for from, allowed := range transitions {
		for to := range allowed {
			name := string(from) + "_to_" + string(to)
			t.Run(name, func(t *testing.T) {
				event := branchTransitionEventName(from, to)
				if event == "" {
					t.Fatalf("missing event for %s -> %s", from, to)
				}
				f := NewBranchFSM(from)
				if !f.Can(event) {
					t.Fatalf("fsm cannot transition %s -> %s", from, to)
				}
				if err := f.Event(context.Background(), event); err != nil {
					t.Fatalf("fsm event failed %s -> %s: %v", from, to, err)
				}
			})
		}
	}
}

func TestBranchFSM_InvalidTransitions(t *testing.T) {
	invalid := []struct {
		from State
		to   State
	}{
		{StateMerged, StateSubmitted},
		{StateSubmitted, StateReviewApproved},
		{StateExpired, StateReviewApproved},
		{StateStale, StateReviewApproved},
	}

	for _, tt := range invalid {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			if branchTransitionEventName(tt.from, tt.to) != "" {
				t.Fatalf("unexpected event for %s -> %s", tt.from, tt.to)
			}
			if err := ValidateTransition(tt.from, tt.to); err == nil {
				t.Fatalf("expected invalid transition for %s -> %s", tt.from, tt.to)
			}
		})
	}
}

func TestBranchFSM_Visualize(t *testing.T) {
	f := NewBranchFSM(StateSubmitted)
	dot := f.Visualize()
	if dot == "" {
		t.Fatal("expected DOT output")
	}
	if !strings.Contains(dot, "digraph BranchFSM") {
		t.Fatalf("DOT missing header: %s", dot)
	}
	if !strings.Contains(dot, "submitted -> waiting") {
		t.Fatalf("DOT missing edge: %s", dot)
	}
}

func TestAssignmentFSM_Transitions(t *testing.T) {
	valid := []struct {
		from assignmentLifecycleState
		to   assignmentLifecycleState
	}{
		{assignmentStateActive, assignmentStateBlocked},
		{assignmentStateActive, assignmentStateCompleted},
		{assignmentStateActive, assignmentStateCancelled},
		{assignmentStateActive, assignmentStateSuperseded},
		{assignmentStateActive, assignmentStateLost},
		{assignmentStateBlocked, assignmentStateActive},
		{assignmentStateBlocked, assignmentStateCompleted},
		{assignmentStateBlocked, assignmentStateCancelled},
		{assignmentStateBlocked, assignmentStateSuperseded},
		{assignmentStateBlocked, assignmentStateLost},
	}

	for _, tt := range valid {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			if err := ValidateAssignmentStateTransition(tt.from, tt.to); err != nil {
				t.Fatalf("expected valid transition %s -> %s: %v", tt.from, tt.to, err)
			}
		})
	}

	invalid := []struct {
		from assignmentLifecycleState
		to   assignmentLifecycleState
	}{
		{assignmentStateCompleted, assignmentStateActive},
		{assignmentStateCancelled, assignmentStateBlocked},
		{assignmentStateLost, assignmentStateActive},
	}

	for _, tt := range invalid {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			if err := ValidateAssignmentStateTransition(tt.from, tt.to); err == nil {
				t.Fatalf("expected invalid transition %s -> %s", tt.from, tt.to)
			}
		})
	}
}

func TestAssignmentFSM_SubstatusValidation(t *testing.T) {
	tests := []struct {
		state     assignmentLifecycleState
		substatus string
		valid     bool
	}{
		{assignmentStateActive, "", true},
		{assignmentStateActive, AssignmentSubstatusInProgress, true},
		{assignmentStateActive, AssignmentSubstatusNeedsRevision, true},
		{assignmentStateActive, AssignmentSubstatusWaitingForCI, true},
		{assignmentStateActive, AssignmentSubstatusWaitingForMergeApproval, true},
		{assignmentStateActive, AssignmentSubstatusBlockedMergeConflict, false},

		{assignmentStateBlocked, AssignmentSubstatusBlockedMergeConflict, true},
		{assignmentStateBlocked, AssignmentSubstatusInProgress, false},

		{assignmentStateCompleted, AssignmentSubstatusTerminalFinished, true},
		{assignmentStateCompleted, AssignmentSubstatusTerminalWaitingComments, true},
		{assignmentStateCompleted, AssignmentSubstatusTerminalLost, false},

		{assignmentStateCancelled, AssignmentSubstatusTerminalCancelled, true},
		{assignmentStateCancelled, AssignmentSubstatusTerminalFinished, false},

		{assignmentStateSuperseded, AssignmentSubstatusTerminalWaitingNextTask, true},
		{assignmentStateSuperseded, AssignmentSubstatusTerminalCancelled, false},

		{assignmentStateLost, AssignmentSubstatusTerminalLost, true},
		{assignmentStateLost, AssignmentSubstatusTerminalStuckAbandoned, true},
		{assignmentStateLost, AssignmentSubstatusTerminalFinished, false},
	}

	for _, tt := range tests {
		name := string(tt.state) + "_" + tt.substatus
		t.Run(name, func(t *testing.T) {
			err := ValidateAssignmentSubstatus(tt.state, tt.substatus)
			if tt.valid && err != nil {
				t.Fatalf("expected valid substatus %s for %s: %v", tt.substatus, tt.state, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("expected invalid substatus %s for %s", tt.substatus, tt.state)
			}
		})
	}
}
