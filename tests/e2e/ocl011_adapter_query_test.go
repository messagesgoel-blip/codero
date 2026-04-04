//go:build e2e

// tests/e2e/ocl011_adapter_query_test.go
//
// OCL-011 E2E: Verify the openclaw-adapter query flow end-to-end.
// Requires a running Codero daemon and openclaw-adapter sidecar.
// Set CODERO_TEST_API_URL (default: http://127.0.0.1:8110) and
// OPENCLAW_ADAPTER_URL (default: http://127.0.0.1:8112).

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func adapterURL() string {
	if v := os.Getenv("OPENCLAW_ADAPTER_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8112"
}

func adapterQuery(t *testing.T, prompt string) map[string]interface{} {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adapterURL()+"/query", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("adapter query: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("adapter returned %d: %s", resp.StatusCode, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("parse response: %v (body: %s)", err, respBody)
	}
	return result
}

func TestOCL011_AdapterHealthCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, adapterURL()+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health returned %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
}

func TestOCL011_AdapterQueryWithSeededData(t *testing.T) {
	agentID := fmt.Sprintf("ocl011-agent-%d", time.Now().UnixNano())
	sessionID := agentID + "-sess"

	// Seed: register session.
	out, err := codero("session", "register",
		"--agent-id="+agentID,
		"--session-id="+sessionID,
		"--mode=autonomous",
	)
	if err != nil {
		t.Fatalf("register session: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		codero("session", "end", "--session-id="+sessionID, "--agent-id="+agentID) //nolint:errcheck
	})

	// Seed: track PR.
	out, err = codero("pr", "track", "--repo=codero", "--branch=feat/ocl011-test", "--pr=7777")
	if err != nil {
		t.Fatalf("pr track: %v\n%s", err, out)
	}

	// Poll until seeded data appears in state endpoint.
	stateURL := testAPIURL() + "/api/v1/openclaw/state"
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(stateURL)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if strings.Contains(string(body), agentID) {
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Query 1: Ask about sessions.
	r1 := adapterQuery(t, "What sessions are active?")
	if r1["response"] == nil || r1["response"].(string) == "" {
		t.Error("query 1: expected non-empty response")
	}
	if r1["conversation_id"] == nil || r1["conversation_id"].(string) == "" {
		t.Error("query 1: expected conversation_id")
	}

	// Query 2: Ask about PRs.
	r2 := adapterQuery(t, "What PRs are being tracked?")
	if r2["response"] == nil || r2["response"].(string) == "" {
		t.Error("query 2: expected non-empty response")
	}

	// Query 3: General health.
	r3 := adapterQuery(t, "Is the system healthy?")
	if r3["response"] == nil || r3["response"].(string) == "" {
		t.Error("query 3: expected non-empty response")
	}
}
