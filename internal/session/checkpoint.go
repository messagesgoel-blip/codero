package session

// Checkpoint represents a named stage in the session lifecycle.
// Every session passes through a defined set of checkpoints; each checkpoint
// is observable in the TUI and dashboard.
//
// Spec reference: Session Lifecycle v1 §1.1 — Lifecycle checkpoints.
type Checkpoint string

const (
	// CheckpointLaunched: launcher process starts, mints session_id.
	CheckpointLaunched Checkpoint = "LAUNCHED"
	// CheckpointRegistered: daemon ACKs session, agent prompt appears.
	CheckpointRegistered Checkpoint = "REGISTERED"
	// CheckpointTaskAssigned: task bound to session (user, orchestrator, or Codero).
	CheckpointTaskAssigned Checkpoint = "TASK_ASSIGNED"
	// CheckpointCoding: agent is writing/modifying code.
	CheckpointCoding Checkpoint = "CODING"
	// CheckpointSubmitted: agent signals code is ready via codero submit.
	CheckpointSubmitted Checkpoint = "SUBMITTED"
	// CheckpointGating: Codero runs pre-commit gate checks.
	CheckpointGating Checkpoint = "GATING"
	// CheckpointGatePassed: all hard gates passed.
	CheckpointGatePassed Checkpoint = "GATE_PASSED"
	// CheckpointGateFailed: hard gate findings; feedback pushed to agent.
	CheckpointGateFailed Checkpoint = "GATE_FAILED"
	// CheckpointCommitted: Codero executed git commit.
	CheckpointCommitted Checkpoint = "COMMITTED"
	// CheckpointPushed: Codero executed git push.
	CheckpointPushed Checkpoint = "PUSHED"
	// CheckpointPRActive: PR created/updated, review triggers fired.
	CheckpointPRActive Checkpoint = "PR_ACTIVE"
	// CheckpointMonitoring: Codero monitoring CI, review, CodeRabbit.
	CheckpointMonitoring Checkpoint = "MONITORING"
	// CheckpointFeedbackDelivered: actionable feedback pushed to agent.
	CheckpointFeedbackDelivered Checkpoint = "FEEDBACK_DELIVERED"
	// CheckpointRevising: agent revising code after feedback.
	CheckpointRevising Checkpoint = "REVISING"
	// CheckpointMergeReady: all merge-readiness conditions met.
	CheckpointMergeReady Checkpoint = "MERGE_READY"
	// CheckpointMerged: PR merged successfully.
	CheckpointMerged Checkpoint = "MERGED"
	// CheckpointNextTask: new task dispatched or session standing by.
	CheckpointNextTask Checkpoint = "NEXT_TASK"
	// CheckpointSessionClosing: agent or launcher signaling session end.
	CheckpointSessionClosing Checkpoint = "SESSION_CLOSING"
	// CheckpointArchived: archive record written, session finalized.
	CheckpointArchived Checkpoint = "ARCHIVED"
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
