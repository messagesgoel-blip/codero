package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshAgentRegistry_BuildsRegistryFromShimAndWrapper(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}

	realBin := filepath.Join(dir, "real-claude")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	shimPath := filepath.Join(binDir, "claude-team")
	shim := `#!/usr/bin/env bash
exec codero agent run --agent-id claude -- "` + realBin + `" "$@"
`
	if err := os.WriteFile(shimPath, []byte(shim), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	shimPath2 := filepath.Join(binDir, "cc")
	if err := os.WriteFile(shimPath2, []byte(shim), 0o755); err != nil {
		t.Fatalf("write second shim: %v", err)
	}

	uc := &UserConfig{
		Version:    1,
		DaemonAddr: "127.0.0.1:8110",
		Wrappers: map[string]WrapperConfig{
			"claude": {
				AgentKind:         AgentKindClaude,
				RealBinary:        realBin,
				DisplayName:       "Claude Code",
				Aliases:           []string{"claude-code"},
				AuthMode:          "subscription",
				HomeStrategy:      "isolated",
				HomeDir:           filepath.Join(dir, "profiles", "claude"),
				ConfigStrategy:    "home",
				ConfigPath:        filepath.Join(dir, "profiles", "claude", "settings.json"),
				PermissionProfile: "strict",
				DefaultArgs:       []string{"--model", "sonnet"},
				EnvVars:           map[string]string{"FOO": "bar"},
			},
		},
		DisabledAgents: []string{"claude"},
	}

	now := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)
	agents, err := uc.RefreshAgentRegistry(now)
	if err != nil {
		t.Fatalf("RefreshAgentRegistry: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(agents)=%d, want 1", len(agents))
	}
	got := agents[0]
	if got.AgentID != "claude" {
		t.Fatalf("AgentID=%q, want claude", got.AgentID)
	}
	if got.AgentKind != AgentKindClaude {
		t.Fatalf("AgentKind=%q, want %q", got.AgentKind, AgentKindClaude)
	}
	if got.DisplayName != "Claude Code" {
		t.Fatalf("DisplayName=%q, want Claude Code", got.DisplayName)
	}
	if got.PrimaryAlias != "claude" {
		t.Fatalf("PrimaryAlias=%q, want claude", got.PrimaryAlias)
	}
	if got.ShimName != "claude-team" && got.ShimName != "cc" && got.ShimName != "claude" {
		t.Fatalf("ShimName=%q unexpected", got.ShimName)
	}
	if !got.Installed {
		t.Fatal("Installed=false, want true")
	}
	if !got.Disabled {
		t.Fatal("Disabled=false, want true")
	}
	if got.EnvVars["FOO"] != "bar" {
		t.Fatalf("EnvVars[FOO]=%q, want bar", got.EnvVars["FOO"])
	}
	if got.AuthMode != "subscription" {
		t.Fatalf("AuthMode=%q, want subscription", got.AuthMode)
	}
	if got.HomeStrategy != "isolated" {
		t.Fatalf("HomeStrategy=%q, want isolated", got.HomeStrategy)
	}
	if got.HomeDir == "" {
		t.Fatal("HomeDir empty")
	}
	if got.ConfigStrategy != "home" {
		t.Fatalf("ConfigStrategy=%q, want home", got.ConfigStrategy)
	}
	if got.ConfigPath == "" {
		t.Fatal("ConfigPath empty")
	}
	if got.PermissionProfile != "strict" {
		t.Fatalf("PermissionProfile=%q, want strict", got.PermissionProfile)
	}
	if len(got.DefaultArgs) != 2 || got.DefaultArgs[0] != "--model" || got.DefaultArgs[1] != "sonnet" {
		t.Fatalf("DefaultArgs=%v, want [--model sonnet]", got.DefaultArgs)
	}
	assertAlias(t, got.Aliases, "claude")
	assertAlias(t, got.Aliases, "claude-team")
	assertAlias(t, got.Aliases, "claude-code")
	assertAlias(t, got.Aliases, "cc")
	assertAlias(t, got.ShimNames, "claude-team")
	assertAlias(t, got.ShimNames, "cc")
	if uc.Registry.LastScan.IsZero() {
		t.Fatal("Registry.LastScan not set")
	}
}

func TestLoadUserConfigWithRegistry_RefreshesWhenStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	realBin := filepath.Join(dir, "real-codex")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	shimPath := filepath.Join(binDir, "codex")
	shim := `#!/usr/bin/env bash
exec codero agent run --agent-id codex -- "` + realBin + `" "$@"
`
	if err := os.WriteFile(shimPath, []byte(shim), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}

	uc := &UserConfig{
		Version:    1,
		DaemonAddr: "127.0.0.1:8110",
		Registry: AgentRegistry{
			LastScan: time.Now().UTC().Add(-48 * time.Hour),
		},
	}
	if err := uc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	gotUC, agents, err := LoadUserConfigWithRegistry(24 * time.Hour)
	if err != nil {
		t.Fatalf("LoadUserConfigWithRegistry: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(agents)=%d, want 1", len(agents))
	}
	if agents[0].AgentID != "codex" {
		t.Fatalf("AgentID=%q, want codex", agents[0].AgentID)
	}
	if gotUC.Registry.LastScan.IsZero() {
		t.Fatal("Registry.LastScan not refreshed")
	}
	if agents[0].AgentKind != AgentKindCodex {
		t.Fatalf("AgentKind=%q, want %q", agents[0].AgentKind, AgentKindCodex)
	}
}

