package deliverypipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotify_ExecutesHook(t *testing.T) {
	worktree := t.TempDir()
	hookDir := filepath.Join(worktree, coderoDir, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	marker := filepath.Join(worktree, coderoDir, feedbackDirName, "hook-ran")
	script := "#!/bin/sh\nmkdir -p \"" + filepath.Join(worktree, coderoDir, feedbackDirName) + "\"\necho ok > \"" + marker + "\"\n"
	hook := filepath.Join(hookDir, "on-feedback")
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	Notify(worktree, "feedback", "assign-1")

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected hook marker: %v", err)
	}
}

func TestNotify_FallbackPending(t *testing.T) {
	worktree := t.TempDir()
	Notify(worktree, "feedback", "assign-2")

	pending := filepath.Join(worktree, coderoDir, feedbackDirName, "pending")
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("expected pending marker: %v", err)
	}
}

func TestNotify_Timeout(t *testing.T) {
	t.Setenv("CODERO_NOTIFICATION_HOOK_TIMEOUT", "1s")
	worktree := t.TempDir()
	hookDir := filepath.Join(worktree, coderoDir, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	hook := filepath.Join(hookDir, "on-feedback")
	script := "#!/bin/sh\nsleep 3\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	start := time.Now()
	Notify(worktree, "feedback", "assign-3")
	if time.Since(start) > 2*time.Second {
		t.Fatalf("expected timeout to return quickly")
	}
}

func TestNotify_HookFailure(t *testing.T) {
	worktree := t.TempDir()
	hookDir := filepath.Join(worktree, coderoDir, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	hook := filepath.Join(hookDir, "on-feedback")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	Notify(worktree, "feedback", "assign-4")
}
