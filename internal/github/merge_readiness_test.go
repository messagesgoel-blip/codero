package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// fullPRResponse returns a mock PR JSON matching go-github's expected format.
func fullPRResponse(mergeable bool, mergeableState, headSHA, baseRef string) map[string]any {
	return map[string]any{
		"number":          42,
		"state":           "open",
		"mergeable":       mergeable,
		"mergeable_state": mergeableState,
		"head":            map[string]any{"sha": headSHA, "ref": "feat"},
		"base":            map[string]any{"ref": baseRef},
	}
}

func TestEvaluateMergeReadiness_AllPass(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls/42": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(fullPRResponse(true, "clean", "abc123", "main"))
		},
		"/repos/owner/repo/commits/abc123/check-runs": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"check_runs": []map[string]any{
					{"name": "ci", "status": "completed", "conclusion": "success"},
				},
			})
		},
		"/repos/owner/repo/pulls/42/reviews": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "user": map[string]any{"login": "alice"}, "state": "APPROVED", "body": "lgtm", "commit_id": "abc123"},
			})
		},
		"/repos/owner/repo/branches/main/protection": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"required_status_checks": map[string]any{"strict": true},
			})
		},
	}
	srv, client := newTestServer(t, handlers)
	defer srv.Close()

	mr, err := client.EvaluateMergeReadiness(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("EvaluateMergeReadiness: %v", err)
	}
	if !mr.MergeReady() {
		t.Errorf("expected MergeReady()=true, blocking: %v", mr.BlockingReasons())
	}
	if len(mr.BlockingReasons()) != 0 {
		t.Errorf("expected no blocking reasons, got %v", mr.BlockingReasons())
	}
}

func TestEvaluateMergeReadiness_CIFail(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls/42": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(fullPRResponse(true, "clean", "abc123", "main"))
		},
		"/repos/owner/repo/commits/abc123/check-runs": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"check_runs": []map[string]any{
					{"name": "ci", "status": "completed", "conclusion": "failure"},
				},
			})
		},
		"/repos/owner/repo/pulls/42/reviews": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{})
		},
		"/repos/owner/repo/branches/main/protection": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	}
	srv, client := newTestServer(t, handlers)
	defer srv.Close()

	mr, err := client.EvaluateMergeReadiness(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("EvaluateMergeReadiness: %v", err)
	}
	if mr.MergeReady() {
		t.Error("expected MergeReady()=false when CI failing")
	}
	reasons := mr.BlockingReasons()
	found := false
	for _, r := range reasons {
		if strings.Contains(r, "CI status") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected blocking reason about CI, got %v", reasons)
	}
}

func TestEvaluateMergeReadiness_MergeConflicts(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls/42": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(fullPRResponse(false, "dirty", "abc123", "main"))
		},
		"/repos/owner/repo/commits/abc123/check-runs": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"total_count": 1,
				"check_runs": []map[string]any{
					{"name": "ci", "status": "completed", "conclusion": "success"},
				},
			})
		},
		"/repos/owner/repo/pulls/42/reviews": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{})
		},
		"/repos/owner/repo/branches/main/protection": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	}
	srv, client := newTestServer(t, handlers)
	defer srv.Close()

	mr, err := client.EvaluateMergeReadiness(context.Background(), "owner/repo", 42)
	if err != nil {
		t.Fatalf("EvaluateMergeReadiness: %v", err)
	}
	if mr.MergeReady() {
		t.Error("expected MergeReady()=false when conflicts exist")
	}
	reasons := mr.BlockingReasons()
	found := false
	for _, r := range reasons {
		if strings.Contains(r, "merge conflicts") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected blocking reason about conflicts, got %v", reasons)
	}
}

func TestMergePRWithMethod_StaleSHA(t *testing.T) {
	handlers := map[string]http.HandlerFunc{
		"/repos/owner/repo/pulls/42": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"number": 42,
				"head":   map[string]any{"sha": "current-sha"},
			})
		},
	}
	srv, client := newTestServer(t, handlers)
	defer srv.Close()

	err := client.MergePRWithMethod(context.Background(), "owner/repo", 42, "stale-sha")
	if err == nil {
		t.Fatal("expected error for stale SHA")
	}
	if !strings.Contains(err.Error(), "stale SHA") {
		t.Errorf("error should mention stale SHA, got: %s", err.Error())
	}
}

func TestMergeMethod_Default(t *testing.T) {
	t.Setenv("CODERO_MERGE_METHOD", "")
	if m := MergeMethod(); m != "squash" {
		t.Errorf("default method: got %q, want squash", m)
	}
}

func TestMergeMethod_Rebase(t *testing.T) {
	t.Setenv("CODERO_MERGE_METHOD", "rebase")
	if m := MergeMethod(); m != "rebase" {
		t.Errorf("method: got %q, want rebase", m)
	}
}