func TestLoadUserConfigWithFreshRegistry_RefreshesEvenWhenRecent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	uc := &UserConfig{
		Version: 1,
		Registry: AgentRegistry{
			LastScan: time.Now().UTC(),
			Agents: map[string]RegisteredAgent{
				"ghost": {
					AgentID:   "ghost",
					Aliases:   []string{"ghost", "g"},
					ShimNames: []string{"ghost", "g"},
				},
			},
		},
	}
	if err := uc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	gotUC, agents, err := LoadUserConfigWithFreshRegistry()
	if err != nil {
		t.Fatalf("LoadUserConfigWithFreshRegistry: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("len(agents)=%d, want 0", len(agents))
	}
	if len(gotUC.Registry.Agents) != 0 {
		t.Fatalf("registry agents=%v, want empty", gotUC.Registry.Agents)
	}
}

func TestRefreshAgentRegistry_RemovesRegistryOnlyEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	uc := &UserConfig{
		Version: 1,
		Registry: AgentRegistry{
			LastScan: time.Now().UTC().Add(-48 * time.Hour),
			Agents: map[string]RegisteredAgent{
				"ghost": {
					AgentID:   "ghost",
					Aliases:   []string{"ghost", "g"},
					ShimNames: []string{"ghost", "g"},
				},
			},
		},
	}

	agents, err := uc.RefreshAgentRegistry(time.Now().UTC())
	if err != nil {
		t.Fatalf("RefreshAgentRegistry: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("len(agents)=%d, want 0", len(agents))
	}
	if len(uc.Registry.Agents) != 0 {
		t.Fatalf("registry agents=%v, want empty", uc.Registry.Agents)
	}
}

func TestRegistryStale_EmptyRegistryAfterScanIsFresh(t *testing.T) {
	now := time.Now().UTC()
	uc := &UserConfig{
		Registry: AgentRegistry{
			LastScan: now,
			Agents:   map[string]RegisteredAgent{},
		},
	}

	if uc.RegistryStale(now.Add(time.Hour), 24*time.Hour) {
		t.Fatal("RegistryStale should treat a recent empty registry as fresh")
	}
}

func TestRefreshAgentRegistry_ClearsRemovedMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	realBin := filepath.Join(t.TempDir(), "real-custom")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write real binary: %v", err)
	}

	now := time.Now().UTC()
	uc := &UserConfig{
		Version: 1,
		Wrappers: map[string]WrapperConfig{
			"ghost": {
				RealBinary:  realBin,
				Aliases:     []string{},
				DefaultArgs: []string{},
				EnvVars:     map[string]string{},
			},
		},
		Registry: AgentRegistry{
			LastScan: now.Add(-48 * time.Hour),
			Agents: map[string]RegisteredAgent{
				"ghost": {
					AgentID:      "ghost",
					Aliases:      []string{"ghost", "ghost-old", "old-shim", "oldbin"},
					ShimName:     "old-shim",
					ShimNames:    []string{"old-shim"},
					RealBinary:   "stale/oldbin",
					DefaultArgs:  []string{"--stale"},
					EnvVars:      map[string]string{"OLD": "1"},
					PrimaryAlias: "ghost-old",
				},
			},
		},
	}

	agents, err := uc.RefreshAgentRegistry(now)
	if err != nil {
		t.Fatalf("RefreshAgentRegistry: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(agents)=%d, want 1", len(agents))
	}
	got := agents[0]
	if len(got.ShimNames) != 0 {
		t.Fatalf("ShimNames=%v, want empty", got.ShimNames)
	}
	if got.ShimName != "" {
		t.Fatalf("ShimName=%q, want empty", got.ShimName)
	}
	if len(got.DefaultArgs) != 0 {
		t.Fatalf("DefaultArgs=%v, want empty", got.DefaultArgs)
	}
	if len(got.EnvVars) != 0 {
		t.Fatalf("EnvVars=%v, want empty", got.EnvVars)
	}
	assertNoAlias(t, got.Aliases, "ghost-old")
	assertNoAlias(t, got.Aliases, "old-shim")
	assertNoAlias(t, got.Aliases, "oldbin")
}

func TestInferAgentKind(t *testing.T) {
	tests := []struct {
		name       string
		agentID    string
		realBinary string
		want       string
	}{
		{name: "profile prefix", agentID: "claude-pro", want: AgentKindClaude},
		{name: "binary basename", realBinary: "/usr/local/bin/copilot", want: AgentKindCopilot},
		{name: "exact kind", agentID: "gemini", want: AgentKindGemini},
		{name: "unknown", agentID: "custom-agent", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InferAgentKind(tt.agentID, tt.realBinary); got != tt.want {
				t.Fatalf("InferAgentKind(%q, %q)=%q, want %q", tt.agentID, tt.realBinary, got, tt.want)
			}
		})
	}
}

func assertAlias(t *testing.T, aliases []string, want string) {
	t.Helper()
	for _, alias := range aliases {
		if alias == want {
			return
		}
	}
	t.Fatalf("aliases=%v, missing %q", aliases, want)
}

func assertNoAlias(t *testing.T, aliases []string, forbidden string) {
	t.Helper()
	for _, alias := range aliases {
		if alias == forbidden {
			t.Fatalf("aliases=%v, unexpectedly contain %q", aliases, forbidden)
		}
	}
}
