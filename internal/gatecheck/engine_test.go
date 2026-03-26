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
		t.Errorf("overall: got %q, want pass", s.OverallStatus)
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
		t.Errorf("overall: got %q, want fail", s.OverallStatus)
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
		t.Errorf("strict profile disabled required: got %q, want fail", s.OverallStatus)
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
		t.Errorf("portable profile disabled required: got %q, want pass", s.OverallStatus)
	}
}

func TestComputeSummary_AllowRequiredSkip(t *testing.T) {
	// strict + allowRequiredSkip=true => even disabled required doesn't fail
	checks := []gatecheck.CheckResult{
		{ID: "a", Status: gatecheck.StatusDisabled, Required: true, Enabled: false},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfileStrict, true)
	if s.OverallStatus != gatecheck.StatusPass {
		t.Errorf("allowRequiredSkip=true: got %q, want pass", s.OverallStatus)
	}
	if s.RequiredDisabled != 0 {
		t.Errorf("required_disabled should be 0 when allowed")
	}
}

func TestComputeSummary_InfraBypassed(t *testing.T) {
	checks := []gatecheck.CheckResult{
		{ID: "a", Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonInfraBypass},
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
		{ID: "ib", Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonInfraBypass},
		{ID: "di", Status: gatecheck.StatusDisabled},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfilePortable, false)
	if s.Passed != 1 {
		t.Errorf("passed: %d", s.Passed)
	}
	if s.Failed != 1 {
		t.Errorf("failed: %d", s.Failed)
	}
	if s.Skipped != 2 {
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
	cmd.Env = cleanGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, out)
	}
}

func cleanGitEnv() []string {
	env := os.Environ()
	cleaned := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		cleaned = append(cleaned, kv)
	}
	return cleaned
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

func writeExecutable(t *testing.T, dir, rel, content string) string {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return abs
}

func findCheck(t *testing.T, report gatecheck.Report, id string) gatecheck.CheckResult {
	t.Helper()
	for _, c := range report.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("check %q not found", id)
	return gatecheck.CheckResult{}
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

	// profile=off: trailing-whitespace, final-newline, gofmt, semgrep, ruff must be skip/disabled
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
				t.Errorf("profile=off check %q: got status %q, want skip or disabled", c.ID, c.Status)
			}
		}
	}
}

