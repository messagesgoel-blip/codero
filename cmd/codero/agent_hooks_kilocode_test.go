package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateKiloCodePlugin_ContainsHeartbeat(t *testing.T) {
	plugin := generateKiloCodePlugin()
	if !strings.Contains(plugin, "codero session heartbeat") {
		t.Fatal("plugin missing heartbeat command")
	}
	if !strings.Contains(plugin, "--kind=kilocode") {
		t.Fatal("plugin missing kilocode regenerate hint")
	}
}

func TestKiloCodePluginPath(t *testing.T) {
	dir := t.TempDir()
	got := kiloCodePluginPath(dir)
	want := filepath.Join(dir, ".config", "kilo", "plugins", "codero-heartbeat.js")
	if got != want {
		t.Fatalf("kiloCodePluginPath=%q, want %q", got, want)
	}
}

func TestInstallKiloCodePlugin_Create(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, ".config", "kilo", "plugins", "codero-heartbeat.js")
	legacyPath := filepath.Join(dir, ".config", "kilo", "plugin", "codero-heartbeat.js")

	status, err := installOpenCodeLikePlugin(pluginPath, legacyPath, generateKiloCodePlugin(), false)
	if err != nil {
		t.Fatalf("installTextFile: %v", err)
	}
	if status != "created" {
		t.Fatalf("status=%q, want created", status)
	}

	if _, err := os.Stat(pluginPath); err != nil {
		t.Fatalf("plugin not created: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy plugin not created: %v", err)
	}
}
