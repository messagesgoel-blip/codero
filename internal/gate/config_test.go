package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codero/codero/internal/gate"
)

// ─── Config File Parsing ─────────────────────────────────────────────────

func TestParseConfigFile_MissingFile(t *testing.T) {
	m, err := gate.ParseConfigFile(filepath.Join(t.TempDir(), "nonexistent-codero-test-config.env"))
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil map for missing file, got: %v", m)
	}
}

func TestParseConfigFile_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")

	content := `# This is a comment
CODERO_COPILOT_ENABLED=true
CODERO_GOVET_ENABLED=false

# Another comment
CODERO_AI_QUORUM=2
CODERO_AI_MODEL=claude-3-5-sonnet
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := gate.ParseConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"CODERO_COPILOT_ENABLED": "true",
		"CODERO_GOVET_ENABLED":   "false",
		"CODERO_AI_QUORUM":       "2",
		"CODERO_AI_MODEL":        "claude-3-5-sonnet",
	}
	for k, want := range checks {
		if got := m[k]; got != want {
			t.Errorf("%s: got %q, want %q", k, got, want)
		}
	}
}

func TestParseConfigFile_BlankLinesAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")

	content := `# comment
   # indented comment

	
CODERO_COPILOT_ENABLED=false
no-equals-sign
also no equals
CODERO_AI_QUORUM=1
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := gate.ParseConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(m) != 2 {
		t.Errorf("expected 2 entries, got %d: %v", len(m), m)
	}
	if m["CODERO_COPILOT_ENABLED"] != "false" {
		t.Errorf("COPILOT: got %q", m["CODERO_COPILOT_ENABLED"])
	}
	if m["CODERO_AI_QUORUM"] != "1" {
		t.Errorf("QUORUM: got %q", m["CODERO_AI_QUORUM"])
	}
}

// ─── Precedence Tests ────────────────────────────────────────────────────

func TestResolveEffective_DefaultsWhenNoFileNoEnv(t *testing.T) {
	// Point at a nonexistent file, ensure no env vars are set.
	path := filepath.Join(t.TempDir(), "nonexistent.env")

	// Clear all registry env vars to avoid test env pollution.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	vars, err := gate.ResolveEffective(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, rv := range vars {
		if rv.Source != gate.SourceDefault {
			t.Errorf("%s: expected source=default, got %s (value=%q)", rv.EnvVar, rv.Source, rv.Value)
		}
		if rv.Value != rv.DefaultValue {
			t.Errorf("%s: value %q != default %q", rv.EnvVar, rv.Value, rv.DefaultValue)
		}
	}
}

func TestResolveEffective_FileOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")

	content := "CODERO_COPILOT_ENABLED=true\nCODERO_AI_QUORUM=3\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clear env to ensure file is the only override.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	vars, err := gate.ResolveEffectiveMap(path)
	if err != nil {
		t.Fatal(err)
	}

	// COPILOT should come from file with value true.
	copilot := vars["CODERO_COPILOT_ENABLED"]
	if copilot.Value != "true" {
		t.Errorf("COPILOT_ENABLED: got %q, want true", copilot.Value)
	}
	if copilot.Source != gate.SourceConfigFile {
		t.Errorf("COPILOT_ENABLED source: got %s, want config_file", copilot.Source)
	}

	// AI_QUORUM should come from file with value 3.
	quorum := vars["CODERO_AI_QUORUM"]
	if quorum.Value != "3" {
		t.Errorf("AI_QUORUM: got %q, want 3", quorum.Value)
	}
	if quorum.Source != gate.SourceConfigFile {
		t.Errorf("AI_QUORUM source: got %s, want config_file", quorum.Source)
	}

	// GOVET should still be default.
	govet := vars["CODERO_GOVET_ENABLED"]
	if govet.Source != gate.SourceDefault {
		t.Errorf("GOVET source: got %s, want default", govet.Source)
	}
}

func TestResolveEffective_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")

	content := "CODERO_COPILOT_ENABLED=false\nCODERO_AI_QUORUM=2\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clear all, then set specific env overrides.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	t.Setenv("CODERO_COPILOT_ENABLED", "true")

	vars, err := gate.ResolveEffectiveMap(path)
	if err != nil {
		t.Fatal(err)
	}

	// COPILOT: env overrides file.
	copilot := vars["CODERO_COPILOT_ENABLED"]
	if copilot.Value != "true" {
		t.Errorf("COPILOT_ENABLED: got %q, want true (env override)", copilot.Value)
	}
	if copilot.Source != gate.SourceShellEnv {
		t.Errorf("COPILOT_ENABLED source: got %s, want shell_env", copilot.Source)
	}

	// AI_QUORUM: not overridden by env, should come from file.
	quorum := vars["CODERO_AI_QUORUM"]
	if quorum.Value != "2" {
		t.Errorf("AI_QUORUM: got %q, want 2 (from file)", quorum.Value)
	}
	if quorum.Source != gate.SourceConfigFile {
		t.Errorf("AI_QUORUM source: got %s, want config_file", quorum.Source)
	}
}

