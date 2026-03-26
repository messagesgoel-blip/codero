package deliverypipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTASK(t *testing.T) {
	worktree := t.TempDir()
	task := Task{
		Title:       "Fix API bug",
		Description: "Ensure the handler returns 202.",
		AcceptanceCriteria: []string{
			"Status code is 202",
			"Body contains assignment id",
		},
	}

	if err := WriteTASK(worktree, task); err != nil {
		t.Fatalf("WriteTASK: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(worktree, coderoDir, taskFileName))
	if err != nil {
		t.Fatalf("read TASK.md: %v", err)
	}
	content := string(data)
	for _, frag := range []string{task.Title, task.Description, "Status code is 202"} {
		if !strings.Contains(content, frag) {
			t.Errorf("TASK.md missing %q", frag)
		}
	}
}

func TestWriteFEEDBACK(t *testing.T) {
	worktree := t.TempDir()
	feedback := FeedbackPackage{
		GateFindings: []FeedbackItem{
			{File: "main.go", Line: 12, Message: "missing error check"},
		},
		CodeReview: []FeedbackItem{
			{Message: "Consider renaming variable"},
		},
	}

	if err := WriteFEEDBACK(worktree, feedback); err != nil {
		t.Fatalf("WriteFEEDBACK: %v", err)
	}

	mdPath := filepath.Join(worktree, coderoDir, feedbackFileName)
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read FEEDBACK.md: %v", err)
	}
	content := string(data)
	for _, header := range []string{"Gate Findings", "Code Review", "CI Failures", "Review Comments"} {
		if !strings.Contains(content, header) {
			t.Errorf("FEEDBACK.md missing %q section", header)
		}
	}

	jsonPath := filepath.Join(worktree, coderoDir, feedbackDirName, feedbackJSONName)
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read current.json: %v", err)
	}
	var parsed FeedbackPackage
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("current.json should parse: %v", err)
	}
}

func TestWriteFEEDBACK_Truncates(t *testing.T) {
	t.Setenv("CODERO_FEEDBACK_MAX_SIZE", "512")
	worktree := t.TempDir()
	large := strings.Repeat("A", 2048)
	feedback := FeedbackPackage{
		CodeReview: []FeedbackItem{{Message: large}},
	}

	if err := WriteFEEDBACK(worktree, feedback); err != nil {
		t.Fatalf("WriteFEEDBACK: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(worktree, coderoDir, feedbackFileName))
	if err != nil {
		t.Fatalf("read FEEDBACK.md: %v", err)
	}
	if len(data) > 512 {
		t.Fatalf("expected FEEDBACK.md to be truncated, got %d bytes", len(data))
	}
	if !strings.Contains(string(data), "[truncated]") {
		t.Errorf("expected truncation notice")
	}
}

func TestClearFEEDBACK(t *testing.T) {
	worktree := t.TempDir()
	if err := WriteFEEDBACK(worktree, FeedbackPackage{}); err != nil {
		t.Fatalf("WriteFEEDBACK: %v", err)
	}
	if err := ClearFEEDBACK(worktree); err != nil {
		t.Fatalf("ClearFEEDBACK: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktree, coderoDir, feedbackFileName)); !os.IsNotExist(err) {
		t.Errorf("FEEDBACK.md should be removed")
	}
	if _, err := os.Stat(filepath.Join(worktree, coderoDir, feedbackDirName, feedbackJSONName)); !os.IsNotExist(err) {
		t.Errorf("current.json should be removed")
	}
}

func TestWriteFEEDBACK_NoTempFilesLeft(t *testing.T) {
	worktree := t.TempDir()
	if err := WriteFEEDBACK(worktree, FeedbackPackage{}); err != nil {
		t.Fatalf("WriteFEEDBACK: %v", err)
	}

	dir := filepath.Join(worktree, coderoDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("unexpected temp file: %s", entry.Name())
		}
	}
}
