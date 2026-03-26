package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCreatePR_Success(t *testing.T) {
	srv, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls": func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			// Verify request body
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			json.Unmarshal(body, &req)
			if req["title"] != "feat: add thing" {
				t.Errorf("title: got %q", req["title"])
			}
			if req["head"] != "feat-branch" {
				t.Errorf("head: got %q", req["head"])
			}
			if req["base"] != "main" {
				t.Errorf("base: got %q", req["base"])
			}

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"number": 99,
				"state":  "open",
			})
		},
	})
	defer srv.Close()

	num, err := client.CreatePR(context.Background(), "owner/repo", "feat-branch", "main", "feat: add thing", "body text")
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if num != 99 {
		t.Errorf("PR number: want 99, got %d", num)
	}
}

func TestCreatePR_AlreadyExists(t *testing.T) {
	srv, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]any{
				"message": "Validation Failed",
				"errors": []map[string]any{
					{"message": "A pull request already exists for owner:feat-branch."},
				},
			})
		},
	})
	defer srv.Close()

	_, err := client.CreatePR(context.Background(), "owner/repo", "feat-branch", "main", "title", "body")
	if err == nil {
		t.Fatal("expected error for 422")
	}
	// Error should be actionable
	if !strings.Contains(err.Error(), "422") && !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "Validation") {
		t.Errorf("error should be actionable, got: %s", err.Error())
	}
}

func TestPostReviewComment_CodeRabbit(t *testing.T) {
	var capturedBody string
	srv, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/owner/repo/issues/42/comments": func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			json.Unmarshal(body, &req)
			capturedBody = req["body"].(string)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1})
		},
	})
	defer srv.Close()

	err := client.PostReviewComment(context.Background(), "owner/repo", 42, "@coderabbitai review")
	if err != nil {
		t.Fatalf("PostReviewComment: %v", err)
	}
	if capturedBody != "@coderabbitai review" {
		t.Errorf("comment body: got %q, want %q", capturedBody, "@coderabbitai review")
	}
}

func TestCreatePRIfEnabled_Disabled(t *testing.T) {
	t.Setenv("CODERO_PR_AUTO_CREATE", "false")
	// Should return immediately without calling API
	client := NewClient("test-token")
	num, created, err := client.CreatePRIfEnabled(context.Background(), "owner/repo", "head", "base", "title", "body")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Error("expected created=false when disabled")
	}
	if num != 0 {
		t.Errorf("expected num=0, got %d", num)
	}
}

func TestTriggerCodeRabbitReview_Disabled(t *testing.T) {
	t.Setenv("CODERO_CODERABBIT_AUTO_REVIEW", "false")
	client := NewClient("test-token")
	err := client.TriggerCodeRabbitReview(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be a no-op (no API call)
}

func TestTriggerCodeRabbitReview_Enabled(t *testing.T) {
	t.Setenv("CODERO_CODERABBIT_AUTO_REVIEW", "true")

	var capturedBody string
	srv, client := newTestServer(t, map[string]http.HandlerFunc{
		"/repos/owner/repo/issues/42/comments": func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			json.Unmarshal(body, &req)
			capturedBody = req["body"].(string)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1})
		},
	})
	defer srv.Close()

	err := client.TriggerCodeRabbitReview(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("TriggerCodeRabbitReview: %v", err)
	}
	if capturedBody != "@coderabbitai review" {
		t.Errorf("expected @coderabbitai review, got %q", capturedBody)
	}
}