// ─── Config Persistence / SaveConfigVar ──────────────────────────────────

func TestSaveConfigVar_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".codero", "config.env")

	err := gate.SaveConfigVar(path, "CODERO_COPILOT_ENABLED", "true")
	if err != nil {
		t.Fatal(err)
	}

	// File should exist.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CODERO_COPILOT_ENABLED=true") {
		t.Errorf("file missing expected key=value: %s", string(data))
	}
}

func TestSaveConfigVar_UpdatesExistingKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	initial := "CODERO_COPILOT_ENABLED=false\nCODERO_AI_QUORUM=1\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	err := gate.SaveConfigVar(path, "CODERO_COPILOT_ENABLED", "true")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CODERO_COPILOT_ENABLED=true") {
		t.Errorf("expected updated value, got: %s", string(data))
	}
	if !strings.Contains(string(data), "CODERO_AI_QUORUM=1") {
		t.Errorf("other key should be preserved: %s", string(data))
	}
}

func TestSaveConfigVar_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")

	// Write two values.
	if err := gate.SaveConfigVar(path, "CODERO_COPILOT_ENABLED", "true"); err != nil {
		t.Fatal(err)
	}
	if err := gate.SaveConfigVar(path, "CODERO_AI_QUORUM", "3"); err != nil {
		t.Fatal(err)
	}

	// Read them back.
	m, err := gate.ParseConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if m["CODERO_COPILOT_ENABLED"] != "true" {
		t.Errorf("COPILOT: got %q", m["CODERO_COPILOT_ENABLED"])
	}
	if m["CODERO_AI_QUORUM"] != "3" {
		t.Errorf("QUORUM: got %q", m["CODERO_AI_QUORUM"])
	}
}

func TestSaveConfigVar_UnknownVariable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	err := gate.SaveConfigVar(path, "NOT_A_REAL_VAR", "value")
	if err == nil {
		t.Fatal("expected error for unknown variable")
	}
	if !strings.Contains(err.Error(), "unknown variable") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveConfigVar_InvalidValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	err := gate.SaveConfigVar(path, "CODERO_COPILOT_ENABLED", "maybe")
	if err == nil {
		t.Fatal("expected error for invalid boolean value")
	}
	if !strings.Contains(err.Error(), "must be true or false") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveConfigVar_InvalidInt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	err := gate.SaveConfigVar(path, "CODERO_AI_QUORUM", "abc")
	if err == nil {
		t.Fatal("expected error for non-integer")
	}
	if !strings.Contains(err.Error(), "non-negative integer") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ─── Config Drift Detection ─────────────────────────────────────────────

func TestDetectDrifts_NoDrift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	if err := os.WriteFile(path, []byte("CODERO_COPILOT_ENABLED=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Env matches file.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	drifts, err := gate.DetectDrifts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drifts) != 0 {
		t.Errorf("expected no drifts, got %d", len(drifts))
	}
}

func TestDetectDrifts_WithDrift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	if err := os.WriteFile(path, []byte("CODERO_COPILOT_ENABLED=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	t.Setenv("CODERO_COPILOT_ENABLED", "true")

	drifts, err := gate.DetectDrifts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(drifts) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(drifts))
	}
	if drifts[0].EnvVar != "CODERO_COPILOT_ENABLED" {
		t.Errorf("drift var: got %s", drifts[0].EnvVar)
	}
	if drifts[0].FileVal != "false" || drifts[0].ShellVal != "true" {
		t.Errorf("drift values: file=%q shell=%q", drifts[0].FileVal, drifts[0].ShellVal)
	}
}

// ─── Registry Completeness ──────────────────────────────────────────────

func TestRegistryContainsAllSpecKeys(t *testing.T) {
	required := []string{
		"CODERO_GOVET_ENABLED",
		"CODERO_TSC_ENABLED",
		"CODERO_SEMGREP_ENABLED",
		"CODERO_LITELLM_ENABLED",
		"CODERO_COPILOT_ENABLED",
		"CODERO_GEMINI_ENABLED",
		"CODERO_AIDER_ENABLED",
		"CODERO_PRAGENT_ENABLED",
		"CODERO_CODERABBIT_ENABLED",
		"CODERO_VALE_ENABLED",
		"CODERO_HADOLINT_ENABLED",
		"CODERO_SHELLCHECK_ENABLED",
		"CODERO_SQLI_ENABLED",
		"CODERO_AI_QUORUM",
		"CODERO_AI_BUDGET_SECONDS",
		"CODERO_LITELLM_TIMEOUT",
		"CODERO_COPILOT_TIMEOUT",
		"CODERO_AI_MODEL",
		"CODERO_MIN_AI_GATES",
		"CODERO_SKIP_INFRA_FAIL",
	}
	for _, key := range required {
		if gate.LookupEntry(key) == nil {
			t.Errorf("registry missing required spec key: %s", key)
		}
	}
}

