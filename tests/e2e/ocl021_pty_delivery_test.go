//go:build e2e

// tests/e2e/ocl021_pty_delivery_test.go
//
// OCL-021 E2E: Verify POST /deliver sends findings to an agent PTY
// via agent-tmux-bridge.
// Requires CODERO_E2E_LIVE=1, a running Codero daemon, and the adapter
// at CODERO_TEST_ADAPTER_URL.

package e2e

import (
	"bytes"
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

func testAdapterURL() string {
	if v := os.Getenv("CODERO_TEST_ADAPTER_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8112"
}

func TestOCL021_PTYDelivery(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%100000)
	tmuxSession := "e2e-deliver-" + suffix
	sessionID := "e2e-ocl021-" + suffix
	agentID := "e2e-test-agent"
	adapterBase := testAdapterURL()

	// 1. Create a tmux session running cat (keeps session alive).
	createCmd := exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "cat")
	if out, err := createCmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session failed: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", tmuxSession).Run()
	})

	// 2. Register agent session via codero CLI with tmux name.
	out, err := codero("session", "register",
		"--session-id="+sessionID,
		"--agent-id="+agentID,
		"--mode=claude",
		"--tmux-name="+tmuxSession,
	)
	if err != nil {
		t.Fatalf("session register failed: %v\nOutput: %s", err, out)
	}
	t.Logf("Registered session %s with tmux %s", sessionID, tmuxSession)
	t.Cleanup(func() {
		codero("session", "end", "--session-id="+sessionID, "--agent-id="+agentID)
	})

	// Wait for session to be visible in the dashboard API.
	waitForCondition(t, 5*time.Second, 100*time.Millisecond, "session visible", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
			testAPIURL()+"/api/v1/dashboard/sessions/"+sessionID, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return false
		}
		resp.Body.Close()
		return true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 3. POST /deliver to the adapter with findings.
	deliverBody, _ := json.Marshal(map[string]interface{}{
		"session_id": sessionID,
		"findings": []map[string]interface{}{
			{"severity": "warning", "file": "main.go", "line": 10, "message": "unused variable x"},
			{"severity": "error", "file": "config.go", "line": 5, "message": "nil pointer deref"},
		},
		"source": "e2e-test",
	})

	deliverReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		adapterBase+"/deliver", bytes.NewReader(deliverBody))
	if err != nil {
		t.Fatalf("build deliver request: %v", err)
	}
	deliverReq.Header.Set("Content-Type", "application/json")
	deliverResp, err := http.DefaultClient.Do(deliverReq)
	if err != nil {
		t.Fatalf("deliver request failed: %v", err)
	}
	defer deliverResp.Body.Close()
	respBody, _ := io.ReadAll(deliverResp.Body)

	if deliverResp.StatusCode != http.StatusOK {
		t.Fatalf("deliver returned %d: %s", deliverResp.StatusCode, respBody)
	}

	var dResp struct {
		Status         string `json:"status"`
		SessionID      string `json:"session_id"`
		DeliveredCount int    `json:"delivered_count"`
	}
	if err := json.Unmarshal(respBody, &dResp); err != nil {
		t.Fatalf("parse deliver response: %v", err)
	}
	if dResp.Status != "success" && dResp.Status != "skipped" {
		t.Errorf("expected status success or skipped, got %s", dResp.Status)
	}
	if dResp.SessionID != sessionID {
		t.Errorf("expected session_id=%s, got %s", sessionID, dResp.SessionID)
	}

	// 4. If delivery succeeded (bridge available), capture tmux pane output.
	if dResp.Status == "success" {
		waitForCondition(t, 5*time.Second, 100*time.Millisecond, "tmux pane contains findings", func() bool {
			captureCmd := exec.Command("tmux", "capture-pane", "-t", tmuxSession, "-p")
			captureOut, err := captureCmd.Output()
			if err != nil {
				return false
			}
			return strings.Contains(string(captureOut), "main.go:10")
		})
		captureCmd := exec.Command("tmux", "capture-pane", "-t", tmuxSession, "-p")
		captureOut, err := captureCmd.Output()
		if err != nil {
			t.Fatalf("tmux capture-pane failed: %v", err)
		}
		paneText := string(captureOut)
		if !strings.Contains(paneText, "unused variable x") {
			t.Errorf("expected 'unused variable x' in pane output, got:\n%s", paneText)
		}
	}

	// 5. Check audit log includes delivery event.
	auditReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		adapterBase+"/audit?limit=10", nil)
	if err != nil {
		t.Fatalf("build audit request: %v", err)
	}
	auditResp, err := http.DefaultClient.Do(auditReq)
	if err != nil {
		t.Fatalf("audit request failed: %v", err)
	}
	defer auditResp.Body.Close()
	auditBody, _ := io.ReadAll(auditResp.Body)

	if auditResp.StatusCode != http.StatusOK {
		t.Fatalf("audit returned %d: %s", auditResp.StatusCode, auditBody)
	}
	if !strings.Contains(string(auditBody), "delivery") {
		t.Logf("audit response: %s", auditBody)
		t.Error("expected 'delivery' kind in audit entries")
	}

	// 6. Kill tmux session and try delivery again — should fail.
	exec.Command("tmux", "kill-session", "-t", tmuxSession).Run()
	waitForCondition(t, 3*time.Second, 100*time.Millisecond, "tmux session gone", func() bool {
		out, _ := exec.Command("tmux", "has-session", "-t", tmuxSession).CombinedOutput()
		_ = out
		return exec.Command("tmux", "has-session", "-t", tmuxSession).Run() != nil
	})

	deliverBody2, _ := json.Marshal(map[string]interface{}{
		"session_id": sessionID,
		"findings":   []map[string]interface{}{{"severity": "info", "file": "x.go", "line": 1, "message": "test"}},
		"source":     "e2e-retry",
	})
	retryReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		adapterBase+"/deliver", bytes.NewReader(deliverBody2))
	if err != nil {
		t.Fatalf("build retry request: %v", err)
	}
	retryReq.Header.Set("Content-Type", "application/json")
	retryResp, err := http.DefaultClient.Do(retryReq)
	if err != nil {
		t.Fatalf("retry deliver failed: %v", err)
	}
	defer retryResp.Body.Close()

	// After tmux is killed, bridge should fail (500 + "failed" status).
	// If BRIDGE_PATH was empty (skipped mode), any status is acceptable.
	retryBody, _ := io.ReadAll(retryResp.Body)
	if dResp.Status == "success" {
		if retryResp.StatusCode == http.StatusOK {
			var rr struct{ Status string }
			json.Unmarshal(retryBody, &rr)
			if rr.Status == "success" {
				t.Error("expected delivery to fail after tmux session killed")
			}
		}
	}
}

func TestOCL021_Deliver_SessionNotFound(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	adapterBase := testAdapterURL()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]interface{}{
		"session_id": "nonexistent-session-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"findings":   []map[string]interface{}{{"severity": "info", "file": "x.go", "line": 1, "message": "test"}},
		"source":     "e2e",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		adapterBase+"/deliver", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("deliver request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 404 for nonexistent session, got %d: %s", resp.StatusCode, respBody)
	}
}

// waitForCondition polls fn every interval until it returns true or timeout expires.
func waitForCondition(t *testing.T, timeout, interval time.Duration, desc string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("waitForCondition(%s): timed out after %s", desc, timeout)
}
