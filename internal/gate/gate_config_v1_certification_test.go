package gate_test

// Gate Config v1 Certification Tests
//
// Maps directly to codero_certification_matrix_v1.md §5 acceptance criteria.
// Each test name includes the clause ID for traceability.
//
// Existing tests in config_test.go and heartbeat_test.go provide primary
// coverage; these certification tests add explicit clause-mapped evidence
// where needed and consolidate the acceptance surface.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codero/codero/internal/gate"
)

// ---------------------------------------------------------------------------
// §2.1 — Config file at $HOME/.codero/config.env
// ---------------------------------------------------------------------------

func TestCert_GCv1_S2_1_ConfigPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	got := gate.DefaultConfigFilePath()
	want := filepath.Join(tmpHome, ".codero", "config.env")
	if got != want {
		t.Fatalf("DefaultConfigFilePath: got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// §2.2 — Precedence: env > config file > defaults
// ---------------------------------------------------------------------------

func TestCert_GCv1_S2_2_Precedence_EnvWins(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".codero")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.env")
	if err := os.WriteFile(cfgPath, []byte("CODERO_COPILOT_ENABLED=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clear all gate env vars, then set one to override file.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	t.Setenv("CODERO_COPILOT_ENABLED", "true")

	vars, err := gate.ResolveEffective(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, rv := range vars {
		if rv.EnvVar == "CODERO_COPILOT_ENABLED" {
			if rv.Value != "true" {
				t.Errorf("value: got %q, want true", rv.Value)
			}
			if rv.Source != gate.SourceShellEnv {
				t.Errorf("source: got %v, want SourceShellEnv", rv.Source)
			}
			return
		}
	}
	t.Fatal("CODERO_COPILOT_ENABLED not in resolved vars")
}

func TestCert_GCv1_S2_2_Precedence_FileOverridesDefault(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".codero")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.env")
	if err := os.WriteFile(cfgPath, []byte("CODERO_COPILOT_ENABLED=true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	vars, err := gate.ResolveEffective(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, rv := range vars {
		if rv.EnvVar == "CODERO_COPILOT_ENABLED" {
			if rv.Value != "true" {
				t.Errorf("value: got %q, want true (file override)", rv.Value)
			}
			if rv.Source != gate.SourceConfigFile {
				t.Errorf("source: got %v, want SourceFile", rv.Source)
			}
			return
		}
	}
	t.Fatal("CODERO_COPILOT_ENABLED not found")
}

// ---------------------------------------------------------------------------
// §3 — All 20 spec env vars implemented in Registry
// ---------------------------------------------------------------------------

func TestCert_GCv1_S3_AllSpecVarsPresent(t *testing.T) {
	// Canonical set from codero_gate_config_spec.docx §3.
	specVars := []string{
		"CODERO_COPILOT_ENABLED",
		"CODERO_GEMINI_ENABLED",
		"CODERO_AIDER_ENABLED",
		"CODERO_PRAGENT_ENABLED",
		"CODERO_CODERABBIT_ENABLED",
		"CODERO_VALE_ENABLED",
		"CODERO_HADOLINT_ENABLED",
		"CODERO_SHELLCHECK_ENABLED",
		"CODERO_SQLI_ENABLED",
		"CODERO_GOVET_ENABLED",
		"CODERO_TSC_ENABLED",
		"CODERO_SEMGREP_ENABLED",
		"CODERO_LITELLM_ENABLED",
		"CODERO_AI_QUORUM",
		"CODERO_AI_BUDGET_SECONDS",
		"CODERO_LITELLM_TIMEOUT",
		"CODERO_COPILOT_TIMEOUT",
		"CODERO_AI_MODEL",
		"CODERO_MIN_AI_GATES",
		"CODERO_SKIP_INFRA_FAIL",
	}

	registryMap := make(map[string]bool, len(gate.Registry))
	for _, entry := range gate.Registry {
		registryMap[entry.EnvVar] = true
	}

	for _, v := range specVars {
		if !registryMap[v] {
			t.Errorf("spec var %s missing from Registry", v)
		}
	}
	if len(gate.Registry) != len(specVars) {
		t.Errorf("Registry has %d entries, spec defines %d", len(gate.Registry), len(specVars))
	}
}

// ---------------------------------------------------------------------------
// §4 — Three-tier check model: always-on / configurable / opt-in
// ---------------------------------------------------------------------------

func TestCert_GCv1_S4_ThreeTierModel(t *testing.T) {
	// Always-on: not disableable.
	alwaysOn := gate.AlwaysOnChecks()
	if len(alwaysOn) != 3 {
		t.Fatalf("AlwaysOnChecks: got %d, want 3", len(alwaysOn))
	}
	expected := map[string]bool{"path-guard": true, "gitleaks": true, "ruff": true}
	for _, name := range alwaysOn {
		if !expected[name] {
			t.Errorf("unexpected always-on check: %s", name)
		}
	}

	// Verify tier separation in registry.
	tiers := map[gate.Tier]int{}
	for _, entry := range gate.Registry {
		tiers[entry.Tier]++
	}
	if tiers[gate.TierConfigurable] == 0 {
		t.Error("no TierConfigurable entries")
	}
	if tiers[gate.TierOptIn] == 0 {
		t.Error("no TierOptIn entries")
	}
	if tiers[gate.TierAISetting] == 0 {
		t.Error("no TierAISetting entries")
	}
}

// ---------------------------------------------------------------------------
// §5.2 — Config drift detection (env vs file mismatch)
// ---------------------------------------------------------------------------

func TestCert_GCv1_S5_2_DriftDetection(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".codero")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.env")
	if err := os.WriteFile(cfgPath, []byte("CODERO_AI_QUORUM=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	// Create drift: env says 5, file says 2.
	t.Setenv("CODERO_AI_QUORUM", "5")

	drifts, err := gate.DetectDrifts(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(drifts))
	}
	if drifts[0].EnvVar != "CODERO_AI_QUORUM" {
		t.Errorf("drift var: got %s, want CODERO_AI_QUORUM", drifts[0].EnvVar)
	}
}

// ---------------------------------------------------------------------------
// §7 — AI settings (quorum, budget, model) in registry with correct tiers
// ---------------------------------------------------------------------------

func TestCert_GCv1_S7_AISettingsRegistry(t *testing.T) {
	aiVars := map[string]struct {
		wantDefault string
	}{
		"CODERO_AI_QUORUM":         {wantDefault: "1"},
		"CODERO_AI_BUDGET_SECONDS": {wantDefault: "180"},
		"CODERO_LITELLM_TIMEOUT":   {wantDefault: "45"},
		"CODERO_COPILOT_TIMEOUT":   {wantDefault: "75"},
		"CODERO_AI_MODEL":          {wantDefault: ""},
		"CODERO_MIN_AI_GATES":      {wantDefault: "1"},
	}

	for _, entry := range gate.Registry {
		spec, ok := aiVars[entry.EnvVar]
		if !ok {
			continue
		}
		if entry.Tier != gate.TierAISetting {
			t.Errorf("%s: tier=%v, want TierAISetting", entry.EnvVar, entry.Tier)
		}
		if entry.DefaultValue != spec.wantDefault {
			t.Errorf("%s: default=%q, want %q", entry.EnvVar, entry.DefaultValue, spec.wantDefault)
		}
		delete(aiVars, entry.EnvVar)
	}
	for v := range aiVars {
		t.Errorf("AI setting %s missing from Registry", v)
	}
}

func TestCert_GCv1_S7_BudgetLoadedIntoConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	t.Setenv("CODERO_AI_BUDGET_SECONDS", "300")

	cfg := gate.LoadConfigFrom(filepath.Join(tmpHome, ".codero", "config.env"))
	if cfg.GateTotalTimeoutSec != 300 {
		t.Errorf("GateTotalTimeoutSec: got %d, want 300", cfg.GateTotalTimeoutSec)
	}
}

// ---------------------------------------------------------------------------
// Missing file — safe defaults applied
// ---------------------------------------------------------------------------

func TestCert_GCv1_MissingFile_SafeDefaults(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), ".codero", "config.env")

	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	vars, err := gate.ResolveEffective(nonexistent)
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) != len(gate.Registry) {
		t.Fatalf("expected %d vars, got %d", len(gate.Registry), len(vars))
	}
	for _, rv := range vars {
		if rv.Source != gate.SourceDefault {
			t.Errorf("%s: source=%v, want SourceDefault", rv.EnvVar, rv.Source)
		}
	}
}

