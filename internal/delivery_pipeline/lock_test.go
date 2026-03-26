package deliverypipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLock_Lifecycle(t *testing.T) {
	dir := t.TempDir()

	if err := Lock(dir, "sess-1", "assign-1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	if !IsLocked(dir) {
		t.Fatal("expected IsLocked to return true after Lock")
	}

	meta, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if meta.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", meta.SessionID, "sess-1")
	}
	if meta.AssignmentID != "assign-1" {
		t.Errorf("AssignmentID = %q, want %q", meta.AssignmentID, "assign-1")
	}
	if time.Since(meta.LockedAt) > 5*time.Second {
		t.Errorf("LockedAt too old: %v", meta.LockedAt)
	}

	if err := Unlock(dir); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if IsLocked(dir) {
		t.Fatal("expected IsLocked to return false after Unlock")
	}
}

func TestLock_AlreadyLocked(t *testing.T) {
	dir := t.TempDir()

	if err := Lock(dir, "sess-1", "assign-1"); err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if err := Lock(dir, "sess-2", "assign-2"); err == nil {
		t.Fatal("second Lock should return error, got nil")
	}
}

func TestUnlock_NotLocked(t *testing.T) {
	dir := t.TempDir()

	if err := Unlock(dir); err != nil {
		t.Fatalf("Unlock on unlocked dir: %v", err)
	}
}

func TestCheckTimeout_RemovesStale(t *testing.T) {
	dir := t.TempDir()

	// Write a lock file manually with an old timestamp.
	coderoPath := filepath.Join(dir, coderoDir)
	if err := os.MkdirAll(coderoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := LockMeta{
		SessionID:    "old-sess",
		AssignmentID: "old-assign",
		LockedAt:     time.Now().Add(-2 * time.Hour),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(dir), data, 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := CheckTimeout(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("CheckTimeout: %v", err)
	}
	if !removed {
		t.Fatal("expected stale lock to be removed")
	}
	if IsLocked(dir) {
		t.Fatal("lock should be gone after CheckTimeout removed it")
	}
}

func TestCheckTimeout_KeepsFresh(t *testing.T) {
	dir := t.TempDir()

	if err := Lock(dir, "sess-1", "assign-1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	removed, err := CheckTimeout(dir, 1*time.Hour)
	if err != nil {
		t.Fatalf("CheckTimeout: %v", err)
	}
	if removed {
		t.Fatal("expected fresh lock to be kept")
	}
	if !IsLocked(dir) {
		t.Fatal("lock should still exist")
	}
}

func TestLock_Concurrent(t *testing.T) {
	dir := t.TempDir()

	const goroutines = 10
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		wins     int
		errCount int
	)

	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			err := Lock(dir, "sess", "assign")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else {
				errCount++
			}
		}(i)
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("expected exactly 1 winner, got %d (errors: %d)", wins, errCount)
	}
	if errCount != goroutines-1 {
		t.Fatalf("expected %d errors, got %d", goroutines-1, errCount)
	}
}
