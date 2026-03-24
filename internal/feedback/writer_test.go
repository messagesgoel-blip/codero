package feedback

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFeedbackFiles_CreatesAllFiles(t *testing.T) {
	dir := t.TempDir()

	task := TaskInfo{
		TaskID:       "task-123",
		AssignmentID: "assign-456",
		Repo:         "myorg/myrepo",
		Branch:       "feat/cool-feature",
		Description:  "Implement the cool feature",
	}

	fb := AggregateFeedback(AggregateInput{
		CI: &SourceSnapshot{
			Status: "failure",
			Body:   "CI failed: lint errors on main.go",
		},
		Human: &SourceSnapshot{
			Status: "changes_requested",
			Body:   "Please fix the formatting",
		},
	})

	if err := WriteFeedbackFiles(dir, task, fb); err != nil {
		t.Fatalf("WriteFeedbackFiles: %v", err)
	}

	// Verify TASK.md
	taskBytes, err := os.ReadFile(filepath.Join(dir, ".codero", "TASK.md"))
	if err != nil {
		t.Fatalf("reading TASK.md: %v", err)
	}
	taskContent := string(taskBytes)
	if !strings.Contains(taskContent, "task-123") {
		t.Errorf("TASK.md missing task_id; got:\n%s", taskContent)
	}
	if !strings.Contains(taskContent, "Implement the cool feature") {
		t.Errorf("TASK.md missing description; got:\n%s", taskContent)
	}
	if !strings.Contains(taskContent, "assign-456") {
		t.Errorf("TASK.md missing assignment_id; got:\n%s", taskContent)
	}
	if !strings.Contains(taskContent, "myorg/myrepo") {
		t.Errorf("TASK.md missing repo; got:\n%s", taskContent)
	}
	if !strings.Contains(taskContent, "feat/cool-feature") {
		t.Errorf("TASK.md missing branch; got:\n%s", taskContent)
	}

	// Verify FEEDBACK.md
	fbBytes, err := os.ReadFile(filepath.Join(dir, ".codero", "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("reading FEEDBACK.md: %v", err)
	}
	fbContent := string(fbBytes)
	if !strings.Contains(fbContent, "# Feedback") {
		t.Errorf("FEEDBACK.md missing header; got:\n%s", fbContent)
	}
	if !strings.Contains(fbContent, "CI failed: lint errors on main.go") {
		t.Errorf("FEEDBACK.md missing CI feedback; got:\n%s", fbContent)
	}
	if !strings.Contains(fbContent, "Please fix the formatting") {
		t.Errorf("FEEDBACK.md missing human feedback; got:\n%s", fbContent)
	}

	// Verify current.json
	jsonBytes, err := os.ReadFile(filepath.Join(dir, ".codero", "feedback", "current.json"))
	if err != nil {
		t.Fatalf("reading current.json: %v", err)
	}
	var payload currentJSONPayload
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		t.Fatalf("parsing current.json: %v", err)
	}
	if payload.TaskID != "task-123" {
		t.Errorf("current.json task_id = %q, want %q", payload.TaskID, "task-123")
	}
	if payload.AssignmentID != "assign-456" {
		t.Errorf("current.json assignment_id = %q, want %q", payload.AssignmentID, "assign-456")
	}
	if payload.GeneratedAt == "" {
		t.Error("current.json generated_at is empty")
	}
	if len(payload.Sections) != 2 {
		t.Errorf("current.json sections count = %d, want 2", len(payload.Sections))
	}
}

func TestWriteFeedbackFiles_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()

	task1 := TaskInfo{
		TaskID:       "task-first",
		AssignmentID: "assign-first",
		Description:  "First description",
	}
	fb1 := AggregateFeedback(AggregateInput{
		CI: &SourceSnapshot{
			Status: "success",
			Body:   "All checks passed",
		},
	})

	if err := WriteFeedbackFiles(dir, task1, fb1); err != nil {
		t.Fatalf("first WriteFeedbackFiles: %v", err)
	}

	// Write again with different content.
	task2 := TaskInfo{
		TaskID:       "task-second",
		AssignmentID: "assign-second",
		Description:  "Second description",
	}
	fb2 := AggregateFeedback(AggregateInput{
		CI: &SourceSnapshot{
			Status: "failure",
			Body:   "Build failed",
		},
	})

	if err := WriteFeedbackFiles(dir, task2, fb2); err != nil {
		t.Fatalf("second WriteFeedbackFiles: %v", err)
	}

	// Verify second content wins in TASK.md.
	taskBytes, err := os.ReadFile(filepath.Join(dir, ".codero", "TASK.md"))
	if err != nil {
		t.Fatalf("reading TASK.md: %v", err)
	}
	taskContent := string(taskBytes)
	if strings.Contains(taskContent, "task-first") {
		t.Error("TASK.md still contains first task_id after overwrite")
	}
	if !strings.Contains(taskContent, "task-second") {
		t.Error("TASK.md missing second task_id after overwrite")
	}

	// Verify second content wins in FEEDBACK.md.
	fbBytes, err := os.ReadFile(filepath.Join(dir, ".codero", "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("reading FEEDBACK.md: %v", err)
	}
	fbContent := string(fbBytes)
	if strings.Contains(fbContent, "All checks passed") {
		t.Error("FEEDBACK.md still contains first feedback after overwrite")
	}
	if !strings.Contains(fbContent, "Build failed") {
		t.Error("FEEDBACK.md missing second feedback after overwrite")
	}

	// Verify second content wins in current.json.
	jsonBytes, err := os.ReadFile(filepath.Join(dir, ".codero", "feedback", "current.json"))
	if err != nil {
		t.Fatalf("reading current.json: %v", err)
	}
	var payload currentJSONPayload
	if err := json.Unmarshal(jsonBytes, &payload); err != nil {
		t.Fatalf("parsing current.json: %v", err)
	}
	if payload.TaskID != "task-second" {
		t.Errorf("current.json task_id = %q, want %q", payload.TaskID, "task-second")
	}
}

func TestWriteFeedbackFiles_EmptyFeedback(t *testing.T) {
	dir := t.TempDir()

	task := TaskInfo{
		TaskID:       "task-empty",
		AssignmentID: "assign-empty",
		Description:  "Empty feedback test",
	}

	fb := AggregateFeedback(AggregateInput{})

	if err := WriteFeedbackFiles(dir, task, fb); err != nil {
		t.Fatalf("WriteFeedbackFiles: %v", err)
	}

	fbBytes, err := os.ReadFile(filepath.Join(dir, ".codero", "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("reading FEEDBACK.md: %v", err)
	}
	fbContent := string(fbBytes)
	if !strings.Contains(fbContent, "_No feedback available yet._") {
		t.Errorf("FEEDBACK.md should contain placeholder text for empty feedback; got:\n%s", fbContent)
	}
}
