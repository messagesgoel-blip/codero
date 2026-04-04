//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestOCL022_WebhookFindingsDelivery verifies the full pipeline:
// adapter POST /deliver with session → PTY output + audit logged.
func TestOCL022_WebhookFindingsDelivery(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	adapterBase := testAdapterURL()
	branch := fmt.Sprintf("feat/e2e-webhook-%d", time.Now().UnixNano())
	tmuxSession := fmt.Sprintf("e2e-wh-%d", time.Now().UnixNano()%100000)
	sessionID := fmt.Sprintf("e2e-wh-sess-%d", time.Now().UnixNano())
	agentID := fmt.Sprintf("e2e-wh-agent-%d", time.Now().UnixNano())

	// 1. Create tmux session.
	if err := exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "cat").Run(); err != nil {
		t.Fatalf("tmux new-session: %v", err)
	}
	t.Cleanup(func() { exec.Command("tmux", "kill-session", "-t", tmuxSession).Run() })

	// 2. Register agent session.
	out, err := codero("session", "register",
		"--session-id="+sessionID,
		"--agent-id="+agentID,
		"--tmux-name="+tmuxSession,
		"--branch="+branch,
		"--mode=claude",
		"--repo=codero",
	)
	if err != nil {
		t.Fatalf("session register: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		codero("session", "end", "--session-id="+sessionID, "--agent-id="+agentID)
	})

	// 3. Track a PR for the branch.
	out, err = codero("pr", "track", "--repo=codero", "--branch="+branch, "--pr=9999")
	if err != nil {
		t.Fatalf("pr track: %v\n%s", err, out)
	}

	// 4. POST /deliver to adapter with test findings.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	deliverBody, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"findings": []map[string]any{
			{"severity": "error", "file": "main.go", "line": 10, "message": "unused variable x", "rule_id": "CR-1"},
			{"severity": "warning", "file": "config.go", "line": 5, "message": "missing doc comment"},
		},
		"source": "e2e-test",
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		adapterBase+"/deliver", strings.NewReader(string(deliverBody)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deliver request: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("Deliver response: %d %s", resp.StatusCode, respBody)

	var dResp struct {
		Status string `json:"status"`
	}
	json.Unmarshal(respBody, &dResp)

	// 5. Assert delivery succeeded and verify tmux pane contains findings.
	if dResp.Status != "success" {
		t.Fatalf("expected delivery status 'success', got %q (full response: %s)", dResp.Status, respBody)
	}
	waitForCondition(t, 5*time.Second, 200*time.Millisecond, "tmux pane contains findings", func() bool {
		captureOut, err := exec.Command("tmux", "capture-pane", "-t", tmuxSession, "-p").Output()
		if err != nil {
			return false
		}
		return strings.Contains(string(captureOut), "main.go")
	})

	// 6. Verify audit log recorded the delivery with at least one entry.
	auditReq, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		adapterBase+"/audit?limit=5", nil)
	auditResp, err := http.DefaultClient.Do(auditReq)
	if err != nil {
		t.Fatalf("audit request: %v", err)
	}
	defer auditResp.Body.Close()

	auditBody, _ := io.ReadAll(auditResp.Body)
	var auditResult struct {
		Entries []struct {
			Prompt string `json:"prompt"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(auditBody, &auditResult); err != nil {
		t.Fatalf("parse audit response: %v (body: %s)", err, auditBody)
	}
	if auditResult.Total == 0 {
		t.Fatalf("expected at least 1 audit entry, got 0 (body: %s)", auditBody)
	}
}

// TestOCL022_FindingsEndpointWithPR verifies that the OCL-020 findings endpoint
// returns pr_metadata when a PR is tracked for the branch.
func TestOCL022_FindingsEndpointWithPR(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	apiBase := testAPIURL()
	branch := fmt.Sprintf("feat/e2e-findings-%d", time.Now().UnixNano())

	// Track a PR for the branch.
	out, err := codero("pr", "track", "--repo=codero", "--branch="+branch, "--pr=8888")
	if err != nil {
		t.Fatalf("pr track: %v\n%s", err, out)
	}

	// Query findings endpoint.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		apiBase+"/api/v1/openclaw/findings?repo=codero&branch="+branch, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("findings request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("findings endpoint: %d %s", resp.StatusCode, body)
	}

	// PR tracked, so response should be valid JSON with pr_metadata.
	if !strings.Contains(string(body), "pr_metadata") {
		t.Fatalf("response missing pr_metadata field: %s", body)
	}
	if !strings.Contains(string(body), "8888") {
		t.Fatalf("response missing PR number 8888: %s", body)
	}
}
