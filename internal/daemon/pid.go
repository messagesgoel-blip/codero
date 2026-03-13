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
	if existing, err := ReadPID(path); err == nil && existing != 0 {
		if ProcessRunning(existing) {
			return fmt.Errorf("pid: daemon already running (pid %d)", existing)
		}
		// Stale file — remove before writing.
		_ = os.Remove(path)
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
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("pid: remove %s: %w", path, err)
}

// ReadPID reads and returns the PID from the file.
// Returns 0 and an error if the file does not exist or is malformed.
// Rejects non-positive PID values.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("pid: read %s: %w", path, err)
	}
	s := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("pid: malformed PID file %s: %w", path, err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("pid: invalid PID value %d in %s", pid, path)
	}
	return pid, nil
}

// ProcessRunning returns true if a process with the given PID exists
// and is alive (kill -0). Returns true for EPERM (process exists but owned by
// another user). Returns false for non-positive PIDs.
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
