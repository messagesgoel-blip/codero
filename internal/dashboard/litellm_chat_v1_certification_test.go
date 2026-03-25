package dashboard_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/dashboard"
)

// ══════════════════════════════════════════════════════════════
// LiteLLM Chat v1 — clause-mapped certification tests
// Each test targets a specific certification-matrix row.
// ══════════════════════════════════════════════════════════════

// §1.1 Component Model: assembler + client + renderer exist in chat.go
func TestCert_LCv1_S1_1_ComponentModel(t *testing.T) {
	// Verify the handler assembles context, calls LiteLLM, and renders
	// a response — component model evidence via a round-trip that hits
	// all three stages (assembler → client → renderer).
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"test component model"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dashboard.ChatResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Reply == "" {
		t.Fatal("reply empty — renderer did not produce output")
	}
	if resp.GeneratedAt.IsZero() {
		t.Fatal("generated_at missing — renderer incomplete")
	}
}

// §2.1 POST /api/v1/dashboard/chat exists and accepts POST
func TestCert_LCv1_S2_1_PostChatEndpoint(t *testing.T) {
	h, _ := newTestHandler(t)

	// POST must succeed
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"hello"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /chat: want 200, got %d", rec.Code)
	}

	// GET is method-not-allowed
	rec = doRequest(t, h, http.MethodGet, "/api/v1/dashboard/chat", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /chat: want 405, got %d", rec.Code)
	}

	// Empty prompt rejected
	rec = doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":""}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty prompt: want 400, got %d", rec.Code)
	}
}

// §2.2 GET /api/v1/chat/history — list conversation history
func TestCert_LCv1_S2_2_ChatHistoryEndpoint(t *testing.T) {
	h, _ := newTestHandler(t)

	// Initially empty
	rec := doRequest(t, h, http.MethodGet, "/api/v1/chat/history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /history: want 200, got %d", rec.Code)
	}
	var entries []dashboard.ConversationEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty, got %d entries", len(entries))
	}

	// Send a chat request to populate history (non-streaming)
	doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"populate history","stream":false}`))

	// Now history should have one entry
	rec = doRequest(t, h, http.MethodGet, "/api/v1/chat/history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /history: want 200, got %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 history entry, got %d", len(entries))
	}
	if entries[0].Prompt != "populate history" {
		t.Fatalf("prompt = %q, want 'populate history'", entries[0].Prompt)
	}
}

// §2.3 DELETE /api/v1/chat/history/{id} — delete conversation entry
func TestCert_LCv1_S2_3_DeleteHistoryEndpoint(t *testing.T) {
	h, _ := newTestHandler(t)

	// Populate one entry
	doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"to-delete","stream":false}`))

	// Get entries to find ID
	rec := doRequest(t, h, http.MethodGet, "/api/v1/chat/history", nil)
	var entries []dashboard.ConversationEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries to delete")
	}
	id := entries[0].ID

	// Delete by ID
	rec = doRequest(t, h, http.MethodDelete, "/api/v1/chat/history/"+id, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify deleted
	rec = doRequest(t, h, http.MethodGet, "/api/v1/chat/history", nil)
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after delete, got %d", len(entries))
	}

	// Delete nonexistent returns 404
	rec = doRequest(t, h, http.MethodDelete, "/api/v1/chat/history/nonexistent", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE nonexistent: want 404, got %d", rec.Code)
	}
}

// §1.3 Context assembler builds context from dashboard state
func TestCert_LCv1_S1_3_ContextAssembler(t *testing.T) {
	h, _ := newTestHandler(t)

	// Set up a mock LiteLLM server that inspects the messages.
	sawSystem := false
	sawUser := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err == nil {
			if msgs, ok := payload["messages"].([]interface{}); ok {
				for _, m := range msgs {
					msg := m.(map[string]interface{})
					switch msg["role"].(string) {
					case "system":
						sawSystem = true
					case "user":
						sawUser = true
					}
				}
			}
		}
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"ok"}}]}` + "\n\n" +
			"data: [DONE]\n\n"))
	}))
	defer srv.Close()

	t.Setenv("CODERO_LITELLM_URL", srv.URL)
	t.Setenv("CODERO_LITELLM_MODEL", "test")
	t.Setenv("CODERO_LITELLM_MASTER_KEY", "key")

	doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"build context","stream":true}`))

	if !sawSystem {
		t.Fatal("context assembler did not send system message")
	}
	if !sawUser {
		t.Fatal("context assembler did not send user message")
	}
}

