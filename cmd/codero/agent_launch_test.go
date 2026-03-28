package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tmux"
)

type fastExitExecutor struct {
	*tmux.MockExecutor
}

func (f *fastExitExecutor) HasSession(_ context.Context, _ string) bool {
	return false
}

func openTestStore(t *testing.T) (*session.Store, *state.DB, func()) {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "agent-launch.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	cleanup := func() {
		_ = db.Close()
	}
	return session.NewStore(db), db, cleanup
}

func withDeterministicLaunch(t *testing.T, sessionID string, now time.Time) {
	t.Helper()
	prevNow := agentLaunchNow
	prevUUID := agentLaunchUUID
	agentLaunchNow = func() time.Time { return now }
	agentLaunchUUID = func() string { return sessionID }
	t.Cleanup(func() {
		agentLaunchNow = prevNow
		agentLaunchUUID = prevUUID
	})
}

func TestRunAgentLaunch_WritesFilesAndCleansUp(t *testing.T) {
	const (
		agentID   = "agent-1"
		sessionID = "11111111-2222-3333-4444-555555555555"
	)
	startedAt := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
	withDeterministicLaunch(t, sessionID, startedAt)

	store, db, cleanup := openTestStore(t)
	defer cleanup()

	worktree := t.TempDir()
	exec := &fastExitExecutor{MockExecutor: tmux.NewMockExecutor()}
	tmuxName := tmux.SessionName(agentID, sessionID)
	exec.PaneContent[tmuxName] = "session-log"

	cfg := &AgentLaunchConfig{
		AgentID:      agentID,
		RepoPath:     worktree,
		Mode:         "agent",
		AgentCommand: []string{"echo", "hello"},
		WriteLog:     true,
		TmuxExecutor: exec,
	}

	if err := runAgentLaunch(context.Background(), cfg, store); err != nil {
		t.Fatalf("runAgentLaunch: %v", err)
	}

	if len(exec.CreatedNames) != 1 || exec.CreatedNames[0] != tmuxName {
		t.Fatalf("tmux session name: got %v, want %s", exec.CreatedNames, tmuxName)
	}
	if len(exec.KilledNames) != 1 || exec.KilledNames[0] != tmuxName {
		t.Fatalf("tmux kill: got %v, want %s", exec.KilledNames, tmuxName)
	}
	if len(exec.SentKeys) != 1 {
		t.Fatalf("send keys: expected 1 (combined env+command), got %d: %v", len(exec.SentKeys), exec.SentKeys)
	}
	sent := exec.SentKeys[0].Command
	if !strings.Contains(sent, "CODERO_SESSION_ID='"+sessionID+"'") {
		t.Fatalf("send-keys should export CODERO_SESSION_ID, got: %s", sent)
	}
	if !strings.Contains(sent, "CODERO_AGENT_ID='"+agentID+"'") {
		t.Fatalf("send-keys should export CODERO_AGENT_ID, got: %s", sent)
	}
	if !strings.Contains(sent, "&& exec echo hello") {
		t.Fatalf("send-keys should contain agent command after && exec, got: %s", sent)
	}

	sessionMD, err := os.ReadFile(filepath.Join(worktree, ".codero", "SESSION.md"))
	if err != nil {
		t.Fatalf("read SESSION.md: %v", err)
	}
	if !strings.Contains(string(sessionMD), sessionID) {
		t.Fatalf("SESSION.md missing session_id")
	}
	if !strings.Contains(string(sessionMD), tmuxName) {
		t.Fatalf("SESSION.md missing tmux name")
	}
	if !strings.Contains(string(sessionMD), startedAt.Format(time.RFC3339)) {
		t.Fatalf("SESSION.md missing started_at")
	}

	agentMD, err := os.ReadFile(filepath.Join(worktree, ".codero", "AGENT.md"))
	if err != nil {
		t.Fatalf("read AGENT.md: %v", err)
	}
	if !strings.Contains(string(agentMD), agentID) || !strings.Contains(string(agentMD), sessionID) {
		t.Fatalf("AGENT.md missing agent/session ids")
	}

	hook, err := os.ReadFile(filepath.Join(worktree, ".codero", "hooks", "on-feedback"))
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !strings.Contains(string(hook), `TMUX_NAME="`+tmuxName+`"`) {
		t.Fatalf("hook missing tmux name")
	}

	logPath := filepath.Join(worktree, ".codero", "session-log.txt")
	if logData, err := os.ReadFile(logPath); err != nil {
		t.Fatalf("read session-log.txt: %v", err)
	} else if string(logData) != "session-log" {
		t.Fatalf("session-log.txt: got %q", string(logData))
	}

	row, err := state.GetAgentSession(context.Background(), db, sessionID)
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if row.TmuxSessionName != tmuxName {
		t.Fatalf("tmux_session_name: got %q, want %q", row.TmuxSessionName, tmuxName)
	}
	if row.EndedAt == nil {
		t.Fatalf("ended_at should be set after finalize")
	}
}

