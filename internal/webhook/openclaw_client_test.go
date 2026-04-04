package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codero/codero/internal/state"
)

func TestOpenClawClient_Deliver_HappyPath(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/deliver" {
			t.Errorf("expected /deliver, got %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	}))
	defer srv.Close()

	client := NewOpenClawClient(srv.URL, srv.Client())
	findings := []*state.FindingRecord{
		{Severity: "error", File: "main.go", Line: 10, Message: "unused var", RuleID: "CR-1"},
	}
	err := client.Deliver(context.Background(), "sess-1", findings, "test-source")
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if gotBody["session_id"] != "sess-1" {
		t.Errorf("session_id: got %v, want sess-1", gotBody["session_id"])
	}
	if gotBody["source"] != "test-source" {
		t.Errorf("source: got %v, want test-source", gotBody["source"])
	}
	findingsArr, _ := gotBody["findings"].([]any)
	if len(findingsArr) != 1 {
		t.Fatalf("findings count: got %d, want 1", len(findingsArr))
	}
}

func TestOpenClawClient_Deliver_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewOpenClawClient(srv.URL, srv.Client())
	findings := []*state.FindingRecord{{Severity: "warning", File: "x.go", Line: 1, Message: "test"}}
	err := client.Deliver(context.Background(), "sess-1", findings, "test")
	if err != nil {
		t.Errorf("expected nil error on server error, got: %v", err)
	}
}

func TestOpenClawClient_Deliver_Unreachable(t *testing.T) {
	client := NewOpenClawClient("http://127.0.0.1:1", &http.Client{Timeout: 1 * time.Second})
	findings := []*state.FindingRecord{{Severity: "info", File: "x.go", Line: 1, Message: "test"}}
	err := client.Deliver(context.Background(), "sess-1", findings, "test")
	if err != nil {
		t.Errorf("expected nil error on unreachable server, got: %v", err)
	}
}

func TestOpenClawClient_Deliver_EmptyFindings(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewOpenClawClient(srv.URL, srv.Client())
	err := client.Deliver(context.Background(), "sess-1", nil, "test")
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if called {
		t.Error("expected no HTTP call for empty findings")
	}
}
