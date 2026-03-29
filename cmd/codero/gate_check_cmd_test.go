package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/gatecheck"
)

func mustGitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = cleanGitEnv()
	return cmd
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

// TestUsageError_InvalidFlagCombination verifies that combining --json and
// --tui-snapshot returns a *UsageError (exit-code 2 class), not a gate failure.
func TestUsageError_InvalidFlagCombination(t *testing.T) {
	cmd := gateCheckCmd()
	cmd.SetArgs([]string{"--json", "--tui-snapshot"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error for --json --tui-snapshot, got nil")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected *UsageError for invalid flag combo, got %T: %v", err, err)
	}
}

// TestUsageError_BadProfile verifies that an unknown --profile value returns a
// *UsageError (exit-code 2 class).
func TestUsageError_BadProfile(t *testing.T) {
	cmd := gateCheckCmd()
	cmd.SetArgs([]string{"--profile", "nonexistent"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown profile, got nil")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected *UsageError for bad profile, got %T: %v", err, err)
	}
}

// TestGateFail_NotUsageError verifies that a real gate failure (merge-markers
// check failing) does NOT produce a *UsageError; it must be a plain error so
// that main maps it to exit code 1, not 2.
func TestGateFail_NotUsageError(t *testing.T) {
	dir := makeConflictRepoForGateTest(t)
	reportPath := filepath.Join(t.TempDir(), "report.json")

	cmd := gateCheckCmd()
	cmd.SetArgs([]string{
		"--json",
		"--profile", "portable",
		"--repo-path", dir,
		"--report-path", reportPath,
	})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected gate-fail error for conflict repo, got nil")
	}
	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		t.Fatalf("gate failure must NOT be a *UsageError (would map to exit 2 instead of 1): %v", err)
	}
}

// TestGatePass_NoError verifies that a passing run returns nil (exit code 0).
func TestGatePass_NoError(t *testing.T) {
	dir := makeCleanRepoForGateTest(t)
	reportPath := filepath.Join(t.TempDir(), "report.json")

	cmd := gateCheckCmd()
	cmd.SetArgs([]string{
		"--json",
		"--profile", string(gatecheck.ProfileOff),
		"--repo-path", dir,
		"--report-path", reportPath,
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("gate-check with profile=off on clean repo should pass, got: %v", err)
	}
}

func TestUsageError_LoadReportConflictWithProfile(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(reportPath, []byte(`{"summary":{"overall_status":"pass"}}`), 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}

	cmd := gateCheckCmd()
	cmd.SetArgs([]string{"--load-report", reportPath, "--profile", "portable"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error for --load-report --profile, got nil")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected *UsageError for invalid flag combo, got %T: %v", err, err)
	}
}

func TestGateCheck_LoadReportTUISnapshot_UsesProvidedReport(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "fixture-report.json")
	reportJSON := `{
  "schema_version": "1.0.0",
  "profile": "PORTABLE",
  "summary": {
    "overall_status": "FAIL",
    "passed": 8,
    "failed": 3,
    "skipped": 1,
    "infra_bypassed": 1,
    "disabled": 1,
    "total": 14,
    "required_failed": 2
  },
  "checks": [
    {
      "id": "secret-scan",
      "name": "Secret Scan",
      "group": "security",
      "required": true,
      "enabled": true,
      "status": "FAILED",
      "reason_code": "check_failed",
      "duration_ms": 0
    },
    {
      "id": "fmt-check",
      "name": "Format Check",
      "group": "format",
      "required": false,
      "enabled": true,
      "status": "pass",
      "duration_ms": 0
    },
    {
      "id": "sonarcloud",
      "name": "SonarCloud Analysis",
      "group": "other",
      "required": false,
      "enabled": true,
      "status": "SKIP",
      "reason_code": "infra_bypass",
      "duration_ms": 0
    }
  ],
  "run_at": "1970-01-01T00:00:00Z"
}`
	if err := os.WriteFile(reportPath, []byte(reportJSON), 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}
	t.Setenv("CODERO_GATE_CHECK_REPORT_PATH", reportPath)

	out := captureGateCheckStdout(t, func() error {
		cmd := gateCheckCmd()
		cmd.SetArgs([]string{"--tui-snapshot", "--load-report", reportPath})
		return cmd.ExecuteContext(context.Background())
	})

	for _, want := range []string{
		"OVERALL  FAIL",
		"PROFILE  portable",
		"total=14",
		"infra=1",
		"failing",
		"passing",
		"secret-scan",
		"sonarcloud",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("snapshot missing %q\nfull output:\n%s", want, out)
		}
	}

	h := dashboard.NewHandler(nil, dashboard.NewSettingsStore(t.TempDir()), nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/gate-checks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gate-checks status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"overall_status":"FAIL"`,
		`"profile":"PORTABLE"`,
		`"total":14`,
		`"infra_bypassed":1`,
		`"secret-scan"`,
		`"sonarcloud"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("gate-checks body missing %q\nfull body:\n%s", want, body)
		}
	}
}

// makeCleanRepoForGateTest creates a minimal git repo with no staged conflicts.
func makeCleanRepoForGateTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := mustGitCmd(dir, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, string(out))
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", "hello.txt")
	run("commit", "-q", "-m", "init")
	return dir
}

// makeConflictRepoForGateTest creates a git repo with a staged merge-conflict file.
func makeConflictRepoForGateTest(t *testing.T) string {
	t.Helper()
	dir := makeCleanRepoForGateTest(t)
	conflict := "<<<<<<< HEAD\nbranch-a\n=======\nbranch-b\n>>>>>>> other\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte(conflict), 0o644); err != nil {
		t.Fatalf("write conflict file: %v", err)
	}
	cmd := mustGitCmd(dir, "add", "hello.txt")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git add conflict file: %v\n%s", err, string(out))
	}
	return dir
}

func captureGateCheckStdout(t *testing.T, fn func() error) string {
	t.Helper()

	type readResult struct {
		out []byte
		err error
	}

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	readCh := make(chan readResult, 1)
	go func() {
		out, err := io.ReadAll(r)
		readCh <- readResult{out: out, err: err}
	}()

	runErr := fn()
	_ = w.Close()
	result := <-readCh
	_ = r.Close()
	if result.err != nil {
		t.Fatalf("read stdout: %v", result.err)
	}
	if runErr == nil {
		return string(result.out)
	}
	var usageErr *UsageError
	if errors.As(runErr, &usageErr) {
		t.Fatalf("expected plain gate failure or nil, got usage error: %v", runErr)
	}
	return string(result.out)
}
