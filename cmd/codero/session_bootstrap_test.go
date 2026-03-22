package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionBootstrapResolveDefaultsToUUID(t *testing.T) {
	worktree := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "codero.yaml")
	t.Setenv("CODERO_AGENT_ID", "agent-alias")
	t.Setenv("CODERO_SESSION_MODE", "agent")
	t.Setenv("CODERO_BASE_URL", "http://127.0.0.1:18181")
	t.Setenv("CODERO_TAILNET_BASE_URL", "http://100.91.230.7:18181")
	t.Setenv("TEST_REPO", "acme/api")
	t.Setenv("TEST_BRANCH", "feat/test")
	t.Setenv("TEST_TASK_ID", "TASK-1")

	cfg := &sessionBootstrapConfig{
		RuntimeRoot: t.TempDir(),
		Worktree:    worktree,
		ConfigPath:  configPath,
	}
	resolved, err := cfg.resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.AgentID != "agent-alias" {
		t.Fatalf("agent_id: got %q", resolved.AgentID)
	}
	if len(resolved.SessionID) != 36 {
		t.Fatalf("session_id should look like UUID, got %q", resolved.SessionID)
	}
}

func TestWriteSessionBootstrapWritesRuntimeNotes(t *testing.T) {
	root := t.TempDir()
	worktree := filepath.Join(t.TempDir(), "worktree")
	configPath := filepath.Join(t.TempDir(), "codero.yaml")
	cfg := &sessionBootstrapConfig{
		SessionID:      "0e22cb0b-80b9-4af7-b824-a6164fefe3cd",
		AgentID:        "opencode-pilot",
		Mode:           "agent",
		Worktree:       worktree,
		RuntimeRoot:    root,
		BaseURL:        "http://127.0.0.1:18181",
		TailnetBaseURL: "http://100.91.230.7:18181",
		ConfigPath:     configPath,
		Repo:           "acme/api",
		Branch:         "feat/test",
		TaskID:         "TASK-1",
	}

	result, err := writeSessionBootstrap(cfg)
	if err != nil {
		t.Fatalf("writeSessionBootstrap: %v", err)
	}

	for _, path := range []string{result.RuntimeAgentMD, result.RuntimeSessionMD} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("runtime note missing: %s (%v)", path, err)
		}
	}

	agentBody, err := os.ReadFile(result.RuntimeAgentMD)
	if err != nil {
		t.Fatalf("read AGENT.md: %v", err)
	}
	if !strings.Contains(string(agentBody), "already claimed and registered for this window") {
		t.Fatalf("AGENT.md missing claimed wording: %s", string(agentBody))
	}
	if !strings.Contains(string(agentBody), "session confirm") {
		t.Fatalf("AGENT.md missing session confirm command: %s", string(agentBody))
	}

	sessionBody, err := os.ReadFile(result.RuntimeSessionMD)
	if err != nil {
		t.Fatalf("read SESSION.md: %v", err)
	}
	if !strings.Contains(string(sessionBody), "CODERO_SESSION_ID=0e22cb0b-80b9-4af7-b824-a6164fefe3cd") {
		t.Fatalf("SESSION.md missing session id: %s", string(sessionBody))
	}
	if !strings.Contains(string(sessionBody), "session confirm") {
		t.Fatalf("SESSION.md missing session confirm command: %s", string(sessionBody))
	}
	if got := result.Exports["CODERO_RUNTIME_DIR"]; got != filepath.Join(root, cfg.SessionID) {
		t.Fatalf("runtime dir export: got %q", got)
	}
}