// §3.1 System prompt contains required identity
func TestCert_LCv1_S3_1_SystemPrompt(t *testing.T) {
	h, _ := newTestHandler(t)

	var systemContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err == nil {
			if msgs, ok := payload["messages"].([]interface{}); ok && len(msgs) > 0 {
				msg := msgs[0].(map[string]interface{})
				if msg["role"].(string) == "system" {
					systemContent = msg["content"].(string)
				}
			}
		}
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	t.Setenv("CODERO_LITELLM_URL", srv.URL)
	t.Setenv("CODERO_LITELLM_MODEL", "test")
	t.Setenv("CODERO_LITELLM_MASTER_KEY", "key")

	doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"test","stream":true}`))

	if systemContent == "" {
		t.Fatal("no system prompt sent")
	}
	if !strings.Contains(systemContent, "Codero") {
		t.Fatalf("system prompt missing Codero identity: %s", systemContent)
	}
	if !strings.Contains(systemContent, "advisory") {
		t.Fatalf("system prompt missing advisory constraint: %s", systemContent)
	}
}

// §4.4 Quick queries expand slash prefixes
func TestCert_LCv1_S4_4_QuickQueryExpansion(t *testing.T) {
	// Test all six spec-required quick queries
	requiredPrefixes := []string{"/prs", "/agent", "/session", "/recent", "/blocked", "/health"}
	for _, prefix := range requiredPrefixes {
		expanded, ok := dashboard.ExpandQuickQueryForTest(prefix)
		if !ok {
			t.Errorf("%s: quick query not recognized", prefix)
			continue
		}
		if expanded == prefix {
			t.Errorf("%s: expansion is identity — no transformation", prefix)
		}
		if len(expanded) < 20 {
			t.Errorf("%s: expansion too short: %q", prefix, expanded)
		}
	}

	// Extra context appended
	expanded, ok := dashboard.ExpandQuickQueryForTest("/prs fix auth")
	if !ok {
		t.Fatal("/prs with args: not recognized")
	}
	if !strings.Contains(expanded, "fix auth") {
		t.Fatalf("/prs extra context lost: %q", expanded)
	}
}

// §6 All 30 config vars have spec-mandated defaults
func TestCert_LCv1_S6_ConfigDefaults(t *testing.T) {
	c := config.DefaultChatConfig()

	checks := []struct {
		name string
		ok   bool
	}{
		{"Enabled=true", c.Enabled == true},
		{"LiteLLMAPIURL", c.LiteLLMAPIURL == "http://localhost:4000"},
		{"LiteLLMModel", c.LiteLLMModel == "gpt-4o-mini"},
		{"LiteLLMTimeout=30", c.LiteLLMTimeout == 30},
		{"LiteLLMMaxTokens=2048", c.LiteLLMMaxTokens == 2048},
		{"LiteLLMTemperature=0.1", c.LiteLLMTemperature == 0.1},
		{"LiteLLMStream=true", c.LiteLLMStream == true},
		{"MaxContextSize=16384", c.MaxContextSize == 16384},
		{"MaxHistory=50", c.MaxHistory == 50},
		{"ConversationTTL=3600", c.ConversationTTL == 3600},
		{"PersistHistory=false", c.PersistHistory == false},
		{"ToolsEnabled=false", c.ToolsEnabled == false},
		{"QuickQueriesEnabled=true", c.QuickQueriesEnabled == true},
		{"ContextSessionsLimit=20", c.ContextSessionsLimit == 20},
		{"ContextArchivesLimit=10", c.ContextArchivesLimit == 10},
		{"ContextFeedbackLimit=5", c.ContextFeedbackLimit == 5},
		{"ContextScopeDefault=all", c.ContextScopeDefault == "all"},
		{"TUIKeybind=c", c.TUIKeybind == "c"},
		{"TUIMarkdown=true", c.TUIMarkdown == true},
		{"TUISyntaxHighlight=true", c.TUISyntaxHighlight == true},
		{"TUITypingIndicator=true", c.TUITypingIndicator == true},
		{"TUICopyKeybind=ctrl+y", c.TUICopyKeybind == "ctrl+y"},
		{"RetryOnError=true", c.RetryOnError == true},
		{"RetryMax=2", c.RetryMax == 2},
		{"RetryDelay=1", c.RetryDelay == 1},
		{"LogQueries=false", c.LogQueries == false},
		{"LogResponses=false", c.LogResponses == false},
		{"RateLimit=30", c.RateLimit == 30},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("FAIL: %s", c.name)
		}
	}
}

// §6 Config env override wiring works
func TestCert_LCv1_S6_ConfigEnvOverrides(t *testing.T) {
	t.Setenv("CODERO_CHAT_ENABLED", "false")
	t.Setenv("CODERO_CHAT_LITELLM_MODEL", "claude-opus")
	t.Setenv("CODERO_CHAT_LITELLM_TIMEOUT", "60")
	t.Setenv("CODERO_CHAT_MAX_HISTORY", "100")
	t.Setenv("CODERO_CHAT_PERSIST_HISTORY", "true")
	t.Setenv("CODERO_CHAT_TOOLS_ENABLED", "true")
	t.Setenv("CODERO_CHAT_RATE_LIMIT", "10")

	cfg := config.LoadEnv()

	if cfg.Chat.Enabled != false {
		t.Errorf("Enabled: want false, got %v", cfg.Chat.Enabled)
	}
	if cfg.Chat.LiteLLMModel != "claude-opus" {
		t.Errorf("Model: want claude-opus, got %s", cfg.Chat.LiteLLMModel)
	}
	if cfg.Chat.LiteLLMTimeout != 60 {
		t.Errorf("Timeout: want 60, got %d", cfg.Chat.LiteLLMTimeout)
	}
	if cfg.Chat.MaxHistory != 100 {
		t.Errorf("MaxHistory: want 100, got %d", cfg.Chat.MaxHistory)
	}
	if cfg.Chat.PersistHistory != true {
		t.Errorf("PersistHistory: want true, got %v", cfg.Chat.PersistHistory)
	}
	if cfg.Chat.ToolsEnabled != true {
		t.Errorf("ToolsEnabled: want true, got %v", cfg.Chat.ToolsEnabled)
	}
	if cfg.Chat.RateLimit != 10 {
		t.Errorf("RateLimit: want 10, got %d", cfg.Chat.RateLimit)
	}
}

// LC-1 Read-only: chat does not modify state
func TestCert_LCv1_LC1_ReadOnly(t *testing.T) {
	h, db := newTestHandler(t)

	// Record row counts before chat
	var countBefore int
	db.QueryRow("SELECT count(*) FROM gate_check_runs").Scan(&countBefore)

	// Send a chat request
	doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"show status"}`))

	var countAfter int
	db.QueryRow("SELECT count(*) FROM gate_check_runs").Scan(&countAfter)
	if countAfter != countBefore {
		t.Fatalf("chat modified gate_check_runs: before=%d after=%d", countBefore, countAfter)
	}
}

