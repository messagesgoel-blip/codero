package contract

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

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
	// profile=off skips most checks and always returns PASS.
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
	// ai-gate is always DISABLED with NOT_IN_SCOPE — must be present
	if !strings.Contains(string(out), `"ai-gate"`) {
		t.Errorf("gate-check JSON output must include ai-gate (disabled check)\noutput: %s", string(out))
	}
	if !strings.Contains(string(out), `"DISABLED"`) {
		t.Errorf("gate-check JSON output must include at least one DISABLED check\noutput: %s", string(out))
	}
}
