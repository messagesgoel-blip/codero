package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSentinel_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codero.ready")
	if err := WriteSentinel(path); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sentinel file not created: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("sentinel file is empty — expected timestamp")
	}
}

func TestWriteSentinel_OverwritesStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codero.ready")

	// Write a stale sentinel.
	if err := os.WriteFile(path, []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// WriteSentinel should remove the stale file and create a new one.
	if err := WriteSentinel(path); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if string(data) == "stale\n" {
		t.Fatal("sentinel was not overwritten — still contains stale data")
	}
}

func TestWriteSentinel_CreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "codero.ready")
	if err := WriteSentinel(path); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if !SentinelExists(path) {
		t.Fatal("sentinel should exist after write")
	}
}

func TestRemoveSentinel_DeletesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codero.ready")
	if err := os.WriteFile(path, []byte("ready\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := RemoveSentinel(path); err != nil {
		t.Fatalf("RemoveSentinel: %v", err)
	}
	if SentinelExists(path) {
		t.Fatal("sentinel should not exist after RemoveSentinel")
	}
}

func TestRemoveSentinel_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.ready")

	if err := RemoveSentinel(path); err != nil {
		t.Fatalf("RemoveSentinel on non-existent file should be nil; got: %v", err)
	}
}

func TestSentinelExists(t *testing.T) {
	dir := t.TempDir()

	present := filepath.Join(dir, "present.ready")
	absent := filepath.Join(dir, "absent.ready")

	if err := os.WriteFile(present, []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if !SentinelExists(present) {
		t.Error("SentinelExists should return true for existing file")
	}
	if SentinelExists(absent) {
		t.Error("SentinelExists should return false for missing file")
	}
}
