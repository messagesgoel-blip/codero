package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteSentinel creates the ready sentinel file at path.
// The file is created atomically using O_EXCL to prevent races.
// An existing sentinel from a stale daemon is removed automatically.
// The sentinel contains the creation timestamp for diagnostics.
func WriteSentinel(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sentinel: mkdir %s: %w", filepath.Dir(path), err)
	}

	// Remove stale sentinel from a previous unclean exit.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("sentinel: remove stale %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("sentinel: create %s: %w", path, err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("sentinel: write %s: %w", path, err)
	}
	return nil
}

// RemoveSentinel deletes the ready sentinel file. Called on clean shutdown.
// Idempotent: returns nil if the file does not exist.
func RemoveSentinel(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("sentinel: remove %s: %w", path, err)
}

// SentinelExists reports whether the ready sentinel file exists.
func SentinelExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
