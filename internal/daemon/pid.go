package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// WritePID writes the current process PID to path.
// Creates parent directories if needed. Fails if the file already exists
// and the PID it contains belongs to a running process (stale PID check).
func WritePID(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("pid: mkdir %s: %w", filepath.Dir(path), err)
	}

	// Check for existing PID file.
	switch existing, err := ReadPID(path); {
	case errors.Is(err, os.ErrNotExist):
		// No existing PID file — proceed normally.
	case err != nil:
		// Unreadable or malformed PID file; propagate rather than masking
		// as a generic O_EXCL "file already exists" error.
		return fmt.Errorf("pid: read existing PID file: %w", err)
	case ProcessRunning(existing):
		return fmt.Errorf("pid: daemon already running (pid %d)", existing)
	default:
		// Stale file (process not running) — remove before creating new one.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("pid: remove stale PID file %s: %w", path, err)
		}
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("pid: create %s: %w", path, err)
	}
	defer f.Close()

	if _, err = fmt.Fprintln(f, os.Getpid()); err != nil {
		return fmt.Errorf("pid: write %s: %w", path, err)
	}
	return nil
}

// RemovePID deletes the PID file. Called on clean shutdown.
func RemovePID(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ReadPID reads and returns the PID from the file.
// Returns 0 and an error if the file does not exist or is malformed.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("pid: malformed PID file %s: %w", path, err)
	}
	return pid, nil
}

// ProcessRunning reports whether a process with the given PID exists and is
// alive (kill -0 check).
//
// PID 0 and negative PIDs target process groups on Unix, not individual
// processes. They are rejected to avoid unintended group signalling.
//
// EPERM means the process exists but we lack permission to signal it — the
// process is running. Only ESRCH (wrapped as os.ErrProcessDone) indicates a
// truly absent process.
func ProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
