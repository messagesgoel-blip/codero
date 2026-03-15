package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// requiredScopes lists the GitHub OAuth scopes the token must carry.
// checks:write is not grantable via classic PAT (it does not appear in
// X-OAuth-Scopes for classic personal access tokens); the Checks API is
// not consumed in Phase 1. Re-add when Checks API usage is introduced.
var requiredScopes = []string{"repo", "admin:repo_hook"}

// githubUserEndpoint is the lightweight endpoint used for scope inspection.
// A GET to this endpoint always returns X-OAuth-Scopes regardless of body content.
const githubUserEndpoint = "https://api.github.com/user"

// ErrScopeCheck is returned when the GitHub API call itself fails (network,
// auth, non-2xx without scope header).
type ErrScopeCheck struct {
	Cause error
}

func (e *ErrScopeCheck) Error() string {
	return fmt.Sprintf("github scope check failed: %v", e.Cause)
}

func (e *ErrScopeCheck) Unwrap() error { return e.Cause }

// ErrMissingScopes is returned when the token lacks one or more required scopes.
type ErrMissingScopes struct {
	Missing []string
}

func (e *ErrMissingScopes) Error() string {
	return fmt.Sprintf("github token missing required scopes: %s", strings.Join(e.Missing, ", "))
}

// ValidateTokenScopes checks that token carries all required GitHub OAuth scopes.
// It makes a single GET request to the GitHub API and inspects X-OAuth-Scopes.
//
// client may be nil; http.DefaultClient is used in that case.
// Provide a custom *http.Client (e.g. with httptest transport) for tests.
//
// Returns:
//   - nil if all required scopes are present
//   - *ErrMissingScopes listing absent scopes
//   - *ErrScopeCheck if the HTTP call fails or returns an unexpected status
func ValidateTokenScopes(ctx context.Context, token string, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserEndpoint, nil)
	if err != nil {
		return &ErrScopeCheck{Cause: err}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return &ErrScopeCheck{Cause: err}
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return &ErrScopeCheck{Cause: fmt.Errorf("token rejected by GitHub API (401)")}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ErrScopeCheck{Cause: fmt.Errorf("GitHub API returned unexpected status %d", resp.StatusCode)}
	}

	scopesHeader := resp.Header.Get("X-OAuth-Scopes")
	granted := parseScopeHeader(scopesHeader)

	var missing []string
	for _, required := range requiredScopes {
		if !granted[required] {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return &ErrMissingScopes{Missing: missing}
	}
	return nil
}

// parseScopeHeader converts the comma-separated X-OAuth-Scopes header value
// into a set for O(1) membership checks.
func parseScopeHeader(header string) map[string]bool {
	result := make(map[string]bool)
	for _, part := range strings.Split(header, ",") {
		s := strings.TrimSpace(part)
		if s != "" {
			result[s] = true
		}
	}
	return result
}
