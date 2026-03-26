package deliverypipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const lockFileName = "delivery.lock"
const coderoDir = ".codero"

// LockMeta is the JSON content written to the lock file.
type LockMeta struct {
	SessionID    string    `json:"session_id"`
	AssignmentID string    `json:"assignment_id"`
	LockedAt     time.Time `json:"locked_at"`
}

// lockPath returns the canonical lock file path for a worktree.
func lockPath(worktree string) string {
	return filepath.Join(worktree, coderoDir, lockFileName)
}

// Lock creates <worktree>/.codero/delivery.lock with JSON metadata.
// Creates .codero/ directory if needed. Returns error if lock already exists.
func Lock(worktree, sessionID, assignmentID string) error {
	dir := filepath.Join(worktree, coderoDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("delivery: create lock dir: %w", err)
	}

	meta := LockMeta{
		SessionID:    sessionID,
		AssignmentID: assignmentID,
		LockedAt:     time.Now().UTC(),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("delivery: marshal lock meta: %w", err)
	}

	f, err := os.OpenFile(lockPath(worktree), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("delivery: lock already held")
		}
		return fmt.Errorf("delivery: create lock file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("delivery: write lock file: %w", err)
	}
	return nil
}

// IsLocked returns true if the lock file exists at <worktree>/.codero/delivery.lock.
func IsLocked(worktree string) bool {
	_, err := os.Stat(lockPath(worktree))
	return err == nil
}

// ReadLock reads and returns the lock metadata. Returns error if no lock.
func ReadLock(worktree string) (*LockMeta, error) {
	data, err := os.ReadFile(lockPath(worktree))
	if err != nil {
		return nil, fmt.Errorf("delivery: read lock: %w", err)
	}
	var meta LockMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("delivery: unmarshal lock: %w", err)
	}
	return &meta, nil
}

// Unlock removes the lock file. Returns nil if lock doesn't exist.
func Unlock(worktree string) error {
	err := os.Remove(lockPath(worktree))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delivery: remove lock: %w", err)
	}
	return nil
}

// CheckTimeout removes the lock if it's older than the given timeout duration.
// Returns true if a stale lock was removed. Returns false if no lock or lock is fresh.
func CheckTimeout(worktree string, timeout time.Duration) (bool, error) {
	meta, err := ReadLock(worktree)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		// Distinguish genuine read/parse errors from "no lock file".
		if _, statErr := os.Stat(lockPath(worktree)); os.IsNotExist(statErr) {
			return false, nil
		}
		return false, err
	}

	if time.Since(meta.LockedAt) <= timeout {
		return false, nil
	}

	if err := Unlock(worktree); err != nil {
		return false, err
	}
	return true, nil
}
