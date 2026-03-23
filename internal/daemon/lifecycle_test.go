package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestLifecycle_PIDBeforeSentinel verifies the spec invariant:
// PID file must exist before the ready sentinel is created.
func TestLifecycle_PIDBeforeSentinel(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "codero.pid")
	readyPath := filepath.Join(dir, "codero.ready")

	// Sentinel must not exist before PID.
	if SentinelExists(readyPath) {
		t.Fatal("ready sentinel should not exist before bootstrap")
	}

	if err := WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	// PID exists, sentinel still absent.
	pid, err := ReadPID(pidPath)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("PID mismatch: got %d, want %d", pid, os.Getpid())
	}
	if SentinelExists(readyPath) {
		t.Fatal("ready sentinel should not exist before WriteSentinel")
	}

	// Now write sentinel (step 8 in spec).
	if err := WriteSentinel(readyPath); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if !SentinelExists(readyPath) {
		t.Fatal("ready sentinel should exist after WriteSentinel")
	}
}

// TestLifecycle_CleanShutdownRemovesBoth verifies spec §4.1 step 8:
// On clean exit, both PID file and ready sentinel are removed.
func TestLifecycle_CleanShutdownRemovesBoth(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "codero.pid")
	readyPath := filepath.Join(dir, "codero.ready")

	if err := WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	if err := WriteSentinel(readyPath); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	// Simulate clean shutdown: remove sentinel first, then PID.
	if err := RemoveSentinel(readyPath); err != nil {
		t.Fatalf("RemoveSentinel: %v", err)
	}
	if err := RemovePID(pidPath); err != nil {
		t.Fatalf("RemovePID: %v", err)
	}

	if SentinelExists(readyPath) {
		t.Error("ready sentinel should be removed after clean shutdown")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file should be removed after clean shutdown")
	}
}

// TestLifecycle_StalePIDRecovery verifies spec §3 step 1:
// Stale PID from unclean exit is removed and overwritten.
func TestLifecycle_StalePIDRecovery(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "codero.pid")

	// Write a PID that does not belong to a running process.
	// PID 2147483647 is extremely unlikely to be running.
	stalePID := 2147483647
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)+"\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// WritePID should detect the stale PID and overwrite it.
	if err := WritePID(pidPath); err != nil {
		t.Fatalf("WritePID after stale: %v", err)
	}

	got, err := ReadPID(pidPath)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if got != os.Getpid() {
		t.Fatalf("PID after stale recovery: got %d, want %d", got, os.Getpid())
	}
}

// TestLifecycle_UncleanExitLeavesSentinels verifies spec §4.2:
// On unclean exit, PID and sentinel remain on disk for recovery.
func TestLifecycle_UncleanExitLeavesSentinels(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "codero.pid")
	readyPath := filepath.Join(dir, "codero.ready")

	if err := WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	if err := WriteSentinel(readyPath); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	// Simulate unclean exit: do NOT call RemovePID/RemoveSentinel.
	// Verify both files persist (for next startup recovery sweep).
	if _, err := os.Stat(pidPath); err != nil {
		t.Errorf("PID file should persist after unclean exit: %v", err)
	}
	if !SentinelExists(readyPath) {
		t.Error("ready sentinel should persist after unclean exit")
	}
}

// TestDegraded_SetAndClear verifies SetDegraded/IsDegraded round-trips.
func TestDegraded_SetAndClear(t *testing.T) {
	// Reset to known state.
	SetDegraded(false)
	if IsDegraded() {
		t.Fatal("should not be degraded after clearing")
	}

	SetDegraded(true)
	if !IsDegraded() {
		t.Fatal("should be degraded after setting")
	}

	SetDegraded(false)
	if IsDegraded() {
		t.Fatal("should not be degraded after re-clearing")
	}
}
