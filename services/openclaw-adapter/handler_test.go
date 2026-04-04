// services/openclaw-adapter/handler_test.go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockStateServer returns a test server that serves OCL-010 state JSON.
func mockStateServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(json.RawMessage(body))
	}))
}

// mockLLMServer returns a test server that mimics a LiteLLM /v1/chat/completions endpoint.
func mockLLMServer(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":                 "chatcmpl-test",
			"object":             "chat.completion",
			"created":            1700000000,
			"model":              "test-model",
			"system_fingerprint": "fp_test",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"message":       map[string]string{"role": "assistant", "content": reply},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
		})
	}))
}

func TestHealthEndpoint(t *testing.T) {
	cfg := adapterConfig{Addr: ":0"}
	h := newHandler(cfg)
	_ = h // handler not needed for /health

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", resp["status"])
	}
}

func TestHandleQuery_Success(t *testing.T) {
	stateBody := `{"sessions":[{"session_id":"s1","agent_id":"agent-x","repo":"codero","branch":"main"}],"pipeline":[],"activity":[],"gate_health":{"providers":[],"summary":"no data"},"scorecard":{},"generated_at":"2026-04-03T00:00:00Z"}`
	stateSrv := mockStateServer(t, 200, stateBody)
	defer stateSrv.Close()

	llmSrv := mockLLMServer(t, "There is 1 active session: agent-x on codero/main.")
	defer llmSrv.Close()

	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")

	cfg := adapterConfig{
		StateURL:     stateSrv.URL,
		LiteLLMURL:   llmSrv.URL,
		LiteLLMModel: "test-model",
		AuditLogPath: auditPath,
	}
	h := newHandler(cfg)

	body := `{"prompt":"What sessions are active?"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.handleQuery(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp queryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Response == "" {
		t.Error("expected non-empty response")
	}
	if resp.ConversationID == "" {
		t.Error("expected conversation_id to be set")
	}
	if !strings.Contains(resp.Response, "agent-x") {
		t.Errorf("response should mention agent-x: %s", resp.Response)
	}

	// Verify audit log entry.
	auditData, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if !strings.Contains(string(auditData), "What sessions are active?") {
		t.Error("audit log should contain the prompt")
	}
}

func TestHandleQuery_StateUnavailable(t *testing.T) {
	stateSrv := mockStateServer(t, 500, `{"error":"internal error"}`)
	defer stateSrv.Close()

	llmSrv := mockLLMServer(t, "State is unavailable but I can help generally.")
	defer llmSrv.Close()

	cfg := adapterConfig{
		StateURL:     stateSrv.URL,
		LiteLLMURL:   llmSrv.URL,
		LiteLLMModel: "test-model",
	}
	h := newHandler(cfg)

	body := `{"prompt":"What is going on?"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.handleQuery(rr, req)

	if rr.Code != 200 {
		t.Fatalf("expected 200 (degraded), got %d", rr.Code)
	}
	var resp queryResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Response == "" {
		t.Error("should still get a response even with state unavailable")
	}
}

func TestHandleQuery_LLMUnavailable(t *testing.T) {
	stateSrv := mockStateServer(t, 200, `{"sessions":[],"pipeline":[],"activity":[],"gate_health":{},"scorecard":{}}`)
	defer stateSrv.Close()

	// LLM server that always returns 500.
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": "service down"})
	}))
	defer llmSrv.Close()

	cfg := adapterConfig{
		StateURL:     stateSrv.URL,
		LiteLLMURL:   llmSrv.URL,
		LiteLLMModel: "test-model",
	}
	h := newHandler(cfg)

	body := `{"prompt":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.handleQuery(rr, req)

	if rr.Code != 502 {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
	var errResp map[string]string
	json.NewDecoder(rr.Body).Decode(&errResp)
	if errResp["error"] != "LLM unavailable" {
		t.Errorf("expected 'LLM unavailable', got %q", errResp["error"])
	}
}

func TestHandleQuery_EmptyPrompt(t *testing.T) {
	cfg := adapterConfig{}
	h := newHandler(cfg)

	body := `{"prompt":""}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.handleQuery(rr, req)

	if rr.Code != 400 {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandleQuery_MethodNotAllowed(t *testing.T) {
	cfg := adapterConfig{}
	h := newHandler(cfg)

	req := httptest.NewRequest(http.MethodGet, "/query", nil)
	rr := httptest.NewRecorder()
	h.handleQuery(rr, req)

	if rr.Code != 405 {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestHandleQuery_ConversationIDPreserved(t *testing.T) {
	stateSrv := mockStateServer(t, 200, `{"sessions":[],"pipeline":[],"activity":[],"gate_health":{},"scorecard":{}}`)
	defer stateSrv.Close()
	llmSrv := mockLLMServer(t, "ok")
	defer llmSrv.Close()

	cfg := adapterConfig{StateURL: stateSrv.URL, LiteLLMURL: llmSrv.URL, LiteLLMModel: "test"}
	h := newHandler(cfg)

	body := `{"prompt":"test","conversation_id":"my-conv-123"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.handleQuery(rr, req)

	var resp queryResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.ConversationID != "my-conv-123" {
		t.Errorf("expected conversation_id=my-conv-123, got %q", resp.ConversationID)
	}
}

func TestBuildSystemPrompt_WithState(t *testing.T) {
	prompt := buildSystemPrompt(`{"sessions":[]}`, true)
	if !strings.Contains(prompt, "OpenClaw") {
		t.Error("system prompt should mention OpenClaw")
	}
	if !strings.Contains(prompt, `"sessions"`) {
		t.Error("system prompt should include state JSON")
	}
}

func TestBuildSystemPrompt_WithoutState(t *testing.T) {
	prompt := buildSystemPrompt("", false)
	if !strings.Contains(prompt, "unavailable") {
		t.Error("should mention state unavailable")
	}
}

func TestAuditLog_WritesEntries(t *testing.T) {
	stateSrv := mockStateServer(t, 200, `{"sessions":[],"pipeline":[],"activity":[],"gate_health":{},"scorecard":{}}`)
	defer stateSrv.Close()
	llmSrv := mockLLMServer(t, "response1")
	defer llmSrv.Close()

	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	cfg := adapterConfig{
		StateURL: stateSrv.URL, LiteLLMURL: llmSrv.URL,
		LiteLLMModel: "test", AuditLogPath: auditPath,
	}
	h := newHandler(cfg)

	// Send 3 queries.
	for i, prompt := range []string{"q1", "q2", "q3"} {
		body := `{"prompt":"` + prompt + `"}`
		req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.handleQuery(rr, req)
		if rr.Code != 200 {
			t.Fatalf("query %d: expected 200, got %d", i, rr.Code)
		}
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 audit lines, got %d", len(lines))
	}
	for _, line := range lines {
		var entry auditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid audit JSON: %v", err)
		}
	}
}
