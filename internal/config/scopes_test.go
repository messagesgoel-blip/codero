package config

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// mockScopeServer creates a test server that returns the given scope header.
func mockScopeServer(t *testing.T, scopeHeader string, status int) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if scopeHeader != "" {
			w.Header().Set("X-OAuth-Scopes", scopeHeader)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	// Return a client that targets the test server by rewriting all requests.
	transport := &rewriteTransport{base: http.DefaultTransport, target: srv.URL}
	return srv, &http.Client{Transport: transport}
}

// rewriteTransport redirects all requests to target, preserving path and headers.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	targetURL, err := url.Parse(rt.target)
	if err != nil {
		return nil, err
	}
	clone.URL.Scheme = targetURL.Scheme
	clone.URL.Host = targetURL.Host
	return rt.base.RoundTrip(clone)
}

func TestValidateTokenScopes_AllPresent(t *testing.T) {
	_, client := mockScopeServer(t, "repo, checks:write, admin:repo_hook", http.StatusOK)
	err := ValidateTokenScopes(context.Background(), "ghp_test", client)
	if err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestValidateTokenScopes_ExtraScopes(t *testing.T) {
	// Extra scopes beyond required must not fail.
	_, client := mockScopeServer(t, "repo, checks:write, admin:repo_hook, read:org, workflow", http.StatusOK)
	err := ValidateTokenScopes(context.Background(), "ghp_test", client)
	if err != nil {
		t.Errorf("extra scopes should not fail, got: %v", err)
	}
}

func TestValidateTokenScopes_MissingOne(t *testing.T) {
	_, client := mockScopeServer(t, "repo, checks:write", http.StatusOK)
	err := ValidateTokenScopes(context.Background(), "ghp_test", client)
	if err == nil {
		t.Fatal("expected error for missing scope, got nil")
	}
	var scopeErr *ErrMissingScopes
	if !errors.As(err, &scopeErr) {
		t.Fatalf("want *ErrMissingScopes, got %T: %v", err, err)
	}
	if len(scopeErr.Missing) != 1 || scopeErr.Missing[0] != "admin:repo_hook" {
		t.Errorf("Missing: got %v, want [admin:repo_hook]", scopeErr.Missing)
	}
}

func TestValidateTokenScopes_AllMissing(t *testing.T) {
	_, client := mockScopeServer(t, "", http.StatusOK)
	err := ValidateTokenScopes(context.Background(), "ghp_test", client)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var scopeErr *ErrMissingScopes
	if !errors.As(err, &scopeErr) {
		t.Fatalf("want *ErrMissingScopes, got %T", err)
	}
	if len(scopeErr.Missing) != len(requiredScopes) {
		t.Errorf("Missing count: got %d, want %d", len(scopeErr.Missing), len(requiredScopes))
	}
}

func TestValidateTokenScopes_Unauthorized(t *testing.T) {
	_, client := mockScopeServer(t, "", http.StatusUnauthorized)
	err := ValidateTokenScopes(context.Background(), "ghp_bad", client)
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	var scopeErr *ErrScopeCheck
	if !errors.As(err, &scopeErr) {
		t.Fatalf("want *ErrScopeCheck, got %T: %v", err, err)
	}
}

func TestValidateTokenScopes_ServerError(t *testing.T) {
	_, client := mockScopeServer(t, "", http.StatusInternalServerError)
	err := ValidateTokenScopes(context.Background(), "ghp_test", client)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	var scopeErr *ErrScopeCheck
	if !errors.As(err, &scopeErr) {
		t.Fatalf("want *ErrScopeCheck, got %T: %v", err, err)
	}
}

func TestValidateTokenScopes_NetworkFailure(t *testing.T) {
	// Point at a non-listening port to simulate network failure.
	badClient := &http.Client{
		Transport: &rewriteTransport{
			base:   http.DefaultTransport,
			target: "http://127.0.0.1:19997",
		},
	}
	err := ValidateTokenScopes(context.Background(), "ghp_test", badClient)
	if err == nil {
		t.Fatal("expected error for network failure, got nil")
	}
	var scopeErr *ErrScopeCheck
	if !errors.As(err, &scopeErr) {
		t.Fatalf("want *ErrScopeCheck, got %T: %v", err, err)
	}
}

func TestParseScopeHeader_EmptyString(t *testing.T) {
	m := parseScopeHeader("")
	if len(m) != 0 {
		t.Errorf("expected empty map for empty header, got %v", m)
	}
}

func TestParseScopeHeader_Whitespace(t *testing.T) {
	m := parseScopeHeader("  repo  ,  checks:write  ")
	if !m["repo"] || !m["checks:write"] {
		t.Errorf("whitespace not trimmed: %v", m)
	}
}
