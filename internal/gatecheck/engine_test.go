package gatecheck_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codero/codero/internal/gatecheck"
)

// --- ComputeSummary tests ---

func TestComputeSummary_AllPass(t *testing.T) {
	checks := []gatecheck.CheckResult{
		{ID: "a", Status: gatecheck.StatusPass, Required: true},
		{ID: "b", Status: gatecheck.StatusPass, Required: false},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfilePortable, false)
	if s.OverallStatus != gatecheck.StatusPass {
		t.Errorf("overall: got %q, want PASS", s.OverallStatus)
	}
	if s.Passed != 2 {
		t.Errorf("passed: got %d, want 2", s.Passed)
	}
	if s.Failed != 0 {
		t.Errorf("failed: got %d, want 0", s.Failed)
	}
	if s.Total != 2 {
		t.Errorf("total: got %d, want 2", s.Total)
	}
	if s.SchemaVersion != "1" {
		t.Errorf("schema_version: got %q", s.SchemaVersion)
	}
}

func TestComputeSummary_RequiredFail(t *testing.T) {
	checks := []gatecheck.CheckResult{
		{ID: "a", Status: gatecheck.StatusFail, Required: true},
		{ID: "b", Status: gatecheck.StatusPass, Required: false},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfilePortable, false)
	if s.OverallStatus != gatecheck.StatusFail {
		t.Errorf("overall: got %q, want FAIL", s.OverallStatus)
	}
	if s.RequiredFailed != 1 {
		t.Errorf("required_failed: got %d, want 1", s.RequiredFailed)
	}
}

func TestComputeSummary_DisabledRequiredStrict(t *testing.T) {
	// strict profile + required disabled + allowRequiredSkip=false => FAIL
	checks := []gatecheck.CheckResult{
		{ID: "a", Status: gatecheck.StatusDisabled, Required: true, Enabled: false},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfileStrict, false)
	if s.OverallStatus != gatecheck.StatusFail {
		t.Errorf("strict profile disabled required: got %q, want FAIL", s.OverallStatus)
	}
	if s.RequiredDisabled != 1 {
		t.Errorf("required_disabled: got %d, want 1", s.RequiredDisabled)
	}
}

func TestComputeSummary_DisabledRequiredPortable(t *testing.T) {
	// portable profile + required disabled => PASS (portable is tolerant)
	checks := []gatecheck.CheckResult{
		{ID: "a", Status: gatecheck.StatusDisabled, Required: true, Enabled: false},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfilePortable, false)
	if s.OverallStatus != gatecheck.StatusPass {
		t.Errorf("portable profile disabled required: got %q, want PASS", s.OverallStatus)
	}
}

func TestComputeSummary_AllowRequiredSkip(t *testing.T) {
	// strict + allowRequiredSkip=true => even disabled required doesn't fail
	checks := []gatecheck.CheckResult{
		{ID: "a", Status: gatecheck.StatusDisabled, Required: true, Enabled: false},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfileStrict, true)
	if s.OverallStatus != gatecheck.StatusPass {
		t.Errorf("allowRequiredSkip=true: got %q, want PASS", s.OverallStatus)
	}
	if s.RequiredDisabled != 0 {
		t.Errorf("required_disabled should be 0 when allowed")
	}
}

func TestComputeSummary_InfraBypassed(t *testing.T) {
	checks := []gatecheck.CheckResult{
		{ID: "a", Status: gatecheck.StatusInfraBypass},
		{ID: "b", Status: gatecheck.StatusPass},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfilePortable, false)
	if s.InfraBypassed != 1 {
		t.Errorf("infra_bypassed: got %d, want 1", s.InfraBypassed)
	}
	if s.OverallStatus != gatecheck.StatusPass {
		t.Errorf("infra_bypass should not fail overall: got %q", s.OverallStatus)
	}
}

func TestComputeSummary_AllStatusTypes(t *testing.T) {
	checks := []gatecheck.CheckResult{
		{ID: "p", Status: gatecheck.StatusPass},
		{ID: "f", Status: gatecheck.StatusFail, Required: false},
		{ID: "sk", Status: gatecheck.StatusSkip},
		{ID: "ib", Status: gatecheck.StatusInfraBypass},
		{ID: "di", Status: gatecheck.StatusDisabled},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfilePortable, false)
	if s.Passed != 1 {
		t.Errorf("passed: %d", s.Passed)
	}
	if s.Failed != 1 {
		t.Errorf("failed: %d", s.Failed)
	}
	if s.Skipped != 1 {
		t.Errorf("skipped: %d", s.Skipped)
	}
	if s.InfraBypassed != 1 {
		t.Errorf("infra_bypassed: %d", s.InfraBypassed)
	}
	if s.Disabled != 1 {
		t.Errorf("disabled: %d", s.Disabled)
	}
	if s.Total != 5 {
		t.Errorf("total: %d", s.Total)
	}
}

