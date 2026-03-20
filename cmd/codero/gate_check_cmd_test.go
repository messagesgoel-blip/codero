package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			OverallStatus:  gatecheck.StatusFail,
			Passed:         8,
			Failed:         3,
			Skipped:        1,
			Disabled:       1,
			InfraBypassed:  1,
			RequiredFailed: 2,
			Total:          14,
			Profile:        gatecheck.ProfilePortable,
			SchemaVersion:  gatecheck.SchemaVersion,
		},
		Checks: []gatecheck.CheckResult{
			{ID: "secret-scan", Group: gatecheck.GroupSecurity, Status: gatecheck.StatusFail, Required: true, ReasonCode: gatecheck.ReasonCheckFailed},
			{ID: "fmt-check", Group: gatecheck.GroupFormat, Status: gatecheck.StatusPass},
			{ID: "sonarcloud", Group: gatecheck.GroupOther, Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonInfraBypass},
		},
		RunAt: time.Unix(0, 0).UTC(),
	}
	reportPath := filepath.Join(t.TempDir(), "fixture-report.json")
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatalf("write report: %v", err)
	}

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

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	runErr := fn()
	_ = w.Close()
	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	if runErr == nil {
		return string(out)
	}
	var usageErr *UsageError
	if errors.As(runErr, &usageErr) {
		t.Fatalf("expected plain gate failure or nil, got usage error: %v", runErr)
	}
	return string(out)
}
