package session

import "testing"

// TestCert_SLv1_AllCheckpoints_Count verifies §1.1: exactly 19 lifecycle
// checkpoints are defined, matching the spec table.
//
// Matrix clause: §1.1 | Evidence: UT
func TestCert_SLv1_AllCheckpoints_Count(t *testing.T) {
	cps := AllCheckpoints()
	if len(cps) != 19 {
		t.Fatalf("AllCheckpoints: got %d, want 19", len(cps))
	}
}

// TestCert_SLv1_AllCheckpoints_Names verifies each checkpoint matches the
// spec-defined name exactly.
//
// Matrix clause: §1.1 | Evidence: UT
func TestCert_SLv1_AllCheckpoints_Names(t *testing.T) {
	expected := []string{
		"LAUNCHED", "REGISTERED", "TASK_ASSIGNED", "CODING", "SUBMITTED",
		"GATING", "GATE_PASSED", "GATE_FAILED", "COMMITTED", "PUSHED",
		"PR_ACTIVE", "MONITORING", "FEEDBACK_DELIVERED", "REVISING",
		"MERGE_READY", "MERGED", "NEXT_TASK", "SESSION_CLOSING", "ARCHIVED",
	}
	cps := AllCheckpoints()
	for i, cp := range cps {
		if string(cp) != expected[i] {
			t.Errorf("checkpoint[%d]: got %q, want %q", i, cp, expected[i])
		}
	}
}

// TestCert_SLv1_CheckpointTerminal verifies only ARCHIVED is terminal.
func TestCert_SLv1_CheckpointTerminal(t *testing.T) {
	for _, cp := range AllCheckpoints() {
		isT := cp.IsTerminal()
		if cp == CheckpointArchived && !isT {
			t.Errorf("ARCHIVED should be terminal")
		}
		if cp != CheckpointArchived && isT {
			t.Errorf("%s should not be terminal", cp)
		}
	}
}

// TestCert_SLv1_ValidCheckpoint verifies ValidCheckpoint accepts all defined
// checkpoints and rejects invalid ones.
func TestCert_SLv1_ValidCheckpoint(t *testing.T) {
	for _, cp := range AllCheckpoints() {
		if !ValidCheckpoint(string(cp)) {
			t.Errorf("ValidCheckpoint(%q) = false, want true", cp)
		}
	}
	if ValidCheckpoint("INVALID") {
		t.Error("ValidCheckpoint(INVALID) = true, want false")
	}
	if ValidCheckpoint("") {
		t.Error("ValidCheckpoint(\"\") = true, want false")
	}
}
