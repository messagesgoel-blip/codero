package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCopilotHooks_ContainsLifecycleEvents(t *testing.T) {
	hooks := generateCopilotHooks()
	for _, event := range []string{"sessionStart", "userPromptSubmitted", "preToolUse", "postToolUse", "errorOccurred", "sessionEnd"} {
		if _, ok := hooks[event]; !ok {
			t.Fatalf("copilot hooks missing event %q", event)
		}
	}
}

func TestGenerateCopilotHooks_ContainsHeartbeatCommand(t *testing.T) {
	raw, err := json.Marshal(generateCopilotHooks())
	if err != nil {
		t.Fatalf("marshal hooks: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "codero session heartbeat") {
		t.Fatal("copilot hooks missing heartbeat command")
	}
	if !strings.Contains(text, "--tool-calls=") {
		t.Fatal("copilot hooks missing tool call counter")
	}
}

func TestInstallCopilotHooks_PreservesConfigJSONC(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".copilot", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := `{
  // existing user setting
  "theme": "dark",
  "stream": true,
}`
	if err := os.WriteFile(configPath, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	status, err := installMergedJSONConfig(configPath, map[string]interface{}{"hooks": generateCopilotHooks()}, false, true)
	if err != nil {
		t.Fatalf("installMergedJSONConfig: %v", err)
	}
	if status != "updated" {
		t.Fatalf("status=%q, want updated", status)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if got["theme"] != "dark" {
		t.Fatalf("theme=%v, want dark", got["theme"])
	}
	if _, ok := got["hooks"]; !ok {
		t.Fatal("missing hooks key after install")
	}
}

func TestCopilotConfigPath(t *testing.T) {
	dir := t.TempDir()
	got := copilotConfigPath(dir)
	want := filepath.Join(dir, ".copilot", "config.json")
	if got != want {
		t.Fatalf("copilotConfigPath=%q, want %q", got, want)
	}
}
