package gatecheck_test

import (
	"testing"
	"time"

	"github.com/codero/codero/internal/gatecheck"
)

func TestLoadEngineConfig_EmptyOverrides(t *testing.T) {
	t.Setenv("CODERO_TOOL_SEMGREP", "")
	t.Setenv("CODERO_GATES_PROFILE", "")
	cfg := gatecheck.LoadEngineConfig()
	if cfg.SemgrepPath != "" {
		t.Errorf("SemgrepPath: got %q, want empty string", cfg.SemgrepPath)
	}
	if cfg.Profile != gatecheck.ProfilePortable {
		t.Errorf("Profile default on empty: got %q, want portable", cfg.Profile)
	}
}

func TestLoadEngineConfig_InvalidNumericDefaults(t *testing.T) {
	t.Setenv("CODERO_MAX_INFRA_BYPASS_GATES", "-1")
	t.Setenv("CODERO_GATE_TIMEOUT", "nope")
	t.Setenv("CODERO_MAX_STAGED_FILE_BYTES", "bad")
	cfg := gatecheck.LoadEngineConfig()
	if cfg.MaxInfraBypass != 2 {
		t.Errorf("MaxInfraBypass invalid: got %d, want 2", cfg.MaxInfraBypass)
	}
	if cfg.GateTimeout != 120*time.Second {
		t.Errorf("GateTimeout invalid: got %v, want 120s", cfg.GateTimeout)
	}
	if cfg.MaxStagedFileBytes != 1048576 {
		t.Errorf("MaxStagedFileBytes invalid: got %d, want 1048576", cfg.MaxStagedFileBytes)
	}
}

func TestLoadEngineConfig_ZeroAllowed(t *testing.T) {
	t.Setenv("CODERO_MAX_INFRA_BYPASS_GATES", "0")
	t.Setenv("CODERO_GATE_TIMEOUT", "0")
	t.Setenv("CODERO_MAX_STAGED_FILE_BYTES", "0")
	cfg := gatecheck.LoadEngineConfig()
	if cfg.MaxInfraBypass != 0 {
		t.Errorf("MaxInfraBypass zero: got %d, want 0", cfg.MaxInfraBypass)
	}
	if cfg.GateTimeout != 0 {
		t.Errorf("GateTimeout zero: got %v, want 0", cfg.GateTimeout)
	}
	if cfg.MaxStagedFileBytes != 0 {
		t.Errorf("MaxStagedFileBytes zero: got %d, want 0", cfg.MaxStagedFileBytes)
	}
}

func TestLoadEngineConfig_InvalidBoolDefaults(t *testing.T) {
	t.Setenv("CODERO_ALLOW_REQUIRED_SKIP", "nope")
	cfg := gatecheck.LoadEngineConfig()
	if cfg.AllowRequiredSkip {
		t.Error("AllowRequiredSkip invalid should be false")
	}
}

func TestLoadEngineConfig_FastProfileAlias(t *testing.T) {
	t.Setenv("CODERO_GATES_PROFILE", "fast")
	cfg := gatecheck.LoadEngineConfig()
	if cfg.Profile != gatecheck.ProfilePortable {
		t.Errorf("Profile fast alias: got %q, want portable", cfg.Profile)
	}
}