// --- Engine profile tests ---

func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init")
	mustRun(t, dir, "git", "config", "user.email", "test@test.com")
	mustRun(t, dir, "git", "config", "user.name", "Test")
	return dir
}

func mustRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out)
	}
}

// writeFile writes content to path (relative to dir) and returns the absolute path.
func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestEngine_ProfileOff_MinimalChecks(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "hello.go", "package main\n")
	mustRun(t, dir, "git", "add", "hello.go")

	cfg := gatecheck.EngineConfig{
		Profile:  gatecheck.ProfileOff,
		RepoPath: dir,
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	// profile=off: trailing-whitespace, final-newline, gofmt, semgrep, ruff must be SKIP/DISABLED
	offChecks := map[string]bool{
		"trailing-whitespace": false,
		"final-newline":       false,
		"gofmt":               false,
		"semgrep":             false,
		"ruff-lint":           false,
	}
	for _, c := range report.Checks {
		if _, ok := offChecks[c.ID]; ok {
			if c.Status != gatecheck.StatusSkip && c.Status != gatecheck.StatusDisabled {
				t.Errorf("profile=off check %q: got status %q, want SKIP or DISABLED", c.ID, c.Status)
			}
		}
	}
}

func TestEngine_MissingTool_GitleaksDisabled(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "file.go", "package main\n")
	mustRun(t, dir, "git", "add", "file.go")

	cfg := gatecheck.EngineConfig{
		Profile:      gatecheck.ProfilePortable,
		RepoPath:     dir,
		GitleaksPath: "/nonexistent/gitleaks",
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	for _, c := range report.Checks {
		if c.ID == "gitleaks-staged" {
			if c.Status != gatecheck.StatusDisabled {
				t.Errorf("gitleaks with missing tool: got %q, want DISABLED", c.Status)
			}
			if c.ReasonCode != gatecheck.ReasonMissingTool {
				t.Errorf("gitleaks reason_code: got %q, want MISSING_TOOL", c.ReasonCode)
			}
			return
		}
	}
	t.Fatal("gitleaks-staged check not found in report")
}

func TestEngine_AllChecksPresent(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "a.go", "package main\n")
	mustRun(t, dir, "git", "add", "a.go")

	cfg := gatecheck.EngineConfig{
		Profile:  gatecheck.ProfilePortable,
		RepoPath: dir,
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	expected := []string{
		"file-size", "merge-markers", "trailing-whitespace", "final-newline",
		"forbidden-paths", "config-validation", "lockfile-sync", "exec-bit-policy",
		"gofmt", "gitleaks-staged", "semgrep", "ruff-lint", "ai-gate",
	}
	found := map[string]bool{}
	for _, c := range report.Checks {
		found[c.ID] = true
	}
	for _, id := range expected {
		if !found[id] {
			t.Errorf("check %q missing from report", id)
		}
	}
	if len(report.Checks) != len(expected) {
		t.Errorf("check count: got %d, want %d", len(report.Checks), len(expected))
	}
}

func TestEngine_FileSizeCheck_Fail(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "big.txt", strings.Repeat("x", 200)+"\n")
	mustRun(t, dir, "git", "add", "big.txt")

	cfg := gatecheck.EngineConfig{
		Profile:            gatecheck.ProfilePortable,
		RepoPath:           dir,
		MaxStagedFileBytes: 100,
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	for _, c := range report.Checks {
		if c.ID == "file-size" {
			if c.Status != gatecheck.StatusFail {
				t.Errorf("file-size: got %q, want FAIL", c.Status)
			}
			return
		}
	}
	t.Fatal("file-size check not found")
}

func TestEngine_MergeMarkersCheck_Fail(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "conflict.go", "package main\n<<<<<<< HEAD\nvar x = 1\n=======\nvar x = 2\n>>>>>>> branch\n")
	mustRun(t, dir, "git", "add", "conflict.go")

	cfg := gatecheck.EngineConfig{
		Profile:  gatecheck.ProfilePortable,
		RepoPath: dir,
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	for _, c := range report.Checks {
		if c.ID == "merge-markers" {
			if c.Status != gatecheck.StatusFail {
				t.Errorf("merge-markers: got %q, want FAIL", c.Status)
			}
			return
		}
	}
	t.Fatal("merge-markers check not found")
}

func TestEngine_ForbiddenPaths_Disabled(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "a.go", "package main\n")
	mustRun(t, dir, "git", "add", "a.go")

	cfg := gatecheck.EngineConfig{
		Profile:               gatecheck.ProfilePortable,
		RepoPath:              dir,
		EnforceForbiddenPaths: false,
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	for _, c := range report.Checks {
		if c.ID == "forbidden-paths" {
			if c.Status != gatecheck.StatusDisabled {
				t.Errorf("forbidden-paths disabled: got %q, want DISABLED", c.Status)
			}
			return
		}
	}
	t.Fatal("forbidden-paths check not found")
}

