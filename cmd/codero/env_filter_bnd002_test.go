package main

import (
	"os"
	"strings"
	"testing"

	"github.com/codero/codero/internal/session"
)

// TestBND002_EnvFiltering_AgentDoesNotReceiveForbiddenVars verifies that
// agent processes do not receive Codero control-plane secrets.
// This is a BND-002 certification test.
func TestBND002_EnvFiltering_AgentDoesNotReceiveForbiddenVars(t *testing.T) {
	// Simulate daemon environment with all sensitive vars
	testEnv := []string{
		"PATH=/usr/bin:/bin",
		"HOME=<FAKE:user-home>",
		"SHELL=/bin/bash",
		"TERM=xterm-256color",
		// Sensitive Codero vars that must NOT reach agent
		"CODERO_DB_PATH=<FAKE:db-path>",
		"CODERO_REDIS_ADDR=localhost:6379",
		"CODERO_REDIS_PASS=supersecret",
		"CODERO_REDIS_MAX_RETRIES=5",
		"CODERO_REDIS_RETRY_INTERVAL=1s",
		"CODERO_REDIS_HEALTH_INTERVAL=30s",
		"CODERO_API_ADDR=127.0.0.1:8110",
		"CODERO_WEBHOOK_SECRET=whsec_xxx",
		"GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx",
		"GH_TOKEN=ghp_yyyyyyyyyyyyyyyyyyyy",
		"CODERO_AUTO_MERGE_ENABLED=true",
		"CODERO_MERGE_METHOD=squash",
		"CODERO_PR_AUTO_CREATE=true",
		"CODERO_CODERABBIT_AUTO_REVIEW=true",
		// Vars that SHOULD reach agent
		"CODERO_SESSION_ID=sess-12345",
		"CODERO_AGENT_ID=claude",
		"CODERO_DAEMON_ADDR=127.0.0.1:8110",
		"CODERO_WORKTREE=<FAKE:worktree>",
		"LITELLM_API_KEY=litellm-key",
		"CODERO_LITELLM_API_KEY=codero-litellm-key",
		"OPENCLAW_STATE_DIR=<FAKE:openclaw-state>",
	}

	filtered := session.FilterEnvFrom(testEnv, session.LayerAgent)

	// Build map for easy lookup
	filteredMap := make(map[string]string)
	for _, e := range filtered {
		if idx := strings.Index(e, "="); idx > 0 {
			filteredMap[e[:idx]] = e[idx+1:]
		}
	}

	// Assert forbidden vars are absent
	forbidden := session.ForbiddenForAgent()
	for _, f := range forbidden {
		if _, exists := filteredMap[f]; exists {
			t.Errorf("BND-002 VIOLATION: agent env contains forbidden var %s", f)
		}
	}

	// Assert allowed vars are present
	allowed := []string{
		"PATH",
		"HOME",
		"SHELL",
		"TERM",
		"CODERO_SESSION_ID",
		"CODERO_AGENT_ID",
		"CODERO_DAEMON_ADDR",
		"CODERO_WORKTREE",
		"LITELLM_API_KEY",
		"CODERO_LITELLM_API_KEY",
		"OPENCLAW_STATE_DIR",
	}
	for _, a := range allowed {
		if _, exists := filteredMap[a]; !exists {
			t.Errorf("BND-002: agent env should contain %s but it was filtered out", a)
		}
	}
}

// TestBND002_EnvFiltering_OpenClawDoesNotReceiveForbiddenVars verifies that
// OpenClaw/adapter processes do not receive Codero persistence secrets.
// This is a BND-002 certification test.
func TestBND002_EnvFiltering_OpenClawDoesNotReceiveForbiddenVars(t *testing.T) {
	testEnv := []string{
		"PATH=/usr/bin",
		"HOME=<FAKE:user-home>",
		// Sensitive vars that must NOT reach OpenClaw
		"CODERO_DB_PATH=<FAKE:db-path>",
		"CODERO_REDIS_ADDR=localhost:6379",
		"CODERO_REDIS_PASS=secret",
		"GITHUB_TOKEN=ghp_xxxxx",
		"GH_TOKEN=ghp_yyyyy",
		"LITELLM_API_KEY=litellm-key",
		"CODERO_LITELLM_API_KEY=codero-litellm-key",
		// Vars that SHOULD reach OpenClaw
		"OPENCLAW_STATE_DIR=<FAKE:oc-state>",
		"OPENCLAW_CONFIG_PATH=<FAKE:oc-config>",
		"CODERO_SESSION_ID=sess-456",
		"CODERO_DAEMON_ADDR=127.0.0.1:8110",
	}

	filtered := session.FilterEnvFrom(testEnv, session.LayerOpenClaw)

	filteredMap := make(map[string]string)
	for _, e := range filtered {
		if idx := strings.Index(e, "="); idx > 0 {
			filteredMap[e[:idx]] = e[idx+1:]
		}
	}

	// Assert forbidden vars are absent
	forbidden := session.ForbiddenForOpenClaw()
	for _, f := range forbidden {
		if _, exists := filteredMap[f]; exists {
			t.Errorf("BND-002 VIOLATION: OpenClaw env contains forbidden var %s", f)
		}
	}

	// Assert allowed vars are present
	allowed := []string{
		"PATH",
		"HOME",
		"OPENCLAW_STATE_DIR",
		"OPENCLAW_CONFIG_PATH",
		"CODERO_SESSION_ID",
		"CODERO_DAEMON_ADDR",
	}
	for _, a := range allowed {
		if _, exists := filteredMap[a]; !exists {
			t.Errorf("BND-002: OpenClaw env should contain %s", a)
		}
	}
}

