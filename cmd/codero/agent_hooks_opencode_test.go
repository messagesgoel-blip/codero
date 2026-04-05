package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateOpenCodePlugin_ContainsHeartbeat(t *testing.T) {
	plugin := generateOpenCodePlugin()
	if !strings.Contains(plugin, "codero session heartbeat") {
		t.Error("plugin does not contain 'codero session heartbeat'")
	}
}

func TestGenerateOpenCodePlugin_ContainsEvents(t *testing.T) {
	plugin := generateOpenCodePlugin()
	for _, event := range []string{"tool.execute.before", "tool.execute.after", "session.idle"} {
		if !strings.Contains(plugin, event) {
			t.Errorf("plugin missing event %q", event)
		}
	}
}

func TestGenerateOpenCodePlugin_ContainsImport(t *testing.T) {
	plugin := generateOpenCodePlugin()
	if strings.Contains(plugin, `node:child_process`) {
		t.Error("plugin should not depend on node:child_process")
	}
}

func TestGenerateOpenCodePlugin_UsesSynchronousFire(t *testing.T) {
	plugin := generateOpenCodePlugin()
	if !strings.Contains(plugin, "await $`cd ${cwd} && bash -lc ${cmd} >/dev/null 2>&1`") {
		t.Fatal("plugin missing plugin-shell heartbeat execution")
	}
	if strings.Contains(plugin, `node:child_process`) {
		t.Fatal("plugin still uses child_process execution")
	}
}

func TestGenerateOpenCodePlugin_BindsHookCWD(t *testing.T) {
	plugin := generateOpenCodePlugin()
	if !strings.Contains(plugin, `async ({ $, directory, worktree }) =>`) {
		t.Fatal("plugin missing plugin shell and directory/worktree context")
	}
	if !strings.Contains(plugin, `const cwd = worktree || directory || process.cwd();`) {
		t.Fatal("plugin missing cwd fallback")
	}
}

func TestGenerateOpenCodePlugin_ContainsManagedComment(t *testing.T) {
	plugin := generateOpenCodePlugin()
	if !strings.Contains(plugin, "managed by codero") {
		t.Error("plugin missing managed-by comment")
	}
}

func TestInstallOpenCodePlugin_Create(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, ".config", "opencode", "plugins", "codero-heartbeat.js")
	legacyPath := filepath.Join(dir, ".config", "opencode", "plugin", "codero-heartbeat.js")

	plugin := generateOpenCodePlugin()
	status, err := installOpenCodeLikePlugin(pluginPath, legacyPath, plugin, false)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if status != "created" {
		t.Errorf("expected 'created', got %q", status)
	}

	data, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "codero session heartbeat") {
		t.Error("written file missing heartbeat command")
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy plugin not created: %v", err)
	}
}

func TestInstallOpenCodePlugin_Idempotent(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, ".config", "opencode", "plugins", "codero-heartbeat.js")
	legacyPath := filepath.Join(dir, ".config", "opencode", "plugin", "codero-heartbeat.js")

	plugin := generateOpenCodePlugin()
	if _, err := installOpenCodeLikePlugin(pluginPath, legacyPath, plugin, false); err != nil {
		t.Fatalf("first install: %v", err)
	}

	status, err := installOpenCodeLikePlugin(pluginPath, legacyPath, plugin, false)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if status != "unchanged" {
		t.Errorf("expected 'unchanged', got %q", status)
	}
}

func TestInstallOpenCodePlugin_Force(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, ".config", "opencode", "plugins", "codero-heartbeat.js")
	legacyPath := filepath.Join(dir, ".config", "opencode", "plugin", "codero-heartbeat.js")

	plugin := generateOpenCodePlugin()
	if _, err := installOpenCodeLikePlugin(pluginPath, legacyPath, plugin, false); err != nil {
		t.Fatalf("first install: %v", err)
	}

	status, err := installOpenCodeLikePlugin(pluginPath, legacyPath, plugin, true)
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if status != "updated" {
		t.Errorf("expected 'updated' with --force, got %q", status)
	}
}

func TestOpenCodePluginPath(t *testing.T) {
	dir := t.TempDir()
	got := openCodePluginPath(dir)
	want := filepath.Join(dir, ".config", "opencode", "plugins", "codero-heartbeat.js")
	if got != want {
		t.Errorf("openCodePluginPath: got %q, want %q", got, want)
	}
}
