//go:build e2e

// tests/e2e/wire001_session_binding_test.go
//
// WIRE-001 E2E: Verify that session registration and heartbeat propagate
// repo/branch to the dashboard active-sessions API.
//
// Requires a running Codero daemon. Set CODERO_TEST_DAEMON_ADDR to the
// gRPC address (default: 127.0.0.1:50051) and CODERO_TEST_API_URL to the
// dashboard HTTP base URL (default: http://127.0.0.1:8110).

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// waitForCondition polls a condition function until it returns true or timeout.
func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

type activeSessionsResponse struct {
	ActiveCount int `json:"active_count"`
	Sessions    []struct {
		SessionID  string `json:"session_id"`
		AgentID    string `json:"agent_id"`
		Repo       string `json:"repo"`
		Branch     string `json:"branch"`
		Mode       string `json:"mode"`
	} `json:"sessions"`
}

func testAPIURL() string {
	if v := getenv("CODERO_TEST_API_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8110"
}

func getenv(key string) string {
	// Thin wrapper to avoid importing os in the build-tagged file.
	out, _ := exec.Command("printenv", key).Output()
	return strings.TrimSpace(string(out))
}

func codero(args ...string) (string, error) {
	cmd := exec.Command("codero", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func fetchActiveSessions(t *testing.T) activeSessionsResponse {
	t.Helper()
	resp, err := http.Get(testAPIURL() + "/api/v1/dashboard/active-sessions")
	if err != nil {
		t.Fatalf("fetch active-sessions: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("active-sessions returned %d: %s", resp.StatusCode, body)
	}
	var result activeSessionsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("parse active-sessions: %v (body: %s)", err, body)
	}
	return result
}

func findSession(sessions activeSessionsResponse, sessionID string) *struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	Mode      string `json:"mode"`
} {
	for i := range sessions.Sessions {
		if sessions.Sessions[i].SessionID == sessionID {
			return &sessions.Sessions[i]
		}
	}
	return nil
}

func TestWIRE001_SessionBinding(t *testing.T) {
	sessionID := fmt.Sprintf("e2e-wire001-%d", time.Now().UnixNano())
	agentID := "e2e-test-agent"

	// Step 1: Register session with repo and branch.
	out, err := codero("session", "register",
		"--session-id="+sessionID,
		"--agent-id="+agentID,
	)
	if err != nil {
		t.Fatalf("session register failed: %v\nOutput: %s", err, out)
	}
	t.Logf("Registered session: %s", sessionID)

	// Extract heartbeat secret from output.
	var heartbeatSecret string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "heartbeat_secret: ") {
			heartbeatSecret = strings.TrimPrefix(line, "heartbeat_secret: ")
		}
	}
	if heartbeatSecret == "" {
		t.Fatalf("no heartbeat_secret in register output: %s", out)
	}

	// Step 2: Send heartbeat with repo and branch.
	out, err = codero("session", "heartbeat",
		"--session-id="+sessionID,
		"--heartbeat-secret="+heartbeatSecret,
		"--status=working",
		"--repo=codero",
		"--branch=main",
	)
	if err != nil {
		t.Fatalf("session heartbeat failed: %v\nOutput: %s", err, out)
	}

	// Step 3: Verify via API (with polling).
	var s *struct {
		SessionID string `json:"session_id"`
		AgentID   string `json:"agent_id"`
		Repo      string `json:"repo"`
		Branch    string `json:"branch"`
		Mode      string `json:"mode"`
	}
	if !waitForCondition(t, 5*time.Second, func() bool {
		sessions := fetchActiveSessions(t)
		s = findSession(sessions, sessionID)
		return s != nil
	}) {
		t.Fatalf("session %s not found in active sessions within timeout", sessionID)
	}
	if s.Repo != "codero" {
		t.Errorf("expected repo=codero, got %q", s.Repo)
	}
	if s.Branch != "main" {
		t.Errorf("expected branch=main, got %q", s.Branch)
	}

	// Step 4: Update repo/branch on heartbeat (simulates agent switching worktrees).
	out, err = codero("session", "heartbeat",
		"--session-id="+sessionID,
		"--heartbeat-secret="+heartbeatSecret,
		"--status=working",
		"--repo=whimsy",
		"--branch=feat/test",
	)
	if err != nil {
		t.Fatalf("heartbeat update failed: %v\nOutput: %s", err, out)
	}

	// Verify update with polling.
	if !waitForCondition(t, 5*time.Second, func() bool {
		sessions := fetchActiveSessions(t)
		s = findSession(sessions, sessionID)
		return s != nil && s.Repo == "whimsy" && s.Branch == "feat/test"
	}) {
		t.Fatalf("session %s not updated to repo=whimsy, branch=feat/test within timeout", sessionID)
	}

	// Step 5: End the session.
	out, err = codero("session", "end",
		"--session-id="+sessionID,
		"--agent-id="+agentID,
	)
	if err != nil {
		t.Logf("session end failed (non-fatal): %v\nOutput: %s", err, out)
	}

	// Verify session removed from active list.
	if !waitForCondition(t, 5*time.Second, func() bool {
		sessions := fetchActiveSessions(t)
		s = findSession(sessions, sessionID)
		return s == nil
	}) {
		t.Errorf("session %s still in active sessions after end", sessionID)
	}
}
