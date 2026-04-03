package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateCodexHooks_Structure(t *testing.T) {
	hooks := generateCodexHooks()

	data, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}

	hooksSection, ok := parsed["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'hooks' key")
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop"} {
		if _, ok := hooksSection[event]; !ok {
			t.Errorf("missing event %q in hooks", event)
		}
	}
}

func TestGenerateCodexHooks_ContainsHeartbeat(t *testing.T) {
	hooks := generateCodexHooks()
	data, _ := json.Marshal(hooks)
	s := string(data)
	if len(s) < 100 {
		t.Error("hooks JSON suspiciously short")
	}
	// The heartbeat command should reference codero session heartbeat.
	if !contains(s, "codero session heartbeat") {
		t.Error("hooks JSON does not contain 'codero session heartbeat'")
	}
}

func TestInstallCodexHooks_Create(t *testing.T) {
	dir := t.TempDir()
	hooksPath := filepath.Join(dir, ".codex", "hooks.json")

	hooks := generateCodexHooks()
	status, err := installStandaloneJSON(hooksPath, hooks, false)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if status != "created" {
		t.Errorf("expected 'created', got %q", status)
	}

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := got["hooks"]; !ok {
		t.Error("missing 'hooks' key in written file")
	}
}

func TestInstallCodexHooks_Idempotent(t *testing.T) {
	dir := t.TempDir()
	hooksPath := filepath.Join(dir, ".codex", "hooks.json")

	hooks := generateCodexHooks()
	if _, err := installStandaloneJSON(hooksPath, hooks, false); err != nil {
		t.Fatalf("first install: %v", err)
	}

	status, err := installStandaloneJSON(hooksPath, hooks, false)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if status != "unchanged" {
		t.Errorf("expected 'unchanged', got %q", status)
	}
}

func TestInstallCodexHooks_Force(t *testing.T) {
	dir := t.TempDir()
	hooksPath := filepath.Join(dir, ".codex", "hooks.json")

	hooks := generateCodexHooks()
	if _, err := installStandaloneJSON(hooksPath, hooks, false); err != nil {
		t.Fatalf("first install: %v", err)
	}

	status, err := installStandaloneJSON(hooksPath, hooks, true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if status != "updated" {
		t.Errorf("expected 'updated' with --force, got %q", status)
	}
}

func TestCodexHooksPath(t *testing.T) {
	dir := t.TempDir()
	got := codexHooksPath(dir)
	want := filepath.Join(dir, ".codex", "hooks.json")
	if got != want {
		t.Errorf("codexHooksPath: got %q, want %q", got, want)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
