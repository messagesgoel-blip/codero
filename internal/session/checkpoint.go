package session

// Checkpoint represents a named stage in the session lifecycle.
// Every session passes through a defined set of checkpoints; each checkpoint
// is observable in the TUI and dashboard.
//
// Spec reference: Session Lifecycle v1 §1.1 — Lifecycle checkpoints.
type Checkpoint string

const (
	CheckpointLaunched          Checkpoint = "LAUNCHED"
	CheckpointRegistered        Checkpoint = "REGISTERED"
	CheckpointTaskAssigned      Checkpoint = "TASK_ASSIGNED"
	CheckpointCoding            Checkpoint = "CODING"
	CheckpointSubmitted         Checkpoint = "SUBMITTED"
	CheckpointGating            Checkpoint = "GATING"
	CheckpointGatePassed        Checkpoint = "GATE_PASSED"
	CheckpointGateFailed        Checkpoint = "GATE_FAILED"
	CheckpointCommitted         Checkpoint = "COMMITTED"
	CheckpointPushed            Checkpoint = "PUSHED"
	CheckpointPRActive          Checkpoint = "PR_ACTIVE"
	CheckpointMonitoring        Checkpoint = "MONITORING"
	CheckpointFeedbackDelivered Checkpoint = "FEEDBACK_DELIVERED"
	CheckpointRevising          Checkpoint = "REVISING"
	CheckpointMergeReady        Checkpoint = "MERGE_READY"
	CheckpointMerged            Checkpoint = "MERGED"
	CheckpointNextTask          Checkpoint = "NEXT_TASK"
	CheckpointSessionClosing    Checkpoint = "SESSION_CLOSING"
	CheckpointArchived          Checkpoint = "ARCHIVED"
)

// AllCheckpoints returns all 19 lifecycle checkpoints in order.
func AllCheckpoints() []Checkpoint {
	return []Checkpoint{
		CheckpointLaunched,
		CheckpointRegistered,
		CheckpointTaskAssigned,
		CheckpointCoding,
		CheckpointSubmitted,
		CheckpointGating,
		CheckpointGatePassed,
		CheckpointGateFailed,
		CheckpointCommitted,
		CheckpointPushed,
		CheckpointPRActive,
		CheckpointMonitoring,
		CheckpointFeedbackDelivered,
		CheckpointRevising,
		CheckpointMergeReady,
		CheckpointMerged,
		CheckpointNextTask,
		CheckpointSessionClosing,
		CheckpointArchived,
	}
}

// IsTerminal returns true if the checkpoint represents a terminal session state.
func (c Checkpoint) IsTerminal() bool {
	return c == CheckpointArchived
}

// String returns the checkpoint as a string.
func (c Checkpoint) String() string {
	return string(c)
}

// ValidCheckpoint returns true if the given string is a valid lifecycle checkpoint.
func ValidCheckpoint(s string) bool {
	for _, cp := range AllCheckpoints() {
		if string(cp) == s {
			return true
		}
	}
	return false
}
