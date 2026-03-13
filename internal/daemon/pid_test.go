package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWritePID_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codero.pid")
	if err := WritePID(path); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("PID file not created: %v", err)
	}
}

func TestWritePID_FailsOnRunningProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codero.pid")

	// Write the current process's PID — which is definitely running.
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := WritePID(path)
	if err == nil {
		t.Fatal("expected error when daemon already running, got nil")
	}
}

func TestReadPID_ReturnsCorrectPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codero.pid")
	want := os.Getpid()
	if err := os.WriteFile(path, []byte(strconv.Itoa(want)+"\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	got, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if got != want {
		t.Errorf("ReadPID: got %d, want %d", got, want)
	}
}

func TestRemovePID_DeletesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codero.pid")
	if err := os.WriteFile(path, []byte("12345\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := RemovePID(path); err != nil {
		t.Fatalf("RemovePID: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("PID file should not exist after RemovePID")
	}
}
