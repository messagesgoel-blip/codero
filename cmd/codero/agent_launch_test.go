package main

import (
	"context"
	"errors"
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

type failingRegisterBackend struct {
	*session.Store
}

func (f *failingRegisterBackend) RegisterWithTmux(context.Context, string, string, string, string) (string, error) {
	return "", errors.New("register failed")
}

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
	if len(exec.Respawned) != 1 {
		t.Fatalf("respawn window: expected 1 launch, got %d: %+v", len(exec.Respawned), exec.Respawned)
	}
	launch := exec.Respawned[0].Command
	if len(launch) < 4 {
		t.Fatalf("respawn-window command too short: %+v", launch)
	}
	if launch[0] != "env" || launch[1] != "-i" {
		t.Fatalf("respawn-window should start with env -i, got: %+v", launch)
	}
	hasSessionID := false
	hasAgentID := false
	hasWorktree := false
	hasMode := false
	hasRuntimeSessionMD := false
	hasTmuxName := false
	hasStartedAt := false
	hasWriteLog := false
	for _, arg := range launch[2:] {
		switch {
		case arg == "CODERO_SESSION_ID="+sessionID:
			hasSessionID = true
		case arg == "CODERO_AGENT_ID="+agentID:
			hasAgentID = true
		case arg == "CODERO_WORKTREE="+worktree:
			hasWorktree = true
		case arg == "CODERO_SESSION_MODE=agent":
			hasMode = true
		case strings.HasPrefix(arg, "CODERO_RUNTIME_SESSION_MD="):
			hasRuntimeSessionMD = true
		case arg == "CODERO_TMUX_NAME="+tmuxName:
			hasTmuxName = true
		case arg == "CODERO_STARTED_AT="+startedAt.Format(time.RFC3339):
			hasStartedAt = true
		case arg == "CODERO_AGENT_WRITE_SESSION_LOG=true":
			hasWriteLog = true
		}
	}
	for name, ok := range map[string]bool{
		"CODERO_SESSION_ID":              hasSessionID,
		"CODERO_AGENT_ID":                hasAgentID,
		"CODERO_WORKTREE":                hasWorktree,
		"CODERO_SESSION_MODE":            hasMode,
		"CODERO_RUNTIME_SESSION_MD":      hasRuntimeSessionMD,
		"CODERO_TMUX_NAME":               hasTmuxName,
		"CODERO_STARTED_AT":              hasStartedAt,
		"CODERO_AGENT_WRITE_SESSION_LOG": hasWriteLog,
	} {
		if !ok {
			t.Fatalf("respawn-window missing %s: %+v", name, launch)
		}
	}
	if got := launch[len(launch)-2:]; len(got) != 2 || got[0] != "echo" || got[1] != "hello" {
		t.Fatalf("respawn-window should exec agent command directly, got: %+v", launch)
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
	if !strings.Contains(string(hook), `TMUX_NAME="${4:-}"`) {
		t.Fatalf("hook should read tmux name from argv: %s", hook)
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

func TestRunAgentLaunch_CleansUpCoderoArtifactsOnRegisterFailure(t *testing.T) {
	const (
		agentID   = "agent-fail"
		sessionID = "99999999-aaaa-bbbb-cccc-dddddddddddd"
	)
	startedAt := time.Date(2026, 3, 26, 14, 0, 0, 0, time.UTC)
	withDeterministicLaunch(t, sessionID, startedAt)

	store, _, cleanup := openTestStore(t)
	defer cleanup()

	worktree := t.TempDir()
	exec := &fastExitExecutor{MockExecutor: tmux.NewMockExecutor()}
	cfg := &AgentLaunchConfig{
		AgentID:      agentID,
		RepoPath:     worktree,
		Mode:         "agent",
		AgentCommand: []string{"echo", "hello"},
		TmuxExecutor: exec,
	}

	err := runAgentLaunch(context.Background(), cfg, &failingRegisterBackend{Store: store})
	if err == nil {
		t.Fatal("expected register failure")
	}
	if _, statErr := os.Stat(filepath.Join(worktree, ".codero")); !os.IsNotExist(statErr) {
		t.Fatalf(".codero should be cleaned up on register failure, stat err=%v", statErr)
	}
	if len(exec.KilledNames) != 1 {
		t.Fatalf("expected tmux cleanup on register failure, got %v", exec.KilledNames)
	}
}

func TestAgentLaunch_ParityWithShellWrapper(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	if _, err := os.Stat("/srv/storage/shared/agent-toolkit/bin/codero-agent"); err != nil {
		t.Skip("shared codero-agent wrapper unavailable")
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
	hook := mustRead(t, filepath.Join(shellWorktree, ".codero", "hooks", "on-feedback"))
	if !strings.Contains(hook, "feedback/pending") {
		t.Fatalf("hook should mark pending feedback: %s", hook)
	}
}

func TestResolveWorktree_UsesPorcelainBranchLine(t *testing.T) {
	repoRoot, worktreePath, branch := setupGitWorktreeRepo(t, "master", "feature-codero")

	got := resolveWorktree(repoRoot, branch)
	if got != worktreePath {
		t.Fatalf("resolveWorktree(%q, %q) = %q, want %q", repoRoot, branch, got, worktreePath)
	}
}

func TestResolveWorktree_UsesMainBranchWorktree(t *testing.T) {
	repoRoot, worktreePath, branch := setupGitWorktreeRepo(t, "master", "main")

	got := resolveWorktree(repoRoot, branch)
	if got != worktreePath {
		t.Fatalf("resolveWorktree(%q, %q) = %q, want %q", repoRoot, branch, got, worktreePath)
	}
}

func TestResolveWorktree_UsesMasterBranchWorktree(t *testing.T) {
	repoRoot, worktreePath, branch := setupGitWorktreeRepo(t, "main", "master")

	got := resolveWorktree(repoRoot, branch)
	if got != worktreePath {
		t.Fatalf("resolveWorktree(%q, %q) = %q, want %q", repoRoot, branch, got, worktreePath)
	}
}

func TestRunAgentLaunch_ExportsResolvedWorktree(t *testing.T) {
	const (
		agentID   = "agent-3"
		sessionID = "99999999-8888-7777-6666-555555555555"
		branch    = "feature-codero"
	)
	startedAt := time.Date(2026, 3, 26, 14, 0, 0, 0, time.UTC)
	withDeterministicLaunch(t, sessionID, startedAt)

	repoRoot, worktreePath, _ := setupGitWorktreeRepo(t, "master", branch)
	store, _, cleanup := openTestStore(t)
	defer cleanup()

	exec := &fastExitExecutor{MockExecutor: tmux.NewMockExecutor()}
	cfg := &AgentLaunchConfig{
		AgentID:      agentID,
		RepoPath:     repoRoot,
		Branch:       branch,
		Mode:         "agent",
		AgentCommand: []string{"echo", "hello"},
		WriteLog:     false,
		TmuxExecutor: exec,
	}

	if err := runAgentLaunch(context.Background(), cfg, store); err != nil {
		t.Fatalf("runAgentLaunch: %v", err)
	}
	if len(exec.Respawned) != 1 {
		t.Fatalf("respawn window: expected 1 launch, got %d: %+v", len(exec.Respawned), exec.Respawned)
	}

	launch := exec.Respawned[0].Command
	want := "CODERO_WORKTREE=" + worktreePath
	for _, arg := range launch {
		if arg == want {
			return
		}
	}
	t.Fatalf("respawn-window env missing resolved worktree: got %+v, want %s", launch, want)
}

func TestRunAgentLaunch_CleansUpOnRespawnFailure(t *testing.T) {
	const (
		agentID   = "agent-4"
		sessionID = "77777777-6666-5555-4444-333333333333"
		branch    = "feature-cleanup"
	)
	startedAt := time.Date(2026, 3, 26, 15, 0, 0, 0, time.UTC)
	withDeterministicLaunch(t, sessionID, startedAt)

	repoRoot, _, _ := setupGitWorktreeRepo(t, "master", branch)
	store, db, cleanup := openTestStore(t)
	defer cleanup()

	exec := &fastExitExecutor{MockExecutor: tmux.NewMockExecutor()}
	exec.RespawnErr = errors.New("respawn failed")
	tmuxName := tmux.SessionName(agentID, sessionID)
	cfg := &AgentLaunchConfig{
		AgentID:      agentID,
		RepoPath:     repoRoot,
		Branch:       branch,
		Mode:         "agent",
		AgentCommand: []string{"echo", "hello"},
		WriteLog:     false,
		TmuxExecutor: exec,
	}

	err := runAgentLaunch(context.Background(), cfg, store)
	if err == nil {
		t.Fatal("runAgentLaunch should fail when respawn-window fails")
	}
	if len(exec.KilledNames) != 1 || exec.KilledNames[0] != tmuxName {
		t.Fatalf("tmux cleanup on respawn failure: got %v, want %s", exec.KilledNames, tmuxName)
	}
	row, err := state.GetAgentSession(context.Background(), db, sessionID)
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if row.EndedAt == nil || row.EndReason != "lost" {
		t.Fatalf("session should be finalized as lost after respawn failure, got %+v", row)
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

func setupGitWorktreeRepo(t *testing.T, initBranch, targetBranch string) (repoRoot, worktreePath, resolvedBranch string) {
	t.Helper()

	repoRoot = filepath.Join(t.TempDir(), "repo")
	worktreePath = filepath.Join(t.TempDir(), "agent-checkout")
	resolvedBranch = targetBranch

	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}

	mustGit(t, repoRoot, "init")
	mustGit(t, repoRoot, "config", "user.name", "Codero Test")
	mustGit(t, repoRoot, "config", "user.email", "codero@example.com")
	mustGit(t, repoRoot, "symbolic-ref", "HEAD", "refs/heads/"+initBranch)

	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("root\n"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	mustGit(t, repoRoot, "add", "README.md")
	mustGit(t, repoRoot, "commit", "-m", "init")
	if targetBranch != initBranch {
		mustGit(t, repoRoot, "branch", targetBranch)
	}
	mustGit(t, repoRoot, "worktree", "add", worktreePath, targetBranch)

	return repoRoot, worktreePath, resolvedBranch
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = cleanGitTestEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, string(out))
	}
	return string(out)
}

func cleanGitTestEnv() []string {
	env := os.Environ()
	cleaned := make([]string, 0, len(env))
	for _, kv := range env {
		key := kv
		if i := strings.Index(kv, "="); i >= 0 {
			key = kv[:i]
		}
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		cleaned = append(cleaned, kv)
	}
	return cleaned
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