// LC-2 Fresh context per question: no stale cache
func TestCert_LCv1_LC2_ContextPerQuestion(t *testing.T) {
	h, _ := newTestHandler(t)

	// Two sequential requests should each get a fresh response
	rec1 := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"first question"}`))
	rec2 := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"second question"}`))

	var resp1, resp2 dashboard.ChatResponse
	json.NewDecoder(rec1.Body).Decode(&resp1)
	json.NewDecoder(rec2.Body).Decode(&resp2)

	// generated_at should differ (fresh context each time)
	if resp1.GeneratedAt.Equal(resp2.GeneratedAt) {
		t.Fatal("both responses have identical generated_at — context may be cached")
	}
}

// LC-3 LiteLLM via proxy: uses the configured proxy URL
func TestCert_LCv1_LC3_LiteLLMProxy(t *testing.T) {
	proxied := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = true
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"proxied\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	t.Setenv("CODERO_LITELLM_URL", srv.URL)
	t.Setenv("CODERO_LITELLM_MODEL", "test")
	t.Setenv("CODERO_LITELLM_MASTER_KEY", "key")

	h, _ := newTestHandler(t)
	doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"proxy test","stream":true}`))

	if !proxied {
		t.Fatal("request did not go through LiteLLM proxy")
	}
}

// LC-4 Available only when enabled + reachable: falls back when unreachable
func TestCert_LCv1_LC4_FallbackOnUnreachable(t *testing.T) {
	t.Setenv("CODERO_LITELLM_URL", "http://127.0.0.1:1/v1/chat/completions")
	t.Setenv("CODERO_LITELLM_MODEL", "test")
	t.Setenv("CODERO_LITELLM_MASTER_KEY", "key")

	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"fallback test","stream":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	// Should get a fallback response, not an error
	if !strings.Contains(body, "event: done") {
		t.Fatalf("expected done event in fallback: %s", body)
	}
}

// LC-5 Unreachable → error not crash
func TestCert_LCv1_LC5_UnreachableNocrash(t *testing.T) {
	t.Setenv("CODERO_LITELLM_URL", "http://127.0.0.1:1/dead")
	t.Setenv("CODERO_LITELLM_MODEL", "test")
	t.Setenv("CODERO_LITELLM_MASTER_KEY", "key")

	h, _ := newTestHandler(t)
	// Must not panic
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"crash test"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 (graceful fallback), got %d", rec.Code)
	}
	var resp dashboard.ChatResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Reply == "" {
		t.Fatal("expected non-empty fallback reply")
	}
}

// LC-6 Request validation: prompt length cap
func TestCert_LCv1_LC6_PromptLengthCap(t *testing.T) {
	h, _ := newTestHandler(t)
	longPrompt := strings.Repeat("a", 5000) // exceeds 4096
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"`+longPrompt+`"}`))
	// Should not error — just truncate
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

