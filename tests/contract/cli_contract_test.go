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
	if !strings.Contains(string(out), "two-pass review") {
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
