package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/codero/codero/internal/gatecheck"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// TestHelpContract validates that --help returns exit code 0 (success), not 1 (usage error).
// This aligns with codero-finish exit code semantics where 0=success, 1=feedback/retry needed.
func TestHelpContract(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "--help")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected help command to succeed: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Usage:") {
		t.Fatalf("help output missing Usage section: %s", string(out))
	}
}

func TestCommitGateCommandExists(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "commit-gate", "--help")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected commit-gate command to exist: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "gate-heartbeat") {
		t.Fatalf("commit-gate help missing expected text: %s", string(out))
	}
}

func TestRegisterCommandExists(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "register", "--help")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected register command to exist: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Register a branch") {
		t.Fatalf("register help missing expected text: %s", string(out))
	}
}

func TestQueueCommandExists(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "queue", "--help")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected queue command to exist: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "queue") {
		t.Fatalf("queue help missing expected text: %s", string(out))
	}
}

func TestBranchCommandExists(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "branch", "--help")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected branch command to exist: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "Displays") {
		t.Fatalf("branch help missing expected text: %s", string(out))
	}
}

func TestEventsCommandExists(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "events", "--help")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected events command to exist: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "events") {
		t.Fatalf("events help missing expected text: %s", string(out))
	}
}

func TestGateCheckCommandExists(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--help")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected gate-check command to exist: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "gate-check") {
		t.Fatalf("gate-check help missing expected text: %s", string(out))
	}
}

func TestGateCheckJSONFlag(t *testing.T) {
	root := repoRoot(t)
	// Run gate-check --json --profile off so it finishes fast with no real checks.
	// profile=off skips most checks and always returns pass.
	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json --profile off failed: %v\noutput: %s", err, string(out))
	}
	outStr := string(out)
	// Verify canonical fields are present in JSON output
	for _, field := range []string{`"summary"`, `"checks"`, `"run_at"`, `"schema_version"`, `"overall_status"`} {
		if !strings.Contains(outStr, field) {
			t.Errorf("gate-check JSON output missing field %q\nfull output: %s", field, outStr)
		}
	}
}

func TestGateCheckJSONDisabledChecksPresent(t *testing.T) {
	root := repoRoot(t)
	// With profile=off, all checks are skipped/disabled — they must still appear in output.
	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json --profile off failed: %v\noutput: %s", err, string(out))
	}
	// ai-gate is always disabled with not_in_scope — must be present
	if !strings.Contains(string(out), `"ai-gate"`) {
		t.Errorf("gate-check JSON output must include ai-gate (disabled check)\noutput: %s", string(out))
	}
	if !strings.Contains(string(out), `"disabled"`) {
		t.Errorf("gate-check JSON output must include at least one disabled check\noutput: %s", string(out))
	}
}

func TestGateCheckFastProfileAlias(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "fast")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json --profile fast failed: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), `"profile": "portable"`) {
		t.Fatalf("gate-check fast profile alias should resolve to portable profile\noutput: %s", string(out))
	}
}

func TestGateCheckJSONFailureExitCode(t *testing.T) {
	root := repoRoot(t)
	repo := gateCheckRepoWithConflict(t)
	reportPath := filepath.Join(t.TempDir(), "report.json")

	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--repo-path", repo)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CODERO_GATE_CHECK_REPORT_PATH="+reportPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected gate-check --json to fail for merge-markers fixture\noutput: %s", string(out))
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			t.Fatalf("gate-check --json exit code: got %d, want 1\noutput: %s", exitErr.ExitCode(), string(out))
		}
	} else {
		t.Fatalf("expected exec.ExitError, got %T: %v\noutput: %s", err, err, string(out))
	}

	reportJSON := extractJSONPayload(t, out)
	var report gatecheck.Report
	if err := json.Unmarshal(reportJSON, &report); err != nil {
		t.Fatalf("decode failing gate-check JSON: %v\noutput: %s", err, string(reportJSON))
	}
	if report.Summary.OverallStatus != gatecheck.StatusFail {
		t.Fatalf("failing JSON report overall_status: got %q, want fail", report.Summary.OverallStatus)
	}
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected JSON-mode report file at %s: %v", reportPath, err)
	}
}

