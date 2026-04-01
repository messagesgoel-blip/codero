package deliverypipeline

import (
	"os"
	"path/filepath"
	"strings"
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

func TestNotify_HookEnvIsMinimal(t *testing.T) {
	worktree := t.TempDir()
	hookDir := filepath.Join(worktree, coderoDir, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	hook := filepath.Join(hookDir, "on-feedback")
	script := "#!/bin/sh\nprintf '%s' \"$PATH\" > \"" + filepath.Join(worktree, coderoDir, "path") + "\"\nprintenv | sort > \"" + filepath.Join(worktree, coderoDir, "env") + "\"\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	t.Setenv("CODERO_DB_PATH", "secret")
	t.Setenv("GITHUB_TOKEN", "secret")
	t.Setenv("PATH", "/usr/bin:/bin")
	Notify(worktree, "feedback", "assign-5")

	envData, err := os.ReadFile(filepath.Join(worktree, coderoDir, "env"))
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	envText := string(envData)
	if strings.Contains(envText, "CODERO_DB_PATH=") || strings.Contains(envText, "GITHUB_TOKEN=") {
		t.Fatalf("hook env should not inherit Codero secrets: %s", envText)
	}
	if !strings.Contains(envText, "PATH=/usr/bin:/bin") {
		t.Fatalf("hook env should include PATH: %s", envText)
	}
}

func TestNotify_PassesTmuxNameAsArgument(t *testing.T) {
	worktree := t.TempDir()
	hookDir := filepath.Join(worktree, coderoDir, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	hook := filepath.Join(hookDir, "on-feedback")
	outPath := filepath.Join(worktree, coderoDir, "tmux-arg")
	script := "#!/bin/sh\nprintf '%s' \"$4\" > \"" + outPath + "\"\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}
	sessionMD := filepath.Join(worktree, coderoDir, "SESSION.md")
	if err := os.WriteFile(sessionMD, []byte("- CODERO_TMUX_NAME=codero-agent-12345678\n"), 0o644); err != nil {
		t.Fatalf("write session md: %v", err)
	}

	Notify(worktree, "feedback", "assign-6")

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read tmux arg: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "codero-agent-12345678" {
		t.Fatalf("tmux arg = %q, want %q", got, "codero-agent-12345678")
	}
}
