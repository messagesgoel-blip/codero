package feedback

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TaskInfo holds metadata about the task being worked on.
type TaskInfo struct {
	TaskID       string
	AssignmentID string
	Repo         string
	Branch       string
	Description  string
}

// currentJSONPayload is the JSON structure written to .codero/feedback/current.json.
type currentJSONPayload struct {
	TaskID         string            `json:"task_id"`
	AssignmentID   string            `json:"assignment_id"`
	Repo           string            `json:"repo,omitempty"`
	Branch         string            `json:"branch,omitempty"`
	CacheHash      string            `json:"cache_hash"`
	Truncated      bool              `json:"truncated"`
	SourceStatuses map[string]string `json:"source_statuses"`
	Sections       []jsonSection     `json:"sections"`
	GeneratedAt    string            `json:"generated_at"`
}

// jsonSection is one section in the JSON payload.
type jsonSection struct {
	Source string `json:"source"`
	Status string `json:"status"`
	Body   string `json:"body"`
}

// WriteFeedbackFiles writes TASK.md, FEEDBACK.md, and feedback/current.json
// into <worktreePath>/.codero/. Directories are created if needed.
func WriteFeedbackFiles(worktreePath string, task TaskInfo, fb AggregateResult) error {
	coderoDir := filepath.Join(worktreePath, ".codero")
	feedbackDir := filepath.Join(coderoDir, "feedback")

	if err := os.MkdirAll(feedbackDir, 0o755); err != nil {
		return fmt.Errorf("creating .codero/feedback dir: %w", err)
	}

	// 1. TASK.md
	taskMD := fmt.Sprintf("# Task\n\n- **task_id:** %s\n- **description:** %s\n- **assignment_id:** %s\n- **repo:** %s\n- **branch:** %s\n",
		task.TaskID, task.Description, task.AssignmentID, task.Repo, task.Branch)
	if err := os.WriteFile(filepath.Join(coderoDir, "TASK.md"), []byte(taskMD), 0o644); err != nil {
		return fmt.Errorf("writing TASK.md: %w", err)
	}

	// 2. FEEDBACK.md
	feedbackContent := "# Feedback\n\n"
	if fb.ContextBlock != "" {
		feedbackContent += fb.ContextBlock
	} else {
		feedbackContent += "_No feedback available yet._\n"
	}
	if err := os.WriteFile(filepath.Join(coderoDir, "FEEDBACK.md"), []byte(feedbackContent), 0o644); err != nil {
		return fmt.Errorf("writing FEEDBACK.md: %w", err)
	}

	// 3. feedback/current.json
	sections := make([]jsonSection, len(fb.OrderedSections))
	for i, s := range fb.OrderedSections {
		sections[i] = jsonSection{
			Source: s.Source,
			Status: s.Status,
			Body:   s.Body,
		}
	}

	payload := currentJSONPayload{
		TaskID:         task.TaskID,
		AssignmentID:   task.AssignmentID,
		Repo:           task.Repo,
		Branch:         task.Branch,
		CacheHash:      fb.CacheHash,
		Truncated:      fb.Truncated,
		SourceStatuses: fb.SourceStatuses,
		Sections:       sections,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling current.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(feedbackDir, "current.json"), data, 0o644); err != nil {
		return fmt.Errorf("writing current.json: %w", err)
	}

	return nil
}
