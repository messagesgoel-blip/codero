package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFakeCodero(t *testing.T, dir string, exitCode int, output string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-codero")
	script := "#!/bin/sh\n"
	if output != "" {
		script += fmt.Sprintf("echo '%s'\n", output)
	}
	script += fmt.Sprintf("exit %d\n", exitCode)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake codero: %v", err)
	}
	return path
}

func TestExecSubmit_Success(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeCodero(t, dir, 0, "Committed: abc1234")
	cfgPath := filepath.Join(dir, "codero.yml")
	result := ExecSubmit(context.Background(), bin, cfgPath, submitArgs{
		Worktree: dir,
		Repo:     "messagesgoel-blip/codero",
		Branch:   "feat/test",
		Title:    "test PR",
		Body:     "test body",
	})
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
	if !strings.Contains(result.Output, "Committed: abc1234") {
		t.Errorf("Output should contain mock output, got %q", result.Output)
	}
}

func TestExecSubmit_Rejection(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeCodero(t, dir, 1, "no changes to submit")
	cfgPath := filepath.Join(dir, "codero.yml")
	result := ExecSubmit(context.Background(), bin, cfgPath, submitArgs{
		Worktree: dir,
		Repo:     "messagesgoel-blip/codero",
		Branch:   "feat/test",
		Title:    "test PR",
		Body:     "",
	})
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestExecSubmit_Timeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-codero")
	script := "#!/bin/sh\nsleep 1\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake codero: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := ExecSubmit(ctx, path, filepath.Join(dir, "codero.yml"), submitArgs{
		Worktree: dir,
		Repo:     "messagesgoel-blip/codero",
		Branch:   "feat/test",
		Title:    "test PR",
		Body:     "",
	})
	if result.Error == "" {
		t.Error("expected error for timed-out command")
	}
}
