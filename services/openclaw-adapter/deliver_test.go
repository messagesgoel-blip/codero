package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFormatFindings_Empty(t *testing.T) {
	msg := formatFindings(nil, "semgrep")
	if !strings.Contains(msg, "no findings") {
		t.Errorf("expected 'no findings' in message, got: %s", msg)
	}
	if !strings.Contains(msg, "semgrep") {
		t.Errorf("expected source name in message, got: %s", msg)
	}
}

func TestFormatFindings_MixedSeverity(t *testing.T) {
	findings := []finding{
		{Severity: "info", File: "readme.md", Line: 1, Message: "low priority"},
		{Severity: "error", File: "main.go", Line: 10, Message: "critical bug"},
		{Severity: "warning", File: "config.go", Line: 5, Message: "deprecated"},
	}
	msg := formatFindings(findings, "coderabbit")

	if !strings.Contains(msg, "(3 total)") {
		t.Errorf("expected '(3 total)' in message, got: %s", msg)
	}

	// Verify ordering: error before warning before info.
	errIdx := strings.Index(msg, "[ERROR]")
	warnIdx := strings.Index(msg, "[WARNING]")
	infoIdx := strings.Index(msg, "[INFO]")
	if errIdx < 0 || warnIdx < 0 || infoIdx < 0 {
		t.Fatalf("missing severity tags in message: %s", msg)
	}
	if !(errIdx < warnIdx && warnIdx < infoIdx) {
		t.Errorf("wrong order: ERROR@%d WARNING@%d INFO@%d", errIdx, warnIdx, infoIdx)
	}

	if !strings.Contains(msg, "main.go:10") {
		t.Error("missing file:line for error finding")
	}
}

func TestFormatFindings_Truncation(t *testing.T) {
	findings := make([]finding, 25)
	for i := range findings {
		findings[i] = finding{Severity: "warning", File: "f.go", Line: i + 1, Message: "msg"}
	}
	msg := formatFindings(findings, "test")

	if !strings.Contains(msg, "(25 total)") {
		t.Errorf("expected '(25 total)', got: %s", msg)
	}
	if !strings.Contains(msg, "... and 5 more") {
		t.Errorf("expected truncation note, got: %s", msg)
	}

	// Should have exactly 20 [WARNING] lines.
	count := strings.Count(msg, "[WARNING]")
	if count != 20 {
		t.Errorf("expected 20 findings displayed, got %d", count)
	}
}

func TestFormatFindings_EmptySeverity(t *testing.T) {
	findings := []finding{
		{File: "x.go", Line: 1, Message: "unknown sev"},
	}
	msg := formatFindings(findings, "test")
	if !strings.Contains(msg, "[INFO]") {
		t.Errorf("empty severity should default to INFO, got: %s", msg)
	}
}

func TestFindingSeverityRank(t *testing.T) {
	tests := []struct {
		sev  string
		want int
	}{
		{"error", 0},
		{"ERROR", 0},
		{"warning", 1},
		{"Warning", 1},
		{"info", 2},
		{"INFO", 2},
		{"unknown", 3},
		{"", 3},
	}
	for _, tc := range tests {
		got := findingSeverityRank(tc.sev)
		if got != tc.want {
			t.Errorf("findingSeverityRank(%q) = %d, want %d", tc.sev, got, tc.want)
		}
	}
}

func TestHandleDeliver_MissingSessionID(t *testing.T) {
	h := &handler{
		cfg:        adapterConfig{BaseURL: "http://localhost:9999"},
		httpClient: http.DefaultClient,
	}

	body := strings.NewReader(`{"findings": []}`)
	req := httptest.NewRequest(http.MethodPost, "/deliver", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleDeliver(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandleDeliver_SessionNotFound(t *testing.T) {
	// Mock Codero returning 404 for session lookup.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer mock.Close()

	h := &handler{
		cfg:        adapterConfig{BaseURL: mock.URL},
		httpClient: http.DefaultClient,
	}

	body := strings.NewReader(`{"session_id": "nonexistent", "findings": []}`)
	req := httptest.NewRequest(http.MethodPost, "/deliver", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleDeliver(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandleDeliver_NoTmuxSession(t *testing.T) {
	// Mock Codero returning session without tmux name.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"session_id": "sess-1",
			"mode":       "claude",
		})
	}))
	defer mock.Close()

	h := &handler{
		cfg:        adapterConfig{BaseURL: mock.URL},
		httpClient: http.DefaultClient,
	}

	body := strings.NewReader(`{"session_id": "sess-1", "findings": [{"severity":"error","file":"x.go","line":1,"message":"bad"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/deliver", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleDeliver(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for no tmux session, got %d", rec.Code)
	}
}

func TestHandleDeliver_BridgePathEmpty(t *testing.T) {
	// Mock Codero returning a valid session with tmux.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session_id":        "sess-1",
			"mode":              "claude",
			"tmux_session_name": "tmux-test",
		})
	}))
	defer mock.Close()

	h := &handler{
		cfg:        adapterConfig{BaseURL: mock.URL, BridgePath: ""},
		httpClient: http.DefaultClient,
	}

	body := strings.NewReader(`{"session_id":"sess-1","findings":[{"severity":"warning","file":"x.go","line":1,"message":"test"}],"source":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/deliver", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleDeliver(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for skipped delivery, got %d", rec.Code)
	}

	respBody, _ := io.ReadAll(rec.Body)
	var resp deliverResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if resp.Status != "skipped" {
		t.Errorf("expected status=skipped, got %s", resp.Status)
	}
}

func TestLookupSession_Success(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sess-abc") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session_id":        "sess-abc",
			"mode":              "codex",
			"tmux_session_name": "tmux-codex-1",
		})
	}))
	defer mock.Close()

	h := &handler{
		cfg:        adapterConfig{BaseURL: mock.URL},
		httpClient: http.DefaultClient,
	}

	sess, err := h.lookupSession(t.Context(), "sess-abc")
	if err != nil {
		t.Fatalf("lookupSession failed: %v", err)
	}
	if sess.SessionID != "sess-abc" {
		t.Errorf("unexpected session_id: %s", sess.SessionID)
	}
	if sess.Mode != "codex" {
		t.Errorf("unexpected mode: %s", sess.Mode)
	}
	if sess.TmuxSessionName != "tmux-codex-1" {
		t.Errorf("unexpected tmux_session_name: %s", sess.TmuxSessionName)
	}
}