// ---------------------------------------------------------------------------
// Malformed file — invalid lines skipped, valid lines parsed
// ---------------------------------------------------------------------------

func TestCert_GCv1_Malformed_SkipInvalidLines(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".codero")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.env")
	content := strings.Join([]string{
		"# comment line",
		"",
		"THIS_HAS_NO_EQUALS",
		"CODERO_COPILOT_ENABLED=true",
		"MALFORMED LINE WITH SPACES",
		"CODERO_AI_QUORUM=3",
	}, "\n")
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	vars, err := gate.ResolveEffective(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, rv := range vars {
		switch rv.EnvVar {
		case "CODERO_COPILOT_ENABLED":
			if rv.Value != "true" || rv.Source != gate.SourceConfigFile {
				t.Errorf("COPILOT: value=%q source=%v, want true/SourceFile", rv.Value, rv.Source)
			}
		case "CODERO_AI_QUORUM":
			if rv.Value != "3" || rv.Source != gate.SourceConfigFile {
				t.Errorf("QUORUM: value=%q source=%v, want 3/SourceFile", rv.Value, rv.Source)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Atomic write — temp + rename pattern
// ---------------------------------------------------------------------------

func TestCert_GCv1_AtomicWrite_Durability(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cfgPath := filepath.Join(tmpHome, ".codero", "config.env")

	// SaveConfigVar creates dir + file if missing.
	if err := gate.SaveConfigVar(cfgPath, "CODERO_AI_QUORUM", "5"); err != nil {
		t.Fatal(err)
	}

	// Verify file exists and contains the variable.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if !strings.Contains(string(data), "CODERO_AI_QUORUM=5") {
		t.Errorf("file missing CODERO_AI_QUORUM=5: %s", string(data))
	}

	// Verify no temp file left behind.
	entries, _ := os.ReadDir(filepath.Dir(cfgPath))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file not cleaned up: %s", e.Name())
		}
	}

	// Write a second var — original must survive.
	if err := gate.SaveConfigVar(cfgPath, "CODERO_COPILOT_ENABLED", "true"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CODERO_AI_QUORUM=5") {
		t.Error("first var lost after second write")
	}
	if !strings.Contains(string(data), "CODERO_COPILOT_ENABLED=true") {
		t.Error("second var not written")
	}
}
