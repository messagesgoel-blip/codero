package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	daemongrpc "github.com/codero/codero/internal/daemon/grpc"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
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

func startSessionFinalizeTestDaemon(t *testing.T) (*state.DB, *session.Store, string) {
	t.Helper()

	db, err := state.Open(filepath.Join(t.TempDir(), "daemon.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := session.NewStore(db)
	srv := daemongrpc.NewServer(daemongrpc.ServerConfig{
		DB:           db,
		RawDB:        db.Unwrap(),
		SessionStore: store,
		Version:      "test",
	})
	srv.MarkReady()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		srv.GRPCServer().Stop()
		_ = lis.Close()
	})
	go srv.GRPCServer().Serve(lis)

	return db, store, lis.Addr().String()
}

func TestSessionFinalizeCmd_UsesDaemonWhenConfigured(t *testing.T) {
	db, store, daemonAddr := startSessionFinalizeTestDaemon(t)
	ctx := context.Background()

	const (
		sessionID = "sess-finalize-daemon"
		agentID   = "agent-finalize-daemon"
		taskID    = "TASK-DAEMON-1"
	)

	if _, err := store.Register(ctx, sessionID, agentID, "coding"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	runtimeRoot := t.TempDir()
	sessionDir := filepath.Join(runtimeRoot, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionMDPath := filepath.Join(sessionDir, "SESSION.md")
	body := `# Runtime Session Note

- CODERO_AGENT_ID=` + agentID + `
- CODERO_SESSION_ID=` + sessionID + `
- CODERO_WORKTREE=/srv/storage/repo/codero/.worktrees/SES-001-session-finalize/.tmp-tests/wt
- TEST_REPO=acme/api
- TEST_BRANCH=feat/finalize-daemon
- TEST_TASK_ID=` + taskID + `

## Completion Record
` + "```json" + `
{
  "task_id": "",
  "status": "merged",
  "substatus": "terminal_finished",
  "summary": "daemon finalize route",
  "tests": ["go test ./cmd/codero"],
  "finished_at": "2026-04-02T18:00:00Z"
}
` + "```" + `
`
	if err := os.WriteFile(sessionMDPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write session note: %v", err)
	}

	out, err := runCmd(t, sessionCmd,
		"--daemon-addr", daemonAddr,
		"finalize",
		"--from-session-md", sessionMDPath,
	)
	if err != nil {
		t.Fatalf("session finalize via daemon: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "session "+sessionID+" finalized") {
		t.Fatalf("output = %q, want finalize success message", out)
	}

	archive, err := state.GetSessionArchive(ctx, db, sessionID)
	if err != nil {
		t.Fatalf("GetSessionArchive: %v", err)
	}
	if archive.AgentID != agentID {
		t.Fatalf("archive.AgentID = %q, want %q", archive.AgentID, agentID)
	}
	if archive.TaskID != taskID {
		t.Fatalf("archive.TaskID = %q, want %q", archive.TaskID, taskID)
	}
	if archive.Result != "merged" {
		t.Fatalf("archive.Result = %q, want merged", archive.Result)
	}

	archivedMD := filepath.Join(runtimeRoot, "archive", sessionID+".md")
	if _, err := os.Stat(archivedMD); err != nil {
		t.Fatalf("archived session note missing: %v", err)
	}
	if _, err := os.Stat(sessionMDPath); !os.IsNotExist(err) {
		t.Fatalf("session note still present at %s; stat err=%v", sessionMDPath, err)
	}

	metaBody, err := os.ReadFile(filepath.Join(runtimeRoot, "archive", sessionID+".json"))
	if err != nil {
		t.Fatalf("read archive metadata: %v", err)
	}
	var meta archivedSessionMetadata
	if err := json.Unmarshal(metaBody, &meta); err != nil {
		t.Fatalf("json.Unmarshal metadata: %v", err)
	}
	if meta.Completion.TaskID != taskID {
		t.Fatalf("metadata completion task_id = %q, want %q", meta.Completion.TaskID, taskID)
	}
}
