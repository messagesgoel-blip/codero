package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestServer returns an httptest.Server that serves the given handlers keyed
// by path. All requests must carry an Authorization header or the server
// returns 401. The caller is responsible for closing the server.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	srv := httptest.NewServer(mux)
	client := NewClient("test-token").WithHTTPClient(srv.Client())
	client.apiURL = srv.URL
	return srv, client
}

func jsonHandler(t *testing.T, v any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("jsonHandler encode: %v", err)
		}
	}
}

func TestFindOpenPR_Found(t *testing.T) {
	payload := []map[string]any{
		{
			"number": 42,
			"state":  "open",
			"head":   map[string]any{"sha": "abc123"},
		},
	}
	srv, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls": jsonHandler(t, payload),
	})
	defer srv.Close()

	pr, err := client.FindOpenPR(context.Background(), "owner/repo", "my-branch")
	if err != nil {
		t.Fatalf("FindOpenPR: %v", err)
	}
	if pr == nil {
		t.Fatal("expected PRInfo, got nil")
	}
	if pr.Number != 42 {
		t.Errorf("Number: want 42, got %d", pr.Number)
	}
	if pr.HeadSHA != "abc123" {
		t.Errorf("HeadSHA: want abc123, got %s", pr.HeadSHA)
	}
}

func TestFindOpenPR_NotFound(t *testing.T) {
	srv, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls": jsonHandler(t, []any{}),
	})
	defer srv.Close()

	pr, err := client.FindOpenPR(context.Background(), "owner/repo", "my-branch")
	if err != nil {
		t.Fatalf("FindOpenPR: %v", err)
	}
	if pr != nil {
		t.Errorf("expected nil PRInfo, got %+v", pr)
	}
}

func TestResolveApprovalStatus_Approved(t *testing.T) {
	reviews := []Review{
		{User: "alice", State: "APPROVED"},
	}
	approved, cr := resolveApprovalStatus(reviews)
	if !approved {
		t.Error("expected approved=true")
	}
	if cr {
		t.Error("expected changesRequested=false")
	}
}

func TestResolveApprovalStatus_ChangesRequested(t *testing.T) {
	reviews := []Review{
		{User: "alice", State: "APPROVED"},
		{User: "alice", State: "CHANGES_REQUESTED"}, // later review overrides
	}
	approved, cr := resolveApprovalStatus(reviews)
	if approved {
		t.Error("expected approved=false after CHANGES_REQUESTED override")
	}
	if !cr {
		t.Error("expected changesRequested=true")
	}
}

func TestIsCIGreen_AllSuccess(t *testing.T) {
	runs := []CheckRun{
		{Status: "completed", Conclusion: "success"},
		{Status: "completed", Conclusion: "skipped"},
	}
	if !isCIGreen(runs) {
		t.Error("expected CI green")
	}
}

func TestIsCIGreen_Failure(t *testing.T) {
	runs := []CheckRun{
		{Status: "completed", Conclusion: "success"},
		{Status: "completed", Conclusion: "failure"},
	}
	if isCIGreen(runs) {
		t.Error("expected CI not green due to failure")
	}
}

func TestIsCIGreen_InProgress(t *testing.T) {
	runs := []CheckRun{
		{Status: "in_progress", Conclusion: ""},
	}
	if isCIGreen(runs) {
		t.Error("expected CI not green when in_progress")
	}
}

func TestIsCIGreen_Empty(t *testing.T) {
	if isCIGreen(nil) {
		t.Error("expected CI not green with no runs")
	}
}

func TestListPRReviews(t *testing.T) {
	payload := []map[string]any{
		{
			"id":    float64(1),
			"user":  map[string]any{"login": "coderabbitai"},
			"state": "CHANGES_REQUESTED",
			"body":  "Please fix the bug.",
		},
	}
	srv, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls/7/reviews": jsonHandler(t, payload),
	})
	defer srv.Close()

	reviews, err := client.ListPRReviews(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListPRReviews: %v", err)
	}
	if len(reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(reviews))
	}
	if reviews[0].User != "coderabbitai" {
		t.Errorf("user: want coderabbitai, got %s", reviews[0].User)
	}
}

func TestListPRReviewComments(t *testing.T) {
	payload := []map[string]any{
		{
			"id":        float64(99),
			"user":      map[string]any{"login": "coderabbitai"},
			"body":      "Potential null pointer.",
			"path":      "main.go",
			"line":      float64(42),
			"commit_id": "deadbeef",
		},
	}
	srv, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls/7/comments": jsonHandler(t, payload),
	})
	defer srv.Close()

	comments, err := client.ListPRReviewComments(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatalf("ListPRReviewComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Path != "main.go" {
		t.Errorf("path: want main.go, got %s", comments[0].Path)
	}
	if comments[0].Line != 42 {
		t.Errorf("line: want 42, got %d", comments[0].Line)
	}
}