// TestGateCheckInvalidFlagsExitCode2 verifies the exit-code contract for
// usage/config errors (COD-055): combining --json and --tui-snapshot must
// produce exit code 2, which is distinct from the gate-failure exit code 1.
//
// Note: this test builds a temporary binary because `go run` always exits with
// code 1 for any child failure, losing the precise exit code.
func TestGateCheckInvalidFlagsExitCode2(t *testing.T) {
	root := repoRoot(t)

	// Build a temporary binary so the exact exit code is preserved.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "codero-test")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/codero")
	buildCmd.Dir = root
	if buildOut, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build codero binary: %v\n%s", err, string(buildOut))
	}

	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.Command(binPath, "gate-check", "--json", "--tui-snapshot") //nolint:gosec
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected gate-check --json --tui-snapshot to fail\noutput: %s", string(out))
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected exec.ExitError, got %T: %v\noutput: %s", err, err, string(out))
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("gate-check --json --tui-snapshot exit code: got %d, want 2\noutput: %s", exitErr.ExitCode(), string(out))
	}
}

func TestGateCheckJSONReportPathEnv(t *testing.T) {
	root := repoRoot(t)
	repo := gateCheckRepoClean(t)
	reportPath := filepath.Join(t.TempDir(), "env-report.json")

	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off", "--repo-path", repo)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CODERO_GATE_CHECK_REPORT_PATH="+reportPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json with env report path failed: %v\noutput: %s", err, string(out))
	}

	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected JSON-mode report file at %s: %v", reportPath, err)
	}

	var stdoutReport gatecheck.Report
	if err := json.Unmarshal(out, &stdoutReport); err != nil {
		t.Fatalf("decode stdout JSON report: %v\noutput: %s", err, string(out))
	}
	fileReport, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read JSON-mode report file: %v", err)
	}
	var persistedReport gatecheck.Report
	if err := json.Unmarshal(fileReport, &persistedReport); err != nil {
		t.Fatalf("decode persisted JSON report: %v\nfile: %s", err, string(fileReport))
	}
	if !reflect.DeepEqual(stdoutReport, persistedReport) {
		t.Fatalf("stdout JSON and persisted report differ:\nstdout=%+v\nfile=%+v", stdoutReport, persistedReport)
	}
}

func TestGateCheckJSONReportPathFlagOverridesEnv(t *testing.T) {
	root := repoRoot(t)
	repo := gateCheckRepoClean(t)
	envReportPath := filepath.Join(t.TempDir(), "env-report.json")
	flagReportPath := filepath.Join(t.TempDir(), "flag-report.json")

	cmd := exec.Command("go", "run", "./cmd/codero", "gate-check", "--json", "--profile", "off", "--repo-path", repo, "--report-path", flagReportPath)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CODERO_GATE_CHECK_REPORT_PATH="+envReportPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate-check --json with explicit report-path failed: %v\noutput: %s", err, string(out))
	}

	if _, err := os.Stat(flagReportPath); err != nil {
		t.Fatalf("expected flag report file at %s: %v", flagReportPath, err)
	}
	if _, err := os.Stat(envReportPath); !os.IsNotExist(err) {
		t.Fatalf("env report path should not be written when flag is set; stat err=%v", err)
	}
}

func gateCheckRepoClean(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\noutput: %s", args, err, string(out))
		}
	}

	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	git("add", "tracked.txt")
	git("commit", "-q", "-m", "init")
	return dir
}

func gateCheckRepoWithConflict(t *testing.T) string {
	t.Helper()
	dir := gateCheckRepoClean(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("<<<<<<< HEAD\nbad\n=======\nworse\n>>>>>>> branch\n"), 0o644); err != nil {
		t.Fatalf("write conflict file: %v", err)
	}
	cmd := exec.Command("git", "add", "tracked.txt")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git add conflict file: %v\noutput: %s", err, string(out))
	}
	return dir
}

func extractJSONPayload(t *testing.T, out []byte) []byte {
	t.Helper()
	start := bytes.IndexByte(out, '{')
	end := bytes.LastIndexByte(out, '}')
	if start < 0 || end < start {
		t.Fatalf("unable to locate JSON object in output:\n%s", string(out))
	}
	return out[start : end+1]
}
