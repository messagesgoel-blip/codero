package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestInstallClaudeHooks_Create verifies fresh install into a non-existent
// settings file returns "created" and writes a valid JSON file.
func TestInstallClaudeHooks_Create(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")

	hooks := generateClaudeHooks()
	status, err := installClaudeHooks(settingsPath, hooks, false)
	if err != nil {
		t.Fatalf("installClaudeHooks: %v", err)
	}
	if status != "created" {
		t.Errorf("expected 'created', got %q", status)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings file not found after install: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("settings file is not valid JSON: %v", err)
	}
	if _, ok := got["hooks"]; !ok {
		t.Error("settings file missing 'hooks' key")
	}
}

// TestInstallClaudeHooks_Idempotent verifies that a second install with
// identical hooks returns "unchanged" and does not rewrite the file.
func TestInstallClaudeHooks_Idempotent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")

	hooks := generateClaudeHooks()

	if _, err := installClaudeHooks(settingsPath, hooks, false); err != nil {
		t.Fatalf("first install: %v", err)
	}
	firstInfo, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("stat after first install: %v", err)
	}
	firstMtime := firstInfo.ModTime()

	status, err := installClaudeHooks(settingsPath, hooks, false)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if status != "unchanged" {
		t.Errorf("expected 'unchanged' on second install, got %q", status)
	}

	secondInfo, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("stat after second install: %v", err)
	}
	if !secondInfo.ModTime().Equal(firstMtime) {
		t.Error("settings file was rewritten on idempotent call (mtime changed)")
	}
}

// TestInstallClaudeHooks_Force verifies that --force reinstalls even when
// hooks are already identical, returning "updated".
func TestInstallClaudeHooks_Force(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")

	hooks := generateClaudeHooks()

	if _, err := installClaudeHooks(settingsPath, hooks, false); err != nil {
		t.Fatalf("first install: %v", err)
	}

	status, err := installClaudeHooks(settingsPath, hooks, true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if status != "updated" {
		t.Errorf("expected 'updated' with --force on existing file, got %q", status)
	}
}

// TestInstallClaudeHooks_PreservesOtherKeys verifies that keys already in
// settings.json that are not related to hooks are preserved after install.
func TestInstallClaudeHooks_PreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	existing := map[string]interface{}{
		"someOtherKey": "someValue",
		"anotherKey":   42,
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatalf("write existing settings: %v", err)
	}

	hooks := generateClaudeHooks()
	if _, err := installClaudeHooks(settingsPath, hooks, false); err != nil {
		t.Fatalf("installClaudeHooks: %v", err)
	}

	result, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings after install: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if got["someOtherKey"] != "someValue" {
		t.Errorf("someOtherKey not preserved; got %v", got["someOtherKey"])
	}
	if _, ok := got["hooks"]; !ok {
		t.Error("hooks key missing after install")
	}
}

// TestInstallClaudeHooks_CreatesDir verifies that installClaudeHooks creates
// the parent directory (e.g. ~/.claude/) if it does not already exist.
func TestInstallClaudeHooks_CreatesDir(t *testing.T) {
	base := t.TempDir()
	settingsPath := filepath.Join(base, "deep", "nested", "dir", "settings.json")

	hooks := generateClaudeHooks()
	status, err := installClaudeHooks(settingsPath, hooks, false)
	if err != nil {
		t.Fatalf("installClaudeHooks with missing dir: %v", err)
	}
	if status != "created" {
		t.Errorf("expected 'created', got %q", status)
	}
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("settings file not created: %v", err)
	}
}

// TestGenerateClaudeHooks_Structure validates that the generated hooks map
// contains the required PreToolUse, PostToolUse, and Notification entries.
func TestGenerateClaudeHooks_Structure(t *testing.T) {
	hooks := generateClaudeHooks()

	hooksSection, ok := hooks["hooks"]
	if !ok {
		t.Fatal("generateClaudeHooks missing top-level 'hooks' key")
	}

	hooksMap, ok := hooksSection.(map[string]interface{})
	if !ok {
		t.Fatalf("hooks section is not a map; got %T", hooksSection)
	}

	for _, required := range []string{"PreToolUse", "PostToolUse", "Notification"} {
		if _, ok := hooksMap[required]; !ok {
			t.Errorf("hooks missing required key %q", required)
		}
	}
}
