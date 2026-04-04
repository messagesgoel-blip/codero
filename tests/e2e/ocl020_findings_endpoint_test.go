//go:build e2e

// tests/e2e/ocl020_findings_endpoint_test.go
//
// OCL-020 E2E: Verify GET /api/v1/openclaw/findings returns structured findings.
// Requires CODERO_E2E_LIVE=1 and a running Codero dashboard at CODERO_TEST_API_URL.

package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

type findingsResponse struct {
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	Findings []struct {
		Severity string `json:"severity"`
		Category string `json:"category"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Message  string `json:"message"`
		Source   string `json:"source"`
		RuleID   string `json:"rule_id"`
		Ts       string `json:"ts"`
	} `json:"findings"`
	PRMetadata *struct {
		PRNumber          int    `json:"pr_number"`
		CIStatus          string `json:"ci_status"`
		Approved          bool   `json:"approved"`
		UnresolvedThreads int    `json:"unresolved_threads"`
	} `json:"pr_metadata"`
}

func TestOCL020_FindingsEndpoint_EmptyBranch(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	base := testAPIURL()

	// Query a non-existent branch — should return 200 with empty findings and null pr_metadata.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/v1/openclaw/findings?repo=codero&branch=nonexistent-branch-ocl020", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET findings: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var fr findingsResponse
	if err := json.Unmarshal(body, &fr); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, body)
	}

	if fr.Repo != "codero" {
		t.Errorf("expected repo 'codero', got %q", fr.Repo)
	}
	if fr.Branch != "nonexistent-branch-ocl020" {
		t.Errorf("expected branch 'nonexistent-branch-ocl020', got %q", fr.Branch)
	}
	if len(fr.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(fr.Findings))
	}
	if fr.PRMetadata != nil {
		t.Errorf("expected null pr_metadata for unknown branch, got %+v", fr.PRMetadata)
	}
}

func TestOCL020_FindingsEndpoint_MissingParams(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	base := testAPIURL()

	// Missing repo and branch should return 400.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/v1/openclaw/findings", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET findings: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}
}

func TestOCL020_FindingsEndpoint_ResponseShape(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	base := testAPIURL()

	// Query a branch that likely has findings from precommit runs.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/v1/openclaw/findings?repo=codero&branch=main", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET findings: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Verify JSON shape has all required top-level fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}

	for _, key := range []string{"repo", "branch", "findings", "pr_metadata"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing required field %q", key)
		}
	}

	// Verify findings is an array (even if empty).
	var fr findingsResponse
	if err := json.Unmarshal(body, &fr); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if fr.Findings == nil {
		t.Error("findings should be an array (possibly empty), got nil")
	}

	// If findings exist, verify severity ordering (error < warning < info)
	// and timestamp ordering within same severity bucket.
	// Note: seeding is non-deterministic (depends on live precommit data);
	// sort verification only runs when findings are present.
	if len(fr.Findings) > 1 {
		for i := 1; i < len(fr.Findings); i++ {
			prevRank := sevRank(fr.Findings[i-1].Severity)
			currRank := sevRank(fr.Findings[i].Severity)
			if prevRank > currRank {
				t.Errorf("findings not sorted by severity: %q (rank %d) before %q (rank %d) at index %d",
					fr.Findings[i-1].Severity, prevRank, fr.Findings[i].Severity, currRank, i)
			}
			// Within same severity, timestamps should be non-decreasing.
			if prevRank == currRank && fr.Findings[i-1].Ts > fr.Findings[i].Ts {
				t.Errorf("findings with same severity %q not sorted by timestamp at index %d: %s > %s",
					fr.Findings[i].Severity, i, fr.Findings[i-1].Ts, fr.Findings[i].Ts)
			}
		}
	} else {
		t.Log("no findings on main branch to verify sort order; sort correctness covered by unit tests")
	}
}

func sevRank(s string) int {
	switch s {
	case "error":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}
