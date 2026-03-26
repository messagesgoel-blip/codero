package deliverypipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	taskFileName      = "TASK.md"
	feedbackFileName  = "FEEDBACK.md"
	feedbackJSONName  = "current.json"
	feedbackDirName   = "feedback"
	defaultFeedbackMB = 32 * 1024
)

// Task describes the task payload written to TASK.md.
type Task struct {
	Title              string
	Description        string
	AcceptanceCriteria []string
}

// FeedbackItem describes a single feedback entry.
type FeedbackItem struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

// FeedbackPackage is the structured payload for feedback/current.json.
type FeedbackPackage struct {
	GateFindings   []FeedbackItem `json:"gate_findings,omitempty"`
	CodeReview     []FeedbackItem `json:"code_review,omitempty"`
	CIFailures     []FeedbackItem `json:"ci_failures,omitempty"`
	ReviewComments []FeedbackItem `json:"review_comments,omitempty"`
	GeneratedAt    time.Time      `json:"generated_at"`
}

// WriteTASK writes <worktree>/.codero/TASK.md with the task details.
func WriteTASK(worktree string, task Task) error {
	if strings.TrimSpace(worktree) == "" {
		return fmt.Errorf("write task: worktree is required")
	}

	var b strings.Builder
	b.WriteString("# TASK\n\n")
	b.WriteString("## Title\n")
	b.WriteString(strings.TrimSpace(task.Title))
	b.WriteString("\n\n## Description\n")
	if desc := strings.TrimSpace(task.Description); desc != "" {
		b.WriteString(desc)
	} else {
		b.WriteString("None")
	}
	b.WriteString("\n\n## Acceptance Criteria\n")
	if len(task.AcceptanceCriteria) == 0 {
		b.WriteString("- None\n")
	} else {
		for _, item := range task.AcceptanceCriteria {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}

	path := filepath.Join(worktree, coderoDir, taskFileName)
	return writeAtomic(path, []byte(b.String()), 0o644)
}

// WriteFEEDBACK writes FEEDBACK.md and feedback/current.json.
func WriteFEEDBACK(worktree string, feedback FeedbackPackage) error {
	if strings.TrimSpace(worktree) == "" {
		return fmt.Errorf("write feedback: worktree is required")
	}
	if feedback.GeneratedAt.IsZero() {
		feedback.GeneratedAt = time.Now().UTC()
	}

	content := buildFeedbackContent(feedback)
	content = truncateFeedback(content, feedbackMaxSize())

	feedbackDir := filepath.Join(worktree, coderoDir, feedbackDirName)
	if err := os.MkdirAll(feedbackDir, 0o755); err != nil {
		return fmt.Errorf("write feedback: create dir: %w", err)
	}

	mdPath := filepath.Join(worktree, coderoDir, feedbackFileName)
	if err := writeAtomic(mdPath, []byte(content), 0o644); err != nil {
		return err
	}

	data, err := json.MarshalIndent(feedback, "", "  ")
	if err != nil {
		return fmt.Errorf("write feedback: marshal json: %w", err)
	}
	jsonPath := filepath.Join(feedbackDir, feedbackJSONName)
	if err := writeAtomic(jsonPath, data, 0o644); err != nil {
		return err
	}
	return nil
}

// ClearFEEDBACK removes FEEDBACK.md and feedback/current.json if present.
func ClearFEEDBACK(worktree string) error {
	if strings.TrimSpace(worktree) == "" {
		return fmt.Errorf("clear feedback: worktree is required")
	}
	mdPath := filepath.Join(worktree, coderoDir, feedbackFileName)
	if err := os.Remove(mdPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear feedback: remove FEEDBACK.md: %w", err)
	}
	jsonPath := filepath.Join(worktree, coderoDir, feedbackDirName, feedbackJSONName)
	if err := os.Remove(jsonPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear feedback: remove json: %w", err)
	}
	return nil
}

func buildFeedbackContent(feedback FeedbackPackage) string {
	var b strings.Builder
	b.WriteString("# FEEDBACK\n\n")
	writeSection(&b, "Gate Findings", feedback.GateFindings)
	writeSection(&b, "Code Review", feedback.CodeReview)
	writeSection(&b, "CI Failures", feedback.CIFailures)
	writeSection(&b, "Review Comments", feedback.ReviewComments)
	return b.String()
}

func writeSection(b *strings.Builder, title string, items []FeedbackItem) {
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n")
	if len(items) == 0 {
		b.WriteString("- None\n\n")
		return
	}
	for _, item := range items {
		line := strings.TrimSpace(item.Message)
		if line == "" {
			continue
		}
		b.WriteString("- ")
		if item.File != "" {
			b.WriteString(item.File)
			if item.Line > 0 {
				b.WriteString(":")
				b.WriteString(strconv.Itoa(item.Line))
			}
			b.WriteString(": ")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func feedbackMaxSize() int {
	if raw := strings.TrimSpace(os.Getenv("CODERO_FEEDBACK_MAX_SIZE")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return defaultFeedbackMB
}

func truncateFeedback(content string, max int) string {
	if max <= 0 || len(content) <= max {
		return content
	}
	notice := "\n\n[truncated]\n"
	if max <= len(notice) {
		return notice[:max]
	}
	return content[:max-len(notice)] + notice
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("write atomic: create dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("write atomic: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write atomic: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write atomic: close temp: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("write atomic: chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("write atomic: rename: %w", err)
	}
	return nil
}
