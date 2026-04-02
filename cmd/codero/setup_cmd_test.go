package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codero/codero/internal/config"
)

// TestInstallShim_Create verifies that installShim writes a valid shim script
// and records the wrapper config on first install.
func TestInstallShim_Create(t *testing.T) {
	shimDir := t.TempDir()
	realBin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\nexec real-claude \"$@\"\n"), 0o755); err != nil {
		t.Fatalf("create fake binary: %v", err)
	}

	uc := &config.UserConfig{Wrappers: make(map[string]config.WrapperConfig)}
	result, err := installShim(shimDir, "claude", "claude", realBin, uc, false)
	if err != nil {
		t.Fatalf("installShim: %v", err)
	}
	if result != "created" {
		t.Errorf("expected 'created', got %q", result)
	}

	// Verify shim script exists and is executable.
	shimPath := filepath.Join(shimDir, "claude")
	info, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("shim not found: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("shim is not executable")
	}

	// Verify shim content contains expected codero agent run invocation.
	data, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("read shim: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "codero agent run --agent-id claude") {
		t.Errorf("shim missing expected invocation; got:\n%s", content)
	}
	if !strings.Contains(content, realBin) {
		t.Errorf("shim missing real binary path %q; got:\n%s", realBin, content)
	}

	// Verify wrapper entry was recorded.
	w, ok := uc.Wrappers["claude"]
	if !ok {
		t.Fatal("wrapper config not recorded in UserConfig")
	}
	if w.RealBinary != realBin {
		t.Errorf("wrapper real_binary = %q, want %q", w.RealBinary, realBin)
	}
	if w.AgentKind != "claude" {
		t.Errorf("wrapper agent_kind = %q, want %q", w.AgentKind, "claude")
	}
}

// TestInstallShim_Idempotent verifies that re-running installShim with the
// same binary and no --force returns "unchanged" and does not mutate state.
func TestInstallShim_Idempotent(t *testing.T) {
	shimDir := t.TempDir()
	realBin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake binary: %v", err)
	}

	uc := &config.UserConfig{Wrappers: make(map[string]config.WrapperConfig)}

	// First install.
	if _, err := installShim(shimDir, "claude", "claude", realBin, uc, false); err != nil {
		t.Fatalf("first installShim: %v", err)
	}
	firstMtime := shimMtime(t, filepath.Join(shimDir, "claude"))
	firstWrapper := uc.Wrappers["claude"]

	// Second install (same binary, no force).
	result, err := installShim(shimDir, "claude", "claude", realBin, uc, false)
	if err != nil {
		t.Fatalf("second installShim: %v", err)
	}
	if result != "unchanged" {
		t.Errorf("expected 'unchanged' on second install, got %q", result)
	}

	// Shim file should not have been rewritten.
	if got := shimMtime(t, filepath.Join(shimDir, "claude")); got != firstMtime {
		t.Error("shim was rewritten on idempotent call (mtime changed)")
	}
	secondWrapper := uc.Wrappers["claude"]
	if !secondWrapper.InstalledAt.Equal(firstWrapper.InstalledAt) {
		t.Error("wrapper metadata changed on idempotent call")
	}
}

// TestInstallShim_UpdateOnBinaryChange verifies that installShim returns
// "updated" when the real binary path changes.
func TestInstallShim_UpdateOnBinaryChange(t *testing.T) {
	shimDir := t.TempDir()
	binDir := t.TempDir()

	realBin1 := filepath.Join(binDir, "claude-v1")
	realBin2 := filepath.Join(binDir, "claude-v2")
	for _, p := range []string{realBin1, realBin2} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("create fake binary: %v", err)
		}
	}

	uc := &config.UserConfig{Wrappers: make(map[string]config.WrapperConfig)}

	if _, err := installShim(shimDir, "claude", "claude", realBin1, uc, false); err != nil {
		t.Fatalf("first installShim: %v", err)
	}

	result, err := installShim(shimDir, "claude", "claude", realBin2, uc, false)
	if err != nil {
		t.Fatalf("second installShim: %v", err)
	}
	if result != "updated" {
		t.Errorf("expected 'updated' after binary change, got %q", result)
	}

	// Verify shim content now points to the new binary.
	data, err := os.ReadFile(filepath.Join(shimDir, "claude"))
	if err != nil {
		t.Fatalf("read shim: %v", err)
	}
	if !strings.Contains(string(data), realBin2) {
		t.Errorf("shim still references old binary after update; got:\n%s", string(data))
	}
}

// TestInstallShim_Force verifies that --force overwrites an existing shim even
// when the binary is unchanged.
func TestInstallShim_Force(t *testing.T) {
	shimDir := t.TempDir()
	realBin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake binary: %v", err)
	}

	uc := &config.UserConfig{Wrappers: make(map[string]config.WrapperConfig)}
	if _, err := installShim(shimDir, "claude", "claude", realBin, uc, false); err != nil {
		t.Fatalf("first installShim: %v", err)
	}

	result, err := installShim(shimDir, "claude", "claude", realBin, uc, true)
	if err != nil {
		t.Fatalf("force installShim: %v", err)
	}
	// With --force the shim is rewritten; result is "updated" since it existed before.
	if result != "updated" {
		t.Errorf("expected 'updated' with --force on existing shim, got %q", result)
	}
}

// TestInstallShim_EmptyProfileID verifies that an empty profile ID is rejected.
func TestInstallShim_EmptyProfileID(t *testing.T) {
	shimDir := t.TempDir()
	uc := &config.UserConfig{Wrappers: make(map[string]config.WrapperConfig)}
	_, err := installShim(shimDir, "claude", "", "/usr/bin/claude", uc, false)
	if err == nil {
		t.Error("expected error for empty profile ID, got nil")
	}
}

// TestInstallShim_ShimTemplate verifies the generated shim matches the
// canonical template used by alias registration (SET-001).
func TestInstallShim_ShimTemplate(t *testing.T) {
	shimDir := t.TempDir()
	binDir := t.TempDir()
	realBin := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("create fake binary: %v", err)
	}

	uc := &config.UserConfig{Wrappers: make(map[string]config.WrapperConfig)}
	if _, err := installShim(shimDir, "opencode", "opencode", realBin, uc, false); err != nil {
		t.Fatalf("installShim: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(shimDir, "opencode"))
	if err != nil {
		t.Fatalf("read shim: %v", err)
	}
	content := string(data)

	// Must be a valid bash shim.
	if !strings.HasPrefix(content, "#!/usr/bin/env bash") {
		t.Errorf("shim missing bash shebang; got first line: %q", firstLine(content))
	}
	// Must include the canonical "do not edit" comment.
	if !strings.Contains(content, "do not edit (managed by codero setup)") {
		t.Error("shim missing 'do not edit' managed-by comment")
	}
	// Must exec through codero agent run.
	if !strings.Contains(content, "exec codero agent run --agent-id opencode") {
		t.Error("shim missing canonical exec invocation")
	}
}

// shimMtime returns the modification time of a shim file as int64 nanoseconds.
func shimMtime(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("shimMtime stat %s: %v", path, err)
	}
	return info.ModTime().UnixNano()
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}
