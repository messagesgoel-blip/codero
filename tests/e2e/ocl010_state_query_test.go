//go:build e2e

// tests/e2e/ocl010_state_query_test.go
//
// OCL-010 E2E: Verify that GET /api/v1/openclaw/state returns structured JSON
// with all required sections populated from WIRE-001/002/003 data.
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

type openClawStateResponse struct {
	Sessions []struct {
		SessionID string `json:"session_id"`
		Repo      string `json:"repo"`
		Branch    string `json:"branch"`
	} `json:"sessions"`
	Pipeline []struct {
		SessionID string `json:"session_id"`
		Repo      string `json:"repo"`
		Branch    string `json:"branch"`
		PRNumber  int    `json:"pr_number"`
	} `json:"pipeline"`
	Activity []struct {
		Seq       int64  `json:"seq"`
		EventType string `json:"event_type"`
	} `json:"activity"`
	GateHealth struct {
		Providers []struct {
			Provider string `json:"provider"`
			Total    int    `json:"total"`
			Passed   int    `json:"passed"`
		} `json:"providers"`
		Summary string `json:"summary"`
	} `json:"gate_health"`
	Scorecard struct {
		GatePassRate    string `json:"gate_pass_rate"`
		AvgCycleTime    string `json:"avg_cycle_time"`
		MergeRate       string `json:"merge_rate"`
		ComplianceScore string `json:"compliance_score"`
		Summary         string `json:"summary"`
	} `json:"scorecard"`
	GeneratedAt time.Time `json:"generated_at"`
}

func fetchOpenClawState(t *testing.T) openClawStateResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testAPIURL()+"/api/v1/openclaw/state", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch openclaw state: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("openclaw/state returned %d: %s", resp.StatusCode, body)
	}
	var result openClawStateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("parse openclaw state: %v (body: %s)", err, body)
	}
	return result
}

func TestOCL010_StateQueryEndpoint(t *testing.T) {
	// ── Seed data ────────────────────────────────────────────────────────

	// WIRE-001: Register a session with repo/branch.
	sessionID := fmt.Sprintf("e2e-ocl010-%d", time.Now().UnixNano())
	agentID := "e2e-ocl010-agent"

	out, err := codero("session", "register",
		"--session-id="+sessionID,
		"--agent-id="+agentID,
	)
	if err != nil {
		t.Fatalf("session register: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		codero("session", "end", "--session-id="+sessionID, "--agent-id="+agentID) //nolint:errcheck
	})

	// Extract heartbeat secret.
	const hbPrefix = "heartbeat_secret: "
	var secret string
	for _, line := range splitLines(out) {
		if len(line) > len(hbPrefix) && line[:len(hbPrefix)] == hbPrefix {
			secret = line[len(hbPrefix):]
		}
	}
	if secret == "" {
		t.Fatalf("no heartbeat_secret in register output: %s", out)
	}

	// Send heartbeat with repo/branch.
	out, err = codero("session", "heartbeat",
		"--session-id="+sessionID,
		"--heartbeat-secret="+secret,
		"--status=working",
		"--repo=codero",
		"--branch=feat/ocl010-test",
	)
	if err != nil {
		t.Fatalf("heartbeat: %v\n%s", err, out)
	}

	// WIRE-002: Record a precommit result.
	out, err = codero("record-precommit",
		"--repo=codero",
		"--branch=feat/ocl010-test",
		"--result=pass",
		"--duration-ms=1500",
		"--checks=gitleaks,semgrep",
	)
	if err != nil {
		t.Fatalf("record-precommit: %v\n%s", err, out)
	}

	// WIRE-003: Track a PR.
	out, err = codero("pr", "track",
		"--repo=codero",
		"--branch=feat/ocl010-test",
		"--pr=9999",
	)
	if err != nil {
		t.Fatalf("pr track: %v\n%s", err, out)
	}

	// ── Query state endpoint ─────────────────────────────────────────────

	var state openClawStateResponse
	if !waitForCondition(t, 5*time.Second, func() bool {
		state = fetchOpenClawState(t)
		// Check that all 5 sections are non-nil arrays/objects.
		return state.Sessions != nil &&
			state.Pipeline != nil &&
			state.Activity != nil &&
			state.GateHealth.Providers != nil &&
			state.Scorecard.Summary != ""
	}) {
		t.Fatal("openclaw/state did not return valid data within timeout")
	}

	// ── Assertions ───────────────────────────────────────────────────────

	// Sessions: should contain our seeded session.
	foundSession := false
	for _, s := range state.Sessions {
		if s.SessionID == sessionID && s.Repo == "codero" && s.Branch == "feat/ocl010-test" {
			foundSession = true
			break
		}
	}
	if !foundSession {
		t.Errorf("session %s not found in openclaw/state sessions", sessionID)
	}

	// Gate health: should contain precommit provider data.
	foundProvider := false
	for _, p := range state.GateHealth.Providers {
		if p.Provider == "precommit" && p.Total > 0 {
			foundProvider = true
			break
		}
	}
	if !foundProvider {
		t.Errorf("precommit provider not found in gate_health (providers: %+v)", state.GateHealth.Providers)
	}

	// Scorecard: should have non-empty summary.
	if state.Scorecard.Summary == "" || state.Scorecard.Summary == "scorecard unavailable" {
		t.Errorf("scorecard summary missing or unavailable: %q", state.Scorecard.Summary)
	}

	// GeneratedAt should be recent.
	if time.Since(state.GeneratedAt) > 30*time.Second {
		t.Errorf("generated_at too old: %v", state.GeneratedAt)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