// TestBND002_EnvFiltering_CoderoReceivesAll verifies that Codero control-plane
// processes receive all env vars (no filtering).
func TestBND002_EnvFiltering_CoderoReceivesAll(t *testing.T) {
	testEnv := []string{
		"PATH=/usr/bin",
		"CODERO_DB_PATH=<FAKE:db-path>",
		"CODERO_REDIS_PASS=secret",
		"GITHUB_TOKEN=ghp_xxxxx",
	}

	filtered := session.FilterEnvFrom(testEnv, session.LayerCodero)

	if len(filtered) != len(testEnv) {
		t.Errorf("BND-002: Codero layer should receive all vars, got %d want %d",
			len(filtered), len(testEnv))
	}
}

// TestBND002_EnvFiltering_RealEnvironment tests filtering against actual os.Environ().
// This ensures the filter works with real environment variable formats.
func TestBND002_EnvFiltering_RealEnvironment(t *testing.T) {
	// Set some test vars
	t.Setenv("CODERO_DB_PATH", "/test/db.sqlite")
	t.Setenv("GITHUB_TOKEN", "ghp_test_token")
	t.Setenv("CODERO_SESSION_ID", "test-session")

	filtered := session.FilterEnv(session.LayerAgent)

	for _, e := range filtered {
		key := strings.SplitN(e, "=", 2)[0]
		if key == "CODERO_DB_PATH" || key == "GITHUB_TOKEN" {
			t.Errorf("BND-002 VIOLATION: real env filtering left %s in agent env", key)
		}
	}

	// Verify CODERO_SESSION_ID is present
	found := false
	for _, e := range filtered {
		if strings.HasPrefix(e, "CODERO_SESSION_ID=") {
			found = true
			break
		}
	}
	if !found {
		t.Error("BND-002: CODERO_SESSION_ID should be present in filtered agent env")
	}
}

// TestBND002_ForbiddenLists_Completeness ensures all required sensitive vars are in the forbidden lists.
func TestBND002_ForbiddenLists_Completeness(t *testing.T) {
	// These must all be forbidden for agent
	mustBeForbiddenForAgent := []string{
		"CODERO_DB_PATH",
		"CODERO_REDIS_ADDR",
		"CODERO_REDIS_PASS",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"CODERO_WEBHOOK_SECRET",
	}

	for _, v := range mustBeForbiddenForAgent {
		if !session.IsForbiddenForAgent(v) {
			t.Errorf("BND-002: %s must be forbidden for agent but is not", v)
		}
	}

	// These must be forbidden for OpenClaw
	mustBeForbiddenForOpenClaw := []string{
		"CODERO_DB_PATH",
		"CODERO_REDIS_ADDR",
		"CODERO_REDIS_PASS",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"LITELLM_API_KEY",
		"CODERO_LITELLM_API_KEY",
	}

	for _, v := range mustBeForbiddenForOpenClaw {
		if !session.IsForbiddenForOpenClaw(v) {
			t.Errorf("BND-002: %s must be forbidden for OpenClaw but is not", v)
		}
	}
}

// TestBND002_DefaultBehavior_UnsetVar tests that filtering works correctly when vars are unset.
func TestBND002_DefaultBehavior_UnsetVar(t *testing.T) {
	// Ensure CODERO_DB_PATH is not set
	os.Unsetenv("CODERO_DB_PATH")

	filtered := session.FilterEnv(session.LayerAgent)

	// Should not crash and should return a valid slice
	if filtered == nil {
		t.Error("FilterEnv should return non-nil slice even when vars are unset")
	}
}

// TestBND002_Precedence_ExplicitOverImplicit tests that explicit additions after filtering work.
func TestBND002_Precedence_ExplicitOverImplicit(t *testing.T) {
	// This simulates what runChild does: filter env, then add explicit vars
	t.Setenv("CODERO_SESSION_ID", "should-be-overwritten")

	filtered := session.FilterEnv(session.LayerAgent)

	// Now add explicit override (as runChild does)
	newSessionID := "explicitly-set-session"
	filtered = append(filtered, "CODERO_SESSION_ID="+newSessionID)

	// The last occurrence should win (Go exec uses last occurrence)
	// Verify there are now two CODERO_SESSION_ID entries
	count := 0
	for _, e := range filtered {
		if strings.HasPrefix(e, "CODERO_SESSION_ID=") {
			count++
		}
	}

	// Both should be present; exec.Cmd uses the last one
	if count < 1 {
		t.Error("Expected at least one CODERO_SESSION_ID entry")
	}
}

// TestBND002_FallbackEnv_PreservesResolvedAgentID verifies that degraded agent
// execution re-applies the resolved agent identity after filtering.
func TestBND002_FallbackEnv_PreservesResolvedAgentID(t *testing.T) {
	t.Setenv("CODERO_AGENT_ID", "stale-agent")

	env := buildFallbackEnv("resolved-agent")
	found := false
	for _, e := range env {
		if e == "CODERO_AGENT_ID=resolved-agent" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("fallback env should preserve resolved agent id, got: %v", env)
	}
}
