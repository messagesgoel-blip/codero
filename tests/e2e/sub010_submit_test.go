//go:build e2e

// tests/e2e/sub010_submit_test.go
//
// SUB-010 E2E: Verify that `codero submit` performs the full git+PR flow:
// commit, push, create PR, record in state, and handles subsequent submits correctly.
//
// Requires:
// - A running Codero daemon (CODERO_TEST_API_URL, default: http://127.0.0.1:8110)
// - GITHUB_TOKEN with repo access
// - gh CLI for cleanup

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSUB010_SubmitCommand(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("GITHUB_TOKEN not set, skipping E2E test")
	}

	// Unique branch name for this test run
	branch := fmt.Sprintf("feat/e2e-submit-test-%d", time.Now().UnixNano())
	repo := "messagesgoel-blip/codero"
	var prNumber int

	// Cleanup at the end
	t.Cleanup(func() {
		if prNumber > 0 {
			t.Logf("Cleanup: closing PR #%d", prNumber)
			_ = exec.Command("gh", "pr", "close", fmt.Sprintf("%d", prNumber), "--repo", repo).Run()
		}
		t.Logf("Cleanup: deleting remote branch %s", branch)
		_ = exec.Command("git", "push", "origin", "--delete", branch).Run()
	})

	// Step 1: Setup - create a temp git worktree with a file
	tmpDir := t.TempDir()
	setupGitWorktree(t, tmpDir, branch)

	// Write initial file
	testFile := filepath.Join(tmpDir, "e2e-test.txt")
	if err := os.WriteFile(testFile, []byte("initial content\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitAdd(t, tmpDir, "e2e-test.txt")

	// Step 2: First submit - should create PR
	out, err := codero("submit",
		"--repo="+repo,
		"--branch="+branch,
		"--title=E2E Submit Test",
		"--body=Automated test - will be closed",
		"--worktree="+tmpDir,
	)
	if err != nil {
		t.Fatalf("first submit failed: %v\nOutput: %s", err, out)
	}
	t.Logf("First submit output: %s", out)

	// Extract PR number from output
	prNumber = extractPRNumber(t, out)
	if prNumber == 0 {
		t.Fatal("failed to extract PR number from submit output")
	}

	// Step 3: Verify PR exists via gh CLI
	ghOut, err := exec.Command("gh", "pr", "list",
		"--repo", repo,
		"--head", branch,
		"--json", "number",
	).Output()
	if err != nil {
		t.Fatalf("gh pr list failed: %v", err)
	}
	if !strings.Contains(string(ghOut), fmt.Sprintf(`"number":%d`, prNumber)) {
		t.Errorf("expected PR #%d in gh pr list output, got: %s", prNumber, ghOut)
	}

	// Step 4: Verify PR is in dashboard pipeline
	if !waitForCondition(t, 10*time.Second, func() bool {
		return verifyBranchInPipeline(t, repo, branch, prNumber)
	}) {
		t.Errorf("branch %s not found in dashboard pipeline within timeout", branch)
	}

	// Step 5: Submit again with no changes - should error
	out, err = codero("submit",
		"--repo="+repo,
		"--branch="+branch,
		"--title=E2E Submit Test",
		"--worktree="+tmpDir,
	)
	if err == nil {
		t.Error("expected error when submitting with no changes")
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("expected 'no changes' error, got: %s", out)
	}

	// Step 6: Write another file and submit again - should reuse PR
	testFile2 := filepath.Join(tmpDir, "e2e-test-2.txt")
	if err := os.WriteFile(testFile2, []byte("second file\n"), 0644); err != nil {
		t.Fatalf("write file 2: %v", err)
	}
	gitAdd(t, tmpDir, "e2e-test-2.txt")

	out, err = codero("submit",
		"--repo="+repo,
		"--branch="+branch,
		"--title=E2E Submit Test (update)",
		"--worktree="+tmpDir,
	)
	if err != nil {
		t.Fatalf("second submit failed: %v\nOutput: %s", err, out)
	}
	t.Logf("Second submit output: %s", out)

	// Verify it reused the same PR
	secondPRNumber := extractPRNumber(t, out)
	if secondPRNumber != prNumber {
		t.Errorf("expected same PR #%d, but got #%d", prNumber, secondPRNumber)
	}
	if !strings.Contains(out, "Found existing PR") {
		t.Errorf("expected 'Found existing PR' in output, got: %s", out)
	}
}

// setupGitWorktree creates a git repo connected to the remote.
func setupGitWorktree(t *testing.T, dir, branch string) {
	t.Helper()

	// Clone the repo (shallow)
	cmd := exec.Command("git", "clone", "--depth=1", "--single-branch",
		"https://github.com/messagesgoel-blip/codero.git", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone failed: %v\n%s", err, out)
	}

	// Create and checkout the new branch
	cmd = exec.Command("git", "-C", dir, "checkout", "-b", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b failed: %v\n%s", err, out)
	}
}

// gitAdd stages a file.
func gitAdd(t *testing.T, dir, file string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "add", file)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}
}

// extractPRNumber extracts PR number from submit output.
func extractPRNumber(t *testing.T, output string) int {
	t.Helper()
	// Output format: "Submitted: PR #123 — https://..."
	var prNum int
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "PR #") {
			// Try to parse "PR #123"
			_, err := fmt.Sscanf(line, "%*[^#]#%d", &prNum)
			if err == nil && prNum > 0 {
				return prNum
			}
		}
	}
	return 0
}

// verifyBranchInPipeline checks if the branch appears in the dashboard pipeline.
func verifyBranchInPipeline(t *testing.T, repo, branch string, prNumber int) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testAPIURL()+"/api/v1/dashboard/repos", nil)
	if err != nil {
		t.Logf("create request: %v", err)
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("fetch repos: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("repos returned %d: %s", resp.StatusCode, body)
		return false
	}

	var data struct {
		Repos []struct {
			Repo     string `json:"repo"`
			Branch   string `json:"branch"`
			PRNumber int    `json:"pr_number"`
		} `json:"repos"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &data); err != nil {
		t.Logf("parse repos: %v", err)
		return false
	}

	// The repo might be stored with or without owner prefix
	repoShort := strings.TrimPrefix(repo, "messagesgoel-blip/")
	for _, r := range data.Repos {
		if (r.Repo == repo || r.Repo == repoShort) && r.Branch == branch && r.PRNumber == prNumber {
			return true
		}
	}
	return false
}
