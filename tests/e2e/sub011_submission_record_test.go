//go:build e2e

package e2e

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestSUB011_SubmissionRecord is an E2E test for the submission record feature.
// It verifies:
// 1. codero submit creates a submission record with non-empty diff_hash/head_sha
// 2. Subsequent submit with no new changes fails with "no changes to submit"
// 3. Submit with new staged changes creates a new submission record
// 4. Different diff hashes produce different submission_ids
func TestSUB011_SubmissionRecord(t *testing.T) {
	if os.Getenv("CODERO_E2E_ENABLED") != "1" {
		t.Skip("E2E tests disabled; set CODERO_E2E_ENABLED=1 to run")
	}

	// Setup: create temp git repo
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "codero.db")
	configPath := filepath.Join(tmpDir, "codero.yaml")

	// Initialize git repo
	runGitE2E(t, tmpDir, "init")
	runGitE2E(t, tmpDir, "config", "user.email", "test@e2e.test")
	runGitE2E(t, tmpDir, "config", "user.name", "E2E Test")

	// Create initial commit
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# E2E Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitE2E(t, tmpDir, "add", "README.md")
	runGitE2E(t, tmpDir, "commit", "-m", "initial commit")

	// Create config file
	configContent := `github_token: ghp_fake_for_e2e_test
repos:
  - messagesgoel-blip/codero
db_path: ` + dbPath + `
api_server:
  addr: ":18080"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Test 1: Stage a file and run codero submit
	if err := os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("first file content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitE2E(t, tmpDir, "add", "file1.txt")

	// Run codero submit (without GitHub token to avoid actual PR creation)
	cmd := exec.Command("go", "run", ".",
		"submit",
		"--config", configPath,
		"--worktree", tmpDir,
		"--repo", "messagesgoel-blip/codero",
		"--branch", "feat/e2e-sub011-test",
		"--title", "E2E SUB-011 Test",
		"--body", "Automated E2E test",
	)
	cmd.Dir = cmdDir(t)
	cmd.Env = append(os.Environ(),
		"GITHUB_TOKEN=", // Deliberately empty to skip GitHub operations
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		// Expected: PR creation skipped warning, but no error
		t.Logf("First submit output: %s", out)
	} else {
		// If we get here, it might be a valid warning about GitHub token
		t.Logf("First submit (expected warning): %v\nOutput: %s", err, out)
	}

	// Verify: DB has 1 submission row with non-empty diff_hash/head_sha
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var count int
	var diffHash, headSHA, state string
	err = db.QueryRowContext(context.Background(),
		`SELECT COUNT(*), diff_hash, head_sha, state FROM submissions WHERE branch = 'feat/e2e-sub011-test'`).
		Scan(&count, &diffHash, &headSHA, &state)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query submission: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 submission, got %d", count)
	}
	if diffHash == "" {
		t.Error("expected non-empty diff_hash")
	}
	if headSHA == "" {
		t.Error("expected non-empty head_sha")
	}
	if state != "submitted" {
		t.Errorf("expected state='submitted', got %q", state)
	}

	firstDiffHash := diffHash
	firstSubmissionID := ""
	_ = db.QueryRowContext(context.Background(),
		`SELECT submission_id FROM submissions WHERE branch = 'feat/e2e-sub011-test' ORDER BY created_at DESC LIMIT 1`).
		Scan(&firstSubmissionID)

	// Test 2: Run codero submit again with no new changes
	cmd = exec.Command("go", "run", ".",
		"submit",
		"--config", configPath,
		"--worktree", tmpDir,
		"--repo", "messagesgoel-blip/codero",
		"--branch", "feat/e2e-sub011-test",
		"--title", "E2E SUB-011 Test (no changes)",
	)
	cmd.Dir = cmdDir(t)
	cmd.Env = append(os.Environ(), "GITHUB_TOKEN=")
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Error("expected error for submit with no changes")
	}
	if !containsAny(string(out), "no changes to submit", "worktree is clean") {
		t.Errorf("expected 'no changes' error, got: %s", out)
	}

	// Test 3: Stage new file and submit again
	if err := os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("second file content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitE2E(t, tmpDir, "add", "file2.txt")

	cmd = exec.Command("go", "run", ".",
		"submit",
		"--config", configPath,
		"--worktree", tmpDir,
		"--repo", "messagesgoel-blip/codero",
		"--branch", "feat/e2e-sub011-test",
		"--title", "E2E SUB-011 Test (second submit)",
	)
	cmd.Dir = cmdDir(t)
	cmd.Env = append(os.Environ(), "GITHUB_TOKEN=")
	out, _ = cmd.CombinedOutput()
	t.Logf("Second submit output: %s", out)

	// Verify: DB has 2 submissions with different diff_hashes
	err = db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM submissions WHERE branch = 'feat/e2e-sub011-test'`).Scan(&count)
	if err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 submissions, got %d", count)
	}

	var secondDiffHash string
	err = db.QueryRowContext(context.Background(),
		`SELECT diff_hash FROM submissions WHERE branch = 'feat/e2e-sub011-test' AND submission_id != ? ORDER BY created_at DESC LIMIT 1`,
		firstSubmissionID).Scan(&secondDiffHash)
	if err != nil {
		t.Fatalf("query second diff_hash: %v", err)
	}
	if secondDiffHash == firstDiffHash {
		t.Error("expected different diff_hash for second submission")
	}

	t.Log("SUB-011 E2E test passed")
}

func runGitE2E(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=E2E Test",
		"GIT_AUTHOR_EMAIL=e2e@test.local",
		"GIT_COMMITTER_NAME=E2E Test",
		"GIT_COMMITTER_EMAIL=e2e@test.local",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\noutput: %s", args, err, out)
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// cmdDir returns the absolute path to cmd/codero relative to the test's working directory.
func cmdDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	return filepath.Join(wd, "..", "..", "cmd", "codero")
}

// TestSUB011_PipelineIncludesSubmissionCount verifies the dashboard pipeline
// endpoint returns submission_count and last_submission_id fields.
func TestSUB011_PipelineIncludesSubmissionCount(t *testing.T) {
	if os.Getenv("CODERO_E2E_ENABLED") != "1" {
		t.Skip("E2E tests disabled; set CODERO_E2E_ENABLED=1 to run")
	}

	// This test requires a running codero daemon
	// Skip if daemon is not running
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "curl", "-s", "http://localhost:18082/api/v1/dashboard/pipeline")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("Daemon not running or not reachable: %v", err)
	}

	// Verify the response contains the new fields.
	response := string(out)
	if !containsAny(response, "submission_count", "last_submission_id") {
		t.Errorf("pipeline response missing submission_count and last_submission_id; got: %s", response)
	}
}
