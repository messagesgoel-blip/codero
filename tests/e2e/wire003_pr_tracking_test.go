//go:build e2e

// tests/e2e/wire003_pr_tracking_test.go
//
// WIRE-003 E2E: Verify that `codero pr track` registers a PR number and
// it surfaces in the dashboard pipeline and repos API endpoints.
//
// Requires a running Codero daemon. Set CODERO_TEST_API_URL to the
// dashboard HTTP base URL (default: http://127.0.0.1:8110).

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

type reposResponse struct {
	Repos []struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		PRNumber int    `json:"pr_number"`
	} `json:"repos,omitempty"`
}

func fetchJSON(t *testing.T, path string, out interface{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testAPIURL()+path, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("%s returned %d: %s", path, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("parse %s: %v (body: %s)", path, err, body)
	}
}

func TestWIRE003_PRTracking(t *testing.T) {
	repo := "codero"
	branch := fmt.Sprintf("feat/e2e-wire003-%d", time.Now().UnixNano())
	prNumber := 9999

	// Step 1: Register a PR via CLI.
	out, err := codero("pr", "track",
		"--repo="+repo,
		"--branch="+branch,
		fmt.Sprintf("--pr=%d", prNumber),
	)
	if err != nil {
		t.Fatalf("pr track failed: %v\nOutput: %s", err, out)
	}
	t.Logf("Tracked PR: %s", out)

	// Step 2: Idempotent — calling again should succeed without duplicating.
	out, err = codero("pr", "track",
		"--repo="+repo,
		"--branch="+branch,
		fmt.Sprintf("--pr=%d", prNumber),
	)
	if err != nil {
		t.Fatalf("idempotent pr track failed: %v\nOutput: %s", err, out)
	}

	// Step 3: Verify via repos API (branch_states-based).
	var repos reposResponse
	if !waitForCondition(t, 5*time.Second, func() bool {
		fetchJSON(t, "/api/v1/dashboard/repos", &repos)
		for _, r := range repos.Repos {
			if r.Repo == repo && r.Branch == branch && r.PRNumber == prNumber {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("PR #%d for %s/%s not found in repos endpoint within timeout", prNumber, repo, branch)
	}

	// Step 4: Update PR number.
	newPR := 8888
	out, err = codero("pr", "track",
		"--repo="+repo,
		"--branch="+branch,
		fmt.Sprintf("--pr=%d", newPR),
	)
	if err != nil {
		t.Fatalf("pr track update failed: %v\nOutput: %s", err, out)
	}

	// Verify updated PR number.
	if !waitForCondition(t, 5*time.Second, func() bool {
		fetchJSON(t, "/api/v1/dashboard/repos", &repos)
		for _, r := range repos.Repos {
			if r.Repo == repo && r.Branch == branch && r.PRNumber == newPR {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("updated PR #%d for %s/%s not found in repos endpoint", newPR, repo, branch)
	}
}
