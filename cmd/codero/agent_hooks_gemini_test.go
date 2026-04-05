package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateGeminiSettings_ContainsExpectedEvents(t *testing.T) {
	settings := generateGeminiSettings()
	rawHooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("hooks section missing or wrong type: %T", settings["hooks"])
	}
	for _, event := range []string{"SessionStart", "BeforeTool", "AfterTool", "AfterAgent", "Notification"} {
		if _, ok := rawHooks[event]; !ok {
			t.Fatalf("gemini hooks missing event %q", event)
		}
	}
}

func TestGenerateGeminiSettings_ContainsJSONSafeCommand(t *testing.T) {
	raw, err := json.Marshal(generateGeminiSettings())
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "codero session heartbeat") {
		t.Fatal("gemini hooks missing heartbeat command")
	}
	if !strings.Contains(text, "printf") || !strings.Contains(text, "{}") {
		t.Fatal("gemini hooks missing JSON stdout wrapper")
	}
}

func TestInstallGeminiHooks_PreservesOtherKeys(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	seed := map[string]interface{}{"theme": "dark"}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	status, err := installMergedJSONConfig(settingsPath, generateGeminiSettings(), false, false)
	if err != nil {
		t.Fatalf("installMergedJSONConfig: %v", err)
	}
	if status != "updated" {
		t.Fatalf("status=%q, want updated", status)
	}

	result, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if got["theme"] != "dark" {
		t.Fatalf("theme=%v, want dark", got["theme"])
	}
	if _, ok := got["hooks"]; !ok {
		t.Fatal("missing hooks key after install")
	}
}

func TestGeminiSettingsPath(t *testing.T) {
	dir := t.TempDir()
	got := geminiSettingsPath(dir)
	want := filepath.Join(dir, ".gemini", "settings.json")
	if got != want {
		t.Fatalf("geminiSettingsPath=%q, want %q", got, want)
	}
}
