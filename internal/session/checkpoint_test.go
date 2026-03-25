package session

import "testing"

func TestAllCheckpoints(t *testing.T) {
	cps := AllCheckpoints()
	if len(cps) != 19 {
		t.Fatalf("expected 19 checkpoints, got %d", len(cps))
	}
	// First should be LAUNCHED, last should be ARCHIVED
	if cps[0] != CheckpointLaunched {
		t.Errorf("first checkpoint = %q, want LAUNCHED", cps[0])
	}
	if cps[len(cps)-1] != CheckpointArchived {
		t.Errorf("last checkpoint = %q, want ARCHIVED", cps[len(cps)-1])
	}
}

func TestCheckpointIsTerminal(t *testing.T) {
	if !CheckpointArchived.IsTerminal() {
		t.Error("ARCHIVED should be terminal")
	}
	if CheckpointCoding.IsTerminal() {
		t.Error("CODING should not be terminal")
	}
}

func TestValidCheckpoint(t *testing.T) {
	if !ValidCheckpoint("LAUNCHED") {
		t.Error("LAUNCHED should be valid")
	}
	if ValidCheckpoint("INVALID") {
		t.Error("INVALID should not be valid")
	}
}

func TestCheckpointString(t *testing.T) {
	if CheckpointMerged.String() != "MERGED" {
		t.Errorf("String() = %q, want MERGED", CheckpointMerged.String())
	}
}
