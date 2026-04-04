//go:build e2e

// tests/e2e/ocl013_audit_log_test.go
//
// OCL-013 E2E: Verify the operator query audit log.
// Requires a running openclaw-adapter at OPENCLAW_ADAPTER_URL (default http://127.0.0.1:8112)
// and Codero at CODERO_TEST_API_URL (default http://127.0.0.1:8110).

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestOCL013_AuditLogEndToEnd(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	adapter := adapterURL()

	// POST 3 queries to adapter directly.
	prompts := []string{"What sessions are active?", "List open PRs", "Is the system healthy?"}
	for _, p := range prompts {
		body, _ := json.Marshal(map[string]string{"prompt": p})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, adapter+"/query", bytes.NewReader(body))
		cancel()
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /query %q: %v", p, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("POST /query %q: unexpected status %d", p, resp.StatusCode)
		}
	}

	// GET /audit from adapter directly.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adapter+"/audit?limit=10", nil)
	if err != nil {
		t.Fatalf("build audit request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /audit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /audit: status %d: %s", resp.StatusCode, body)
	}

	var auditResp struct {
		Entries []struct {
			Ts             string `json:"ts"`
			Prompt         string `json:"prompt"`
			ConversationID string `json:"conversation_id"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&auditResp); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}

	if len(auditResp.Entries) < 3 {
		t.Fatalf("expected at least 3 audit entries, got %d", len(auditResp.Entries))
	}

	// Verify newest-first: first entry should have a timestamp >= second entry.
	if auditResp.Entries[0].Ts < auditResp.Entries[1].Ts {
		t.Errorf("entries not in newest-first order: %s < %s", auditResp.Entries[0].Ts, auditResp.Entries[1].Ts)
	}

	// Each entry should have ts, prompt, conversation_id.
	for i, e := range auditResp.Entries[:3] {
		if e.Ts == "" {
			t.Errorf("entry[%d] missing ts", i)
		}
		if e.Prompt == "" {
			t.Errorf("entry[%d] missing prompt", i)
		}
		if e.ConversationID == "" {
			t.Errorf("entry[%d] missing conversation_id", i)
		}
	}
}

func TestOCL013_AuditProxiedViaCodero(t *testing.T) {
	if os.Getenv("CODERO_E2E_LIVE") != "1" {
		t.Skip("CODERO_E2E_LIVE not set, skipping live test")
	}

	// GET /api/v1/openclaw/audit via Codero proxy.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := testAPIURL() + "/api/v1/openclaw/audit?limit=10"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/openclaw/audit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Entries []map[string]interface{} `json:"entries"`
		Total   int                      `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Total == 0 {
		t.Error("expected non-zero total from proxied audit")
	}
	t.Logf("Proxied audit returned %d entries", result.Total)
}
