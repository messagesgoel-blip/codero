package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codero/codero/internal/session"
	"github.com/spf13/cobra"
)

type sessionCompletionRecord struct {
	TaskID     string   `json:"task_id"`
	Status     string   `json:"status"`
	Summary    string   `json:"summary"`
	Tests      []string `json:"tests"`
	FinishedAt string   `json:"finished_at"`
}

type parsedSessionNote struct {
	SessionID  string
	AgentID    string
	Repo       string
	Branch     string
	Worktree   string
	TaskID     string
	Completion sessionCompletionRecord
}

func sessionFinalizeCmd(configPath *string) *cobra.Command {
	var (
		sessionMDPath string
		archiveDir    string
	)

	cmd := &cobra.Command{
		Use:   "finalize",
		Short: "Finalize a session from SESSION.md completion data",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			if sessionMDPath == "" {
				return fmt.Errorf("from-session-md is required")
			}
			note, err := parseSessionNote(sessionMDPath)
			if err != nil {
				return err
			}
			finishedAt, err := time.Parse(time.RFC3339, note.Completion.FinishedAt)
			if err != nil {
				return fmt.Errorf("parse completion finished_at: %w", err)
			}
			if note.Completion.TaskID == "" {
				note.Completion.TaskID = note.TaskID
			}
			if err := store.Finalize(cmd.Context(), note.SessionID, note.AgentID, session.Completion{
				TaskID:     note.Completion.TaskID,
				Status:     note.Completion.Status,
				Summary:    note.Completion.Summary,
				Tests:      note.Completion.Tests,
				FinishedAt: finishedAt,
			}); err != nil {
				return err
			}
			archivedPath, err := archiveSessionNote(sessionMDPath, note.SessionID, archiveDir)
			if err != nil {
				return err
			}
			fmt.Printf("session %s finalized\narchived: %s\n", note.SessionID, archivedPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionMDPath, "from-session-md", "", "path to runtime SESSION.md")
	cmd.Flags().StringVar(&archiveDir, "archive-dir", "", "archive directory (default: runtime-root/archive)")

	return cmd
}

func parseSessionNote(path string) (*parsedSessionNote, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session note: %w", err)
	}
	text := string(body)

	note := &parsedSessionNote{
		SessionID: parseSessionNoteKV(text, "CODERO_SESSION_ID"),
		AgentID:   parseSessionNoteKV(text, "CODERO_AGENT_ID"),
		Repo:      parseSessionNoteKV(text, "TEST_REPO"),
		Branch:    parseSessionNoteKV(text, "TEST_BRANCH"),
		Worktree:  parseSessionNoteKV(text, "CODERO_WORKTREE"),
		TaskID:    parseSessionNoteKV(text, "TEST_TASK_ID"),
	}
	if note.SessionID == "" {
		return nil, fmt.Errorf("parse session note: missing CODERO_SESSION_ID")
	}
	if note.AgentID == "" {
		return nil, fmt.Errorf("parse session note: missing CODERO_AGENT_ID")
	}

	block, err := extractCompletionJSON(text)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(block), &note.Completion); err != nil {
		return nil, fmt.Errorf("parse session note completion: %w", err)
	}
	if note.Completion.Status == "" {
		return nil, fmt.Errorf("parse session note completion: missing status")
	}
	if note.Completion.FinishedAt == "" {
		return nil, fmt.Errorf("parse session note completion: missing finished_at")
	}
	return note, nil
}

func parseSessionNoteKV(text, key string) string {
	prefix := "- " + key + "="
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func extractCompletionJSON(text string) (string, error) {
	const heading = "## Completion Record"
	idx := strings.Index(text, heading)
	if idx == -1 {
		return "", fmt.Errorf("parse session note completion: missing completion record")
	}
	rest := text[idx+len(heading):]
	startFence := strings.Index(rest, "```json")
	if startFence == -1 {
		return "", fmt.Errorf("parse session note completion: missing json block")
	}
	rest = rest[startFence+len("```json"):]
	endFence := strings.Index(rest, "```")
	if endFence == -1 {
		return "", fmt.Errorf("parse session note completion: unterminated json block")
	}
	return strings.TrimSpace(rest[:endFence]), nil
}

func archiveSessionNote(path, sessionID, archiveDir string) (string, error) {
	if archiveDir == "" {
		parent := filepath.Dir(path)
		if filepath.Base(parent) == sessionID {
			archiveDir = filepath.Join(filepath.Dir(parent), "archive")
		} else {
			archiveDir = filepath.Join(parent, "archive")
		}
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("archive session note: mkdir: %w", err)
	}
	target := filepath.Join(archiveDir, sessionID+".md")
	if err := os.Rename(path, target); err != nil {
		return "", fmt.Errorf("archive session note: rename: %w", err)
	}
	return target, nil
}