func TestEngine_ConfigValidation_InvalidJSON(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "bad.json", `{"unclosed":`)
	mustRun(t, dir, "git", "add", "bad.json")

	cfg := gatecheck.EngineConfig{
		Profile:  gatecheck.ProfilePortable,
		RepoPath: dir,
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	for _, c := range report.Checks {
		if c.ID == "config-validation" {
			if c.Status != gatecheck.StatusFail {
				t.Errorf("config-validation bad JSON: got %q, want FAIL", c.Status)
			}
			return
		}
	}
	t.Fatal("config-validation check not found")
}

func TestEngine_AIGate_AlwaysDisabled(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "a.go", "package main\n")
	mustRun(t, dir, "git", "add", "a.go")

	cfg := gatecheck.EngineConfig{
		Profile:  gatecheck.ProfileStrict,
		RepoPath: dir,
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	for _, c := range report.Checks {
		if c.ID == "ai-gate" {
			if c.Status != gatecheck.StatusDisabled {
				t.Errorf("ai-gate: got %q, want DISABLED", c.Status)
			}
			if c.ReasonCode != gatecheck.ReasonNotInScope {
				t.Errorf("ai-gate reason_code: got %q, want NOT_IN_SCOPE", c.ReasonCode)
			}
			return
		}
	}
	t.Fatal("ai-gate check not found")
}

func TestEngine_InfraBudget(t *testing.T) {
	checks := []gatecheck.CheckResult{
		{ID: "s1", Status: gatecheck.StatusInfraBypass, Required: false},
		{ID: "s2", Status: gatecheck.StatusInfraBypass, Required: false},
		{ID: "s3", Status: gatecheck.StatusPass, Required: false},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfilePortable, false)
	if s.InfraBypassed != 2 {
		t.Errorf("infra_bypassed: got %d, want 2", s.InfraBypassed)
	}
	// ComputeSummary itself doesn't fail on infra bypass (that's engine policy)
	if s.OverallStatus != gatecheck.StatusPass {
		t.Errorf("summary with infra bypass should PASS: got %q", s.OverallStatus)
	}
}

func TestEngine_ReportContainsRunAt(t *testing.T) {
	cfg := gatecheck.EngineConfig{
		Profile:     gatecheck.ProfileOff,
		StagedFiles: []string{},
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())
	if report.RunAt.IsZero() {
		t.Error("RunAt should not be zero")
	}
}

func TestLoadEngineConfig_Defaults(t *testing.T) {
	for _, k := range []string{
		"CODERO_GATES_PROFILE", "CODERO_MAX_INFRA_BYPASS_GATES",
		"CODERO_ALLOW_REQUIRED_SKIP", "CODERO_GATE_TIMEOUT",
		"CODERO_MAX_STAGED_FILE_BYTES",
	} {
		t.Setenv(k, "")
	}
	cfg := gatecheck.LoadEngineConfig()
	if cfg.Profile != gatecheck.ProfilePortable {
		t.Errorf("default profile: got %q, want portable", cfg.Profile)
	}
	if cfg.MaxInfraBypass != 2 {
		t.Errorf("default MaxInfraBypass: got %d, want 2", cfg.MaxInfraBypass)
	}
	if cfg.AllowRequiredSkip {
		t.Error("default AllowRequiredSkip should be false")
	}
}

func TestLoadEngineConfig_EnvOverrides(t *testing.T) {
	t.Setenv("CODERO_GATES_PROFILE", "strict")
	t.Setenv("CODERO_MAX_INFRA_BYPASS_GATES", "5")
	t.Setenv("CODERO_ALLOW_REQUIRED_SKIP", "1")
	t.Setenv("CODERO_ENFORCE_FORBIDDEN_PATHS", "1")
	t.Setenv("CODERO_FORBIDDEN_PATH_REGEX", "^secrets/")
	t.Setenv("CODERO_REQUIRED_CHECKS", "gitleaks-staged,merge-markers")

	cfg := gatecheck.LoadEngineConfig()
	if cfg.Profile != gatecheck.ProfileStrict {
		t.Errorf("profile: got %q, want strict", cfg.Profile)
	}
	if cfg.MaxInfraBypass != 5 {
		t.Errorf("MaxInfraBypass: got %d, want 5", cfg.MaxInfraBypass)
	}
	if !cfg.AllowRequiredSkip {
		t.Error("AllowRequiredSkip should be true")
	}
	if !cfg.EnforceForbiddenPaths {
		t.Error("EnforceForbiddenPaths should be true")
	}
	if len(cfg.RequiredChecks) != 2 {
		t.Errorf("RequiredChecks: got %v", cfg.RequiredChecks)
	}
}
