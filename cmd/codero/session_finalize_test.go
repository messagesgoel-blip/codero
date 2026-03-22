package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseSessionNote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SESSION.md")
	body := `# Runtime Session Note

- CODERO_AGENT_ID=opencode-pilot
- CODERO_SESSION_ID=sess-1
- CODERO_WORKTREE=/srv/storage/repo/codero/.worktrees/COD-066-cozy-tui-port/.tmp-tests/wt
- TEST_REPO=acme/api
- TEST_BRANCH=feat/test
- TEST_TASK_ID=TASK-1

## Completion Record
` + "```json" + `
{
  "task_id": "TASK-1",
  "status": "done",
  "substatus": "terminal_finished",
  "summary": "finished work",
  "tests": ["go test ./cmd/codero"],
  "finished_at": "2026-03-21T20:00:00Z"
}
` + "```" + `
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write session note: %v", err)
	}

	note, err := parseSessionNote(path)
	if err != nil {
		t.Fatalf("parseSessionNote: %v", err)
	}
	if note.SessionID != "sess-1" || note.AgentID != "opencode-pilot" {
		t.Fatalf("parsed identity = %#v", note)
	}
	if note.Completion.Status != "done" || note.Completion.TaskID != "TASK-1" {
		t.Fatalf("parsed completion = %#v", note.Completion)
	}
	if note.Completion.Substatus != "terminal_finished" {
		t.Fatalf("parsed completion substatus = %q, want terminal_finished", note.Completion.Substatus)
	}
}

func TestArchiveSessionNote(t *testing.T) {
	runtimeRoot := t.TempDir()
	sessionID := "sess-archive"
	sessionDir := filepath.Join(runtimeRoot, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(sessionDir, "SESSION.md")
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write session note: %v", err)
	}

	note := &parsedSessionNote{
		SessionID: sessionID,
		AgentID:   "agent-1",
		Repo:      "acme/api",
		Branch:    "feat/test",
		Worktree:  "/srv/storage/repo/codero/.worktrees/COD-066-cozy-tui-port/.tmp-tests/archive-wt",
		TaskID:    "TASK-1",
		Completion: sessionCompletionRecord{
			TaskID:     "TASK-1",
			Status:     "done",
			Substatus:  "terminal_finished",
			Summary:    "finished",
			Tests:      []string{"go test ./cmd/codero"},
			FinishedAt: "2026-03-21T20:00:00Z",
		},
	}

	archived, err := archiveSessionNote(path, note, "")
	if err != nil {
		t.Fatalf("archiveSessionNote: %v", err)
	}
	want := filepath.Join(runtimeRoot, "archive", sessionID+".md")
	if archived != want {
		t.Fatalf("archived path = %q, want %q", archived, want)
	}
	if _, err := os.Stat(archived); err != nil {
		t.Fatalf("archived file missing: %v", err)
	}
	metaPath := filepath.Join(runtimeRoot, "archive", sessionID+".json")
	metaBody, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta archivedSessionMetadata
	if err := json.Unmarshal(metaBody, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta.SessionID != sessionID || meta.ArchivedMD != archived {
		t.Fatalf("metadata = %#v", meta)
	}
}