func TestRunPipeline_GitleaksFailBlocked(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	mustRun(t, dir, "git", "add", "main.go")

	tool := writeExecutable(t, dir, "bin/gitleaks", "#!/bin/sh\necho leak-found\nexit 1\n")

	cfg := gatecheck.EngineConfig{
		Profile:      gatecheck.ProfilePortable,
		GitleaksPath: tool,
		Invocation:   "codero",
	}
	engine := gatecheck.NewEngine(cfg)

	report, err := engine.RunPipeline(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if report.Result != gatecheck.StatusFail {
		t.Errorf("result: got %q, want fail", report.Result)
	}
	if !report.Blocked {
		t.Error("expected blocked=true on gitleaks failure")
	}

	check := findCheck(t, *report, "gitleaks-staged")
	if check.Status != gatecheck.StatusFail {
		t.Errorf("gitleaks status: got %q, want fail", check.Status)
	}

	substatus := filepath.Join(dir, gatecheck.HeartbeatSubstatusPath)
	if _, err := os.Stat(substatus); err != nil {
		t.Fatalf("expected substatus at %s: %v", substatus, err)
	}
}

func TestRunPipeline_CleanPass(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	mustRun(t, dir, "git", "add", "main.go")

	tool := writeExecutable(t, dir, "bin/gitleaks", "#!/bin/sh\nexit 0\n")

	cfg := gatecheck.EngineConfig{
		Profile:      gatecheck.ProfilePortable,
		GitleaksPath: tool,
		Invocation:   "codero",
	}
	engine := gatecheck.NewEngine(cfg)

	report, err := engine.RunPipeline(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	if report.Result != gatecheck.StatusPass {
		t.Errorf("result: got %q, want pass", report.Result)
	}
	if report.Blocked {
		t.Error("expected blocked=false on clean pass")
	}
}

func TestRunPipeline_DisabledCheckShowsSkip(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	mustRun(t, dir, "git", "add", "main.go")

	tool := writeExecutable(t, dir, "bin/gitleaks", "#!/bin/sh\nexit 1\n")

	cfg := gatecheck.EngineConfig{
		Profile:      gatecheck.ProfileOff,
		GitleaksPath: tool,
		Invocation:   "codero",
	}
	engine := gatecheck.NewEngine(cfg)

	report, err := engine.RunPipeline(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	check := findCheck(t, *report, "gitleaks-staged")
	if check.Status != gatecheck.StatusSkip {
		t.Errorf("gitleaks status: got %q, want skip", check.Status)
	}
}

func TestRunPipeline_FindingsTruncated(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	mustRun(t, dir, "git", "add", "main.go")

	var b strings.Builder
	for i := 0; i < 75; i++ {
		b.WriteString("finding ")
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString("\n")
	}
	script := "#!/bin/sh\ncat <<'EOF'\n" + b.String() + "EOF\nexit 1\n"
	tool := writeExecutable(t, dir, "bin/gitleaks", script)

	cfg := gatecheck.EngineConfig{
		Profile:      gatecheck.ProfilePortable,
		GitleaksPath: tool,
		Invocation:   "codero",
	}
	engine := gatecheck.NewEngine(cfg)

	report, err := engine.RunPipeline(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("RunPipeline: %v", err)
	}
	check := findCheck(t, *report, "gitleaks-staged")
	if check.FindingsCount != 75 {
		t.Errorf("findings_count: got %d, want 75", check.FindingsCount)
	}
	if !check.Truncated {
		t.Error("expected truncated=true for findings > 50")
	}
	if len(check.Findings) != gatecheck.MaxFindingsPerCheck {
		t.Errorf("findings length: got %d, want %d", len(check.Findings), gatecheck.MaxFindingsPerCheck)
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
				t.Errorf("gitleaks with missing tool: got %q, want disabled", c.Status)
			}
			if c.ReasonCode != gatecheck.ReasonMissingTool {
				t.Errorf("gitleaks reason_code: got %q, want missing_tool", c.ReasonCode)
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
				t.Errorf("file-size: got %q, want fail", c.Status)
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
				t.Errorf("merge-markers: got %q, want fail", c.Status)
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
				t.Errorf("forbidden-paths disabled: got %q, want disabled", c.Status)
			}
			const wantReason = "CODERO_ENFORCE_FORBIDDEN_PATHS not set"
			if c.Reason != wantReason {
				t.Errorf("forbidden-paths reason: got %q, want %q", c.Reason, wantReason)
			}
			return
		}
	}
	t.Fatal("forbidden-paths check not found")
}

// TestEngine_ForbiddenPaths_EnforceSetRegexEmpty verifies that when
// CODERO_ENFORCE_FORBIDDEN_PATHS is true but CODERO_FORBIDDEN_PATH_REGEX is
// empty the disabled reason message correctly identifies the missing regex,
// not the enforce flag (BUG-001 / COD-052).
func TestEngine_ForbiddenPaths_EnforceSetRegexEmpty(t *testing.T) {
	dir := makeRepo(t)
	writeFile(t, dir, "a.go", "package main\n")
	mustRun(t, dir, "git", "add", "a.go")

	cfg := gatecheck.EngineConfig{
		Profile:               gatecheck.ProfileStrict,
		RepoPath:              dir,
		EnforceForbiddenPaths: true,
		ForbiddenPathRegex:    "", // enforce set but regex empty
	}
	engine := gatecheck.NewEngine(cfg)
	report := engine.Run(context.Background())

	for _, c := range report.Checks {
		if c.ID == "forbidden-paths" {
			if c.Status != gatecheck.StatusDisabled {
				t.Errorf("forbidden-paths: got status %q, want disabled", c.Status)
			}
			const wantReason = "CODERO_FORBIDDEN_PATH_REGEX not set or empty"
			if c.Reason != wantReason {
				t.Errorf("forbidden-paths reason: got %q, want %q", c.Reason, wantReason)
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
				t.Errorf("config-validation bad JSON: got %q, want fail", c.Status)
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
				t.Errorf("ai-gate: got %q, want disabled", c.Status)
			}
			if c.ReasonCode != gatecheck.ReasonNotInScope {
				t.Errorf("ai-gate reason_code: got %q, want not_in_scope", c.ReasonCode)
			}
			return
		}
	}
	t.Fatal("ai-gate check not found")
}

func TestEngine_InfraBudget(t *testing.T) {
	checks := []gatecheck.CheckResult{
		{ID: "s1", Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonInfraBypass, Required: false},
		{ID: "s2", Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonInfraBypass, Required: false},
		{ID: "s3", Status: gatecheck.StatusPass, Required: false},
	}
	s := gatecheck.ComputeSummary(checks, gatecheck.ProfilePortable, false)
	if s.InfraBypassed != 2 {
		t.Errorf("infra_bypassed: got %d, want 2", s.InfraBypassed)
	}
	// ComputeSummary itself doesn't fail on infra bypass (that's engine policy)
	if s.OverallStatus != gatecheck.StatusPass {
		t.Errorf("summary with infra bypass should pass: got %q", s.OverallStatus)
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

// TestEngine_FailedCheck_HasReasonCode verifies that when a check produces a fail
// status without an explicit reason_code (the common case for content checks), the
// engine's normalisation pass fills in ReasonCode = "check_failed".
// This covers BUG-002 from the v1.2.3 advanced pilot.
func TestEngine_FailedCheck_HasReasonCode(t *testing.T) {
	dir := makeRepo(t)

	// Stage a file with merge conflict markers to trigger a deterministic fail.
	f := filepath.Join(dir, "conflict.txt")
	content := "hello\n<<<<<<< HEAD\nbranch-a\n=======\nbranch-b\n>>>>>>> other\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mustRun(t, dir, "git", "add", "conflict.txt")

	cfg := gatecheck.EngineConfig{
		Profile:  gatecheck.ProfilePortable,
		RepoPath: dir,
	}
	report := gatecheck.NewEngine(cfg).Run(context.Background())

	var failCheck *gatecheck.CheckResult
	for i := range report.Checks {
		if report.Checks[i].ID == "merge-markers" {
			failCheck = &report.Checks[i]
			break
		}
	}
	if failCheck == nil {
		t.Fatal("merge-markers check not found in report")
	}
	if failCheck.Status != gatecheck.StatusFail {
		t.Fatalf("expected merge-markers status=fail, got %q", failCheck.Status)
	}
	if failCheck.ReasonCode != gatecheck.ReasonCheckFailed {
		t.Errorf("expected reason_code=%q, got %q", gatecheck.ReasonCheckFailed, failCheck.ReasonCode)
	}
}

// TestEngine_ExplicitReasonCode_NotOverwritten confirms that when a check runner
// sets an explicit reason_code on a fail result (e.g. exec_error, infra_bypass),
// the normalisation pass does NOT overwrite it.
func TestEngine_ExplicitReasonCode_NotOverwritten(t *testing.T) {
	// exec_error is set by runForbiddenPathsCheck when the regex is invalid.
	dir := makeRepo(t)
	f := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(f, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustRun(t, dir, "git", "add", "file.txt")

	cfg := gatecheck.EngineConfig{
		Profile:               gatecheck.ProfilePortable,
		RepoPath:              dir,
		EnforceForbiddenPaths: true,
		ForbiddenPathRegex:    "[invalid(regex",
	}
	report := gatecheck.NewEngine(cfg).Run(context.Background())

	var fpCheck *gatecheck.CheckResult
	for i := range report.Checks {
		if report.Checks[i].ID == "forbidden-paths" {
			fpCheck = &report.Checks[i]
			break
		}
	}
	if fpCheck == nil {
		t.Fatal("forbidden-paths check not found")
	}
	if fpCheck.Status != gatecheck.StatusFail {
		t.Fatalf("expected status=fail, got %q", fpCheck.Status)
	}
	// exec_error was set by the runner; normalisation must not overwrite it.
	if fpCheck.ReasonCode != gatecheck.ReasonExecError {
		t.Errorf("expected reason_code=%q (set by runner), got %q", gatecheck.ReasonExecError, fpCheck.ReasonCode)
	}
}

// --- LOG-001: DisplayState mapping tests ---

func TestDisplayState_Mapping(t *testing.T) {
	cases := []struct {
		status gatecheck.CheckStatus
		want   string
	}{
		{gatecheck.StatusPass, "passing"},
		{gatecheck.StatusFail, "failing"},
		{gatecheck.StatusSkip, "disabled"},
		{gatecheck.StatusDisabled, "disabled"},
	}
	for _, tc := range cases {
		got := string(tc.status.ToDisplayState())
		if got != tc.want {
			t.Errorf("CheckStatus(%q).ToDisplayState() = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestDisplayState_UnknownFallsToDisabled(t *testing.T) {
	// Any unrecognized status value must not produce passing or failing.
	var unknown gatecheck.CheckStatus = "pending"
	if got := unknown.ToDisplayState(); got != gatecheck.DisplayDisabled {
		t.Errorf("unknown status: ToDisplayState() = %q, want %q", got, gatecheck.DisplayDisabled)
	}
}

// --- LOG-001: Stable step ordering test ---

// knownStepOrder lists the 13 canonical step IDs in their fixed runner order.
// This test ensures the engine never silently reorders or renames them.
var knownStepOrder = []string{
	"file-size",
	"merge-markers",
	"trailing-whitespace",
	"final-newline",
	"forbidden-paths",
	"config-validation",
	"lockfile-sync",
	"exec-bit-policy",
	"gofmt",
	"gitleaks-staged",
	"semgrep",
	"ruff-lint",
	"ai-gate",
}

func TestEngine_StableStepOrder(t *testing.T) {
	dir := makeRepo(t)
	// No staged files → all checks will be skip/disabled, but all must appear.
	report := gatecheck.NewEngine(gatecheck.EngineConfig{
		Profile:  gatecheck.ProfilePortable,
		RepoPath: dir,
	}).Run(context.Background())

	if len(report.Checks) != len(knownStepOrder) {
		t.Fatalf("expected %d checks, got %d", len(knownStepOrder), len(report.Checks))
	}
	for i, want := range knownStepOrder {
		if got := report.Checks[i].ID; got != want {
			t.Errorf("checks[%d].id = %q, want %q", i, got, want)
		}
	}
}

func TestEngine_AllChecksPresent_EvenWhenSkipped(t *testing.T) {
	// Verifies GC-001 rule: no check is omitted even if disabled/skipped.
	dir := makeRepo(t)
	report := gatecheck.NewEngine(gatecheck.EngineConfig{
		Profile:  gatecheck.ProfilePortable,
		RepoPath: dir,
	}).Run(context.Background())

	seen := make(map[string]bool, len(report.Checks))
	for _, c := range report.Checks {
		if seen[c.ID] {
			t.Errorf("duplicate check ID %q", c.ID)
		}
		seen[c.ID] = true
	}
	for _, want := range knownStepOrder {
		if !seen[want] {
			t.Errorf("check %q missing from report", want)
		}
	}
}

func TestEngine_SkipDisabledHaveReasonCode(t *testing.T) {
	// GC-001: skip/disabled must carry a reason_code after normalisation.
	dir := makeRepo(t)
	report := gatecheck.NewEngine(gatecheck.EngineConfig{
		Profile:  gatecheck.ProfilePortable,
		RepoPath: dir,
	}).Run(context.Background())

	for _, c := range report.Checks {
		if c.Status == gatecheck.StatusSkip || c.Status == gatecheck.StatusDisabled {
			if c.ReasonCode == "" {
				t.Errorf("check %q has status=%q but empty reason_code", c.ID, c.Status)
			}
		}
	}
}