func TestAgentLaunch_ParityWithShellWrapper(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}

	const (
		agentID   = "agent-2"
		sessionID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	)
	startedAt := time.Date(2026, 3, 26, 13, 0, 0, 0, time.UTC)
	withDeterministicLaunch(t, sessionID, startedAt)

	store, _, cleanup := openTestStore(t)
	defer cleanup()

	goWorktree := t.TempDir()
	shellWorktree := t.TempDir()
	tmuxName := tmux.SessionName(agentID, sessionID)

	execMock := &fastExitExecutor{MockExecutor: tmux.NewMockExecutor()}
	goCfg := &AgentLaunchConfig{
		AgentID:      agentID,
		RepoPath:     goWorktree,
		Mode:         "agent",
		AgentCommand: []string{"echo", "hello"},
		WriteLog:     false,
		TmuxExecutor: execMock,
	}
	if err := runAgentLaunch(context.Background(), goCfg, store); err != nil {
		t.Fatalf("runAgentLaunch: %v", err)
	}

	stubDir := t.TempDir()
	tmuxStateDir := filepath.Join(stubDir, "tmux-state")
	if err := os.MkdirAll(tmuxStateDir, 0o755); err != nil {
		t.Fatalf("mkdir tmux state: %v", err)
	}

	writeStub(t, stubDir, "tmux", tmuxStubScript())
	writeStub(t, stubDir, "codero", coderoStubScript())

	cmd := exec.Command("/srv/storage/shared/agent-toolkit/bin/codero-agent",
		"--agent", agentID,
		"--repo", shellWorktree,
		"--",
		"echo", "hello",
	)
	cmd.Env = append(os.Environ(),
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TMUX_STATE_DIR="+tmuxStateDir,
		"CODERO_SESSION_ID="+sessionID,
		"CODERO_STARTED_AT="+startedAt.Format(time.RFC3339),
		"CODERO_AGENT_WRITE_SESSION_LOG=false",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("codero-agent: %v\n%s", err, string(out))
	}

	compareFile(t,
		filepath.Join(goWorktree, ".codero", "SESSION.md"),
		filepath.Join(shellWorktree, ".codero", "SESSION.md"),
	)
	compareFile(t,
		filepath.Join(goWorktree, ".codero", "AGENT.md"),
		filepath.Join(shellWorktree, ".codero", "AGENT.md"),
	)
	compareFile(t,
		filepath.Join(goWorktree, ".codero", "hooks", "on-feedback"),
		filepath.Join(shellWorktree, ".codero", "hooks", "on-feedback"),
	)

	hook := mustRead(t, filepath.Join(shellWorktree, ".codero", "hooks", "on-feedback"))
	if !strings.Contains(hook, `TMUX_NAME="`+tmuxName+`"`) {
		t.Fatalf("hook missing tmux name")
	}
}

func compareFile(t *testing.T, left, right string) {
	t.Helper()
	leftData := mustRead(t, left)
	rightData := mustRead(t, right)
	if leftData != rightData {
		t.Fatalf("file mismatch:\nleft=%s\nright=%s", left, right)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func writeStub(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
}

func tmuxStubScript() string {
	return `#!/bin/bash
set -u
state_dir="${TMUX_STATE_DIR:?TMUX_STATE_DIR not set}"
mkdir -p "$state_dir"
cmd="$1"
shift || true

name=""
case "$cmd" in
  new-session)
    while [[ $# -gt 0 ]]; do
      case "$1" in
        -s)
          name="$2"
          shift 2
          ;;
        -c)
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    date +%s > "$state_dir/$name"
    exit 0
    ;;
  has-session)
    while [[ $# -gt 0 ]]; do
      case "$1" in
        -t)
          name="$2"
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    if [[ -f "$state_dir/$name" ]]; then
      created=$(cat "$state_dir/$name")
      now=$(date +%s)
      if (( now - created >= 1 )); then
        rm -f "$state_dir/$name"
        exit 1
      fi
      exit 0
    fi
    exit 1
    ;;
  send-keys)
    exit 0
    ;;
  kill-session)
    while [[ $# -gt 0 ]]; do
      case "$1" in
        -t)
          name="$2"
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    rm -f "$state_dir/$name"
    exit 0
    ;;
  capture-pane)
    echo "stub-log"
    exit 0
    ;;
  display-message)
    exit 0
    ;;
  list-sessions)
    for f in "$state_dir"/*; do
      [[ -e "$f" ]] || exit 0
      basename "$f"
    done
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
}

func coderoStubScript() string {
	return `#!/bin/bash
if [[ "$1" == "session" && "$2" == "register" ]]; then
  exit 0
fi
if [[ "$1" == "session" && "$2" == "end" ]]; then
  exit 0
fi
exit 0
`
}