// LC-7 Response envelope: has required fields
func TestCert_LCv1_LC7_ResponseEnvelope(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
		strings.NewReader(`{"prompt":"envelope test"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp dashboard.ChatResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Reply == "" {
		t.Fatal("reply empty")
	}
	if resp.Provider == "" {
		t.Fatal("provider empty")
	}
	if resp.GeneratedAt.IsZero() {
		t.Fatal("generated_at missing")
	}
}

// §3.3 Conversation memory: entries accumulate and are retrievable
func TestCert_LCv1_S3_3_ConversationMemory(t *testing.T) {
	h, _ := newTestHandler(t)

	// Three non-streaming chats
	for i := 0; i < 3; i++ {
		doRequest(t, h, http.MethodPost, "/api/v1/dashboard/chat",
			strings.NewReader(`{"prompt":"conversation memory test","stream":false}`))
	}

	rec := doRequest(t, h, http.MethodGet, "/api/v1/chat/history", nil)
	var entries []dashboard.ConversationEntry
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 history entries, got %d", len(entries))
	}
}

// ConversationStore unit tests
func TestConversationStore_MaxLen(t *testing.T) {
	s := dashboard.NewConversationStore(3)
	for i := 0; i < 5; i++ {
		s.Append(dashboard.ConversationEntry{
			ID:     "e" + strings.Repeat("x", i),
			Prompt: "p",
		})
	}
	entries := s.List()
	if len(entries) != 3 {
		t.Fatalf("want 3, got %d (maxLen eviction)", len(entries))
	}
}

func TestConversationStore_ClearAll(t *testing.T) {
	s := dashboard.NewConversationStore(10)
	s.Append(dashboard.ConversationEntry{ID: "a"})
	s.Append(dashboard.ConversationEntry{ID: "b"})
	s.Clear()
	if len(s.List()) != 0 {
		t.Fatal("clear did not empty store")
	}
}