func TestAlwaysOnChecks(t *testing.T) {
	checks := gate.AlwaysOnChecks()
	expected := map[string]bool{"path-guard": true, "gitleaks": true, "ruff": true}
	for _, c := range checks {
		if !expected[c] {
			t.Errorf("unexpected always-on check: %s", c)
		}
		delete(expected, c)
	}
	for c := range expected {
		t.Errorf("missing always-on check: %s", c)
	}
}

// ─── DefaultConfigFileContent ────────────────────────────────────────────

func TestDefaultConfigFileContent(t *testing.T) {
	content := gate.DefaultConfigFileContent()
	if !strings.Contains(content, "CODERO_COPILOT_ENABLED=false") {
		t.Error("default content missing CODERO_COPILOT_ENABLED=false")
	}
	if !strings.Contains(content, "CODERO_GOVET_ENABLED=true") {
		t.Error("default content missing CODERO_GOVET_ENABLED=true")
	}
	if !strings.Contains(content, "CODERO_AI_QUORUM=1") {
		t.Error("default content missing CODERO_AI_QUORUM=1")
	}
	if !strings.Contains(content, "CODERO_SKIP_INFRA_FAIL=true") {
		t.Error("default content missing CODERO_SKIP_INFRA_FAIL=true")
	}
}

// ─── EffectiveBool / EffectiveInt helpers ────────────────────────────────

func TestEffectiveBool(t *testing.T) {
	vars := map[string]gate.ResolvedVar{
		"CODERO_COPILOT_ENABLED": {Value: "true"},
		"CODERO_GOVET_ENABLED":   {Value: "false"},
	}
	if !gate.EffectiveBool(vars, "CODERO_COPILOT_ENABLED") {
		t.Error("expected true")
	}
	if gate.EffectiveBool(vars, "CODERO_GOVET_ENABLED") {
		t.Error("expected false")
	}
	// Missing key falls back to registry default.
	if !gate.EffectiveBool(vars, "CODERO_SEMGREP_ENABLED") {
		t.Error("expected true (default)")
	}
}

func TestEffectiveInt(t *testing.T) {
	vars := map[string]gate.ResolvedVar{
		"CODERO_AI_QUORUM": {Value: "3"},
	}
	if got := gate.EffectiveInt(vars, "CODERO_AI_QUORUM", 1); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
	if got := gate.EffectiveInt(vars, "CODERO_AI_BUDGET_SECONDS", 180); got != 180 {
		t.Errorf("got %d, want 180 (fallback)", got)
	}
}

// ─── LoadConfig integration with file ────────────────────────────────────

func TestLoadConfigFrom_WithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	content := "CODERO_COPILOT_ENABLED=true\nCODERO_COPILOT_TIMEOUT=120\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clear all env vars.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	// Also clear legacy env vars.
	t.Setenv("CODERO_COPILOT_TIMEOUT_SEC", "")
	t.Setenv("CODERO_LITELLM_TIMEOUT_SEC", "")
	t.Setenv("CODERO_GATE_TOTAL_TIMEOUT_SEC", "")

	cfg := gate.LoadConfigFrom(path)

	if !cfg.CopilotEnabled {
		t.Error("expected CopilotEnabled=true from file")
	}
	if cfg.CopilotTimeoutSec != 120 {
		t.Errorf("CopilotTimeoutSec: got %d, want 120 from file", cfg.CopilotTimeoutSec)
	}
	// LiteLLM should be default since not in file.
	if cfg.LiteLLMTimeoutSec != gate.DefaultLiteLLMTimeoutSec {
		t.Errorf("LiteLLMTimeoutSec: got %d, want default %d", cfg.LiteLLMTimeoutSec, gate.DefaultLiteLLMTimeoutSec)
	}
}

func TestLoadConfigFrom_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	content := "CODERO_COPILOT_ENABLED=false\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	t.Setenv("CODERO_COPILOT_ENABLED", "true")

	cfg := gate.LoadConfigFrom(path)
	if !cfg.CopilotEnabled {
		t.Error("env should override file: expected CopilotEnabled=true")
	}
}

func TestLoadConfigFrom_LegacyEnvVarsStillWork(t *testing.T) {
	// Legacy env var names (pre-spec) should still be honoured.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	t.Setenv("CODERO_COPILOT_TIMEOUT_SEC", "99")
	t.Setenv("CODERO_LITELLM_TIMEOUT_SEC", "88")
	t.Setenv("CODERO_GATE_TOTAL_TIMEOUT_SEC", "500")

	path := filepath.Join(t.TempDir(), "nonexistent.env")
	cfg := gate.LoadConfigFrom(path)

	if cfg.CopilotTimeoutSec != 99 {
		t.Errorf("CopilotTimeoutSec: got %d, want 99 (legacy env)", cfg.CopilotTimeoutSec)
	}
	if cfg.LiteLLMTimeoutSec != 88 {
		t.Errorf("LiteLLMTimeoutSec: got %d, want 88 (legacy env)", cfg.LiteLLMTimeoutSec)
	}
	if cfg.GateTotalTimeoutSec != 500 {
		t.Errorf("GateTotalTimeoutSec: got %d, want 500 (legacy env)", cfg.GateTotalTimeoutSec)
	}
}
