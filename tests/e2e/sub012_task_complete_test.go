//go:build e2e

// tests/e2e/sub012_task_complete_test.go
//
// SUB-012 E2E: Verify that TASK_COMPLETE signals in agent PTY output can be
// detected and parsed correctly via tmux capture.

package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSUB012_TaskCompleteObserver(t *testing.T) {
	if os.Getenv("CODERO_E2E_ENABLED") != "1" {
		t.Skip("CODERO_E2E_ENABLED != 1")
	}

	tmuxSession := "e2e-tc-test"
	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", tmuxSession).Run()
	})

	// 1. Create temp tmux session.
	if err := exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "bash").Run(); err != nil {
		t.Fatalf("create tmux session: %v", err)
	}

	// 2. Send TASK_COMPLETE with full summary block.
	sendKeys(t, tmuxSession, "echo 'TASK_COMPLETE'")
	sendKeys(t, tmuxSession, "echo 'pr_title: fix lint error'")
	sendKeys(t, tmuxSession, "echo 'change_summary: fixed unused import'")
	sendKeys(t, tmuxSession, "echo 'test_notes: ran go vet'")

	// 3. Wait for output to appear in tmux pane.
	var captured string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "capture-pane", "-t", tmuxSession, "-p").Output()
		if err == nil {
			captured = string(out)
			if strings.Contains(captured, "TASK_COMPLETE") &&
				strings.Contains(captured, "pr_title: fix lint error") {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !strings.Contains(captured, "TASK_COMPLETE") {
		t.Fatal("TASK_COMPLETE not found in tmux pane")
	}

	// 4. Validate the captured output can be parsed for summary fields.
	prTitle := extractField(captured, "pr_title")
	changeSummary := extractField(captured, "change_summary")
	testNotes := extractField(captured, "test_notes")

	if prTitle != "fix lint error" {
		t.Errorf("pr_title = %q, want %q", prTitle, "fix lint error")
	}
	if changeSummary != "fixed unused import" {
		t.Errorf("change_summary = %q, want %q", changeSummary, "fixed unused import")
	}
	if testNotes != "ran go vet" {
		t.Errorf("test_notes = %q, want %q", testNotes, "ran go vet")
	}

	// 5. Test bare TASK_COMPLETE (no summary block) for fallback detection.
	// Clear the pane and send bare marker.
	sendKeys(t, tmuxSession, "clear")
	time.Sleep(300 * time.Millisecond)
	sendKeys(t, tmuxSession, "echo 'TASK_COMPLETE'")
	sendKeys(t, tmuxSession, "echo ''")
	sendKeys(t, tmuxSession, "echo 'unrelated text'")

	deadline = time.Now().Add(15 * time.Second)
	var captured2 string
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "capture-pane", "-t", tmuxSession, "-p").Output()
		if err == nil {
			captured2 = string(out)
			if strings.Contains(captured2, "TASK_COMPLETE") &&
				strings.Contains(captured2, "unrelated text") {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !strings.Contains(captured2, "TASK_COMPLETE") {
		t.Fatal("bare TASK_COMPLETE not found in tmux pane")
	}

	// With bare TASK_COMPLETE followed by blank line, pr_title should be empty (fallback).
	barePRTitle := extractField(captured2, "pr_title")
	if barePRTitle != "" {
		t.Errorf("bare TASK_COMPLETE: pr_title should be empty, got %q", barePRTitle)
	}

	t.Log("SUB-012 E2E: TASK_COMPLETE observer flow validated")
}

// sendKeys sends a command to the tmux session and waits briefly.
func sendKeys(t *testing.T, session, cmd string) {
	t.Helper()
	if err := exec.Command("tmux", "send-keys", "-t", session, cmd, "Enter").Run(); err != nil {
		t.Fatalf("send keys to tmux: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
}

// extractField finds "key: value" in text and returns value. Returns "" if not found.
func extractField(text, key string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		prefix := key + ":"
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
