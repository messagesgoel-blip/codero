package session

import (
	"slices"
	"strings"
	"testing"
)

func TestFilterEnvFrom_Agent(t *testing.T) {
	// Simulate a typical Codero daemon environment with sensitive vars
	environ := []string{
		"PATH=/usr/bin:/bin",
		"HOME=<FAKE:user-home>",
		"TERM=xterm-256color",
		"CODERO_SESSION_ID=sess-123",
		"CODERO_AGENT_ID=claude",
		"CODERO_DAEMON_ADDR=127.0.0.1:8110",
		"CODERO_DB_PATH=<FAKE:db-path>",
		"CODERO_REDIS_ADDR=localhost:6379",
		"CODERO_REDIS_PASS=secret123",
		"GITHUB_TOKEN=ghp_xxxxxxxxxxxx",
		"GH_TOKEN=ghp_yyyyyyyyyyyy",
		"CODERO_HEARTBEAT_SECRET=hbsecret",
		"CODERO_GITHUB_TOKEN=ghp_codero",
		"CODERO_LITELLM_MASTER_KEY=litellm-secret",
		"LITELLM_API_KEY=litellm-key",
		"CODERO_LITELLM_API_KEY=codero-litellm-key",
		"CODERO_AIDER_GEMINI_API_KEY=gemini-aider-secret",
		"CODERO_GEMINI_SECOND_PASS_API_KEY=gemini-second-pass-secret",
		"CODERO_WEBHOOK_SECRET=whsec_zzz",
		"CODERO_AUTO_MERGE_ENABLED=true",
		"OPENCLAW_STATE_DIR=<FAKE:openclaw-state>",
	}

	filtered := FilterEnvFrom(environ, LayerAgent)

	// Must NOT contain these
	forbidden := []string{
		"CODERO_DB_PATH",
		"CODERO_REDIS_ADDR",
		"CODERO_REDIS_PASS",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"CODERO_HEARTBEAT_SECRET",
		"CODERO_GITHUB_TOKEN",
		"CODERO_LITELLM_MASTER_KEY",
		"CODERO_AIDER_GEMINI_API_KEY",
		"CODERO_GEMINI_SECOND_PASS_API_KEY",
		"CODERO_WEBHOOK_SECRET",
		"CODERO_AUTO_MERGE_ENABLED",
	}
	for _, f := range forbidden {
		for _, e := range filtered {
			if strings.HasPrefix(e, f+"=") {
				t.Errorf("agent env should NOT contain %s, but found: %s", f, e)
			}
		}
	}

	// MUST contain these (allowed for agent)
	allowed := []string{
		"PATH",
		"HOME",
		"TERM",
		"CODERO_SESSION_ID",
		"CODERO_AGENT_ID",
		"CODERO_DAEMON_ADDR",
		"LITELLM_API_KEY",
		"CODERO_LITELLM_API_KEY",
		"OPENCLAW_STATE_DIR",
	}
	for _, a := range allowed {
		found := false
		for _, e := range filtered {
			if strings.HasPrefix(e, a+"=") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("agent env should contain %s, but it was missing", a)
		}
	}
}

func TestFilterEnvFrom_OpenClaw(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"CODERO_SESSION_ID=sess-456",
		"CODERO_DB_PATH=<FAKE:db-path>",
		"CODERO_REDIS_PASS=secret",
		"CODERO_HEARTBEAT_SECRET=hbsecret",
		"CODERO_GITHUB_TOKEN=ghp_codero",
		"LITELLM_API_KEY=litellm-key",
		"LITELLM_MASTER_KEY=litellm-secret",
		"CODERO_LITELLM_MASTER_KEY=litellm-secret",
		"CODERO_LITELLM_API_KEY=codero-litellm-key",
		"CODERO_AIDER_GEMINI_API_KEY=gemini-aider-secret",
		"CODERO_GEMINI_SECOND_PASS_API_KEY=gemini-second-pass-secret",
		"GITHUB_TOKEN=ghp_xxxxx",
		"OPENCLAW_STATE_DIR=<FAKE:oc-state>",
		"OPENCLAW_CONFIG_PATH=<FAKE:oc-config>",
	}

	filtered := FilterEnvFrom(environ, LayerOpenClaw)

	// Must NOT contain DB/Redis/GitHub secrets
	forbidden := []string{
		"CODERO_DB_PATH",
		"CODERO_REDIS_PASS",
		"CODERO_HEARTBEAT_SECRET",
		"CODERO_GITHUB_TOKEN",
		"LITELLM_API_KEY",
		"LITELLM_MASTER_KEY",
		"CODERO_LITELLM_MASTER_KEY",
		"CODERO_LITELLM_API_KEY",
		"CODERO_AIDER_GEMINI_API_KEY",
		"CODERO_GEMINI_SECOND_PASS_API_KEY",
		"GITHUB_TOKEN",
	}
	for _, f := range forbidden {
		for _, e := range filtered {
			if strings.HasPrefix(e, f+"=") {
				t.Errorf("openclaw env should NOT contain %s", f)
			}
		}
	}

	// MUST contain OPENCLAW_ vars
	mustHave := []string{
		"PATH",
		"OPENCLAW_STATE_DIR",
		"OPENCLAW_CONFIG_PATH",
		"CODERO_SESSION_ID",
	}
	for _, m := range mustHave {
		found := false
		for _, e := range filtered {
			if strings.HasPrefix(e, m+"=") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("openclaw env should contain %s", m)
		}
	}
}

func TestFilterEnvFrom_Codero(t *testing.T) {
	environ := []string{
		"CODERO_DB_PATH=<FAKE:db-path>",
		"CODERO_REDIS_ADDR=localhost:6379",
		"GITHUB_TOKEN=ghp_xxxxx",
	}

	filtered := FilterEnvFrom(environ, LayerCodero)

	// Codero layer should receive everything unchanged
	if len(filtered) != len(environ) {
		t.Errorf("codero layer should receive all env vars, got %d want %d", len(filtered), len(environ))
	}
}

func TestIsForbiddenForAgent(t *testing.T) {
	cases := []struct {
		key      string
		expected bool
	}{
		{"CODERO_DB_PATH", true},
		{"CODERO_REDIS_PASS", true},
		{"GITHUB_TOKEN", true},
		{"GH_TOKEN", true},
		{"CODERO_WEBHOOK_SECRET", true},
		{"CODERO_AUTO_MERGE_ENABLED", true},
		{"LITELLM_API_KEY", false},
		{"CODERO_LITELLM_API_KEY", false},
		{"PATH", false},
		{"HOME", false},
		{"CODERO_SESSION_ID", false},
		{"CODERO_AGENT_ID", false},
		{"OPENCLAW_STATE_DIR", false},
	}

	for _, tc := range cases {
		got := IsForbiddenForAgent(tc.key)
		if got != tc.expected {
			t.Errorf("IsForbiddenForAgent(%q) = %v, want %v", tc.key, got, tc.expected)
		}
	}
}

func TestIsForbiddenForOpenClaw(t *testing.T) {
	cases := []struct {
		key      string
		expected bool
	}{
		{"CODERO_DB_PATH", true},
		{"CODERO_REDIS_ADDR", true},
		{"GITHUB_TOKEN", true},
		{"CODERO_HEARTBEAT_SECRET", true},
		{"CODERO_GITHUB_TOKEN", true},
		{"LITELLM_API_KEY", true},
		{"LITELLM_MASTER_KEY", true},
		{"CODERO_LITELLM_MASTER_KEY", true},
		{"CODERO_LITELLM_API_KEY", true},
		{"CODERO_AIDER_GEMINI_API_KEY", true},
		{"CODERO_GEMINI_SECOND_PASS_API_KEY", true},
		{"PATH", false},
		{"OPENCLAW_STATE_DIR", false},
		{"CODERO_SESSION_ID", false},
		// OpenClaw may receive these (they're forbidden for agent but not openclaw)
		{"CODERO_WEBHOOK_SECRET", false},
	}

	for _, tc := range cases {
		got := IsForbiddenForOpenClaw(tc.key)
		if got != tc.expected {
			t.Errorf("IsForbiddenForOpenClaw(%q) = %v, want %v", tc.key, got, tc.expected)
		}
	}
}

func TestFilterEnvStrict(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"HOME=<FAKE:user-home>",
		"TERM=xterm",
		"CODERO_SESSION_ID=sess-123",
		"CODERO_AGENT_ID=claude",
		"CODERO_DB_PATH=<FAKE:db-path>",
		"OPENCLAW_STATE_DIR=<FAKE:oc-state>",
	}

	filtered := FilterEnvStrict(environ)

	// Should only have allowed vars
	allowedKeys := []string{
		"PATH",
		"HOME",
		"TERM",
		"CODERO_SESSION_ID",
		"CODERO_AGENT_ID",
		"OPENCLAW_STATE_DIR",
	}

	for _, e := range filtered {
		key := envKey(e)
		if !slices.Contains(allowedKeys, key) {
			t.Errorf("strict filter should not include %s", key)
		}
	}

	// Should NOT have these
	notAllowed := []string{"RANDOM_VAR", "CODERO_DB_PATH"}
	for _, na := range notAllowed {
		for _, e := range filtered {
			if strings.HasPrefix(e, na+"=") {
				t.Errorf("strict filter should NOT include %s", na)
			}
		}
	}
}

func TestForbiddenLists(t *testing.T) {
	// Ensure lists are non-empty and don't modify originals
	agentList := ForbiddenForAgent()
	if len(agentList) == 0 {
		t.Error("ForbiddenForAgent should return non-empty list")
	}

	openclawList := ForbiddenForOpenClaw()
	if len(openclawList) == 0 {
		t.Error("ForbiddenForOpenClaw should return non-empty list")
	}

	// Verify defensive copy
	agentList[0] = "MODIFIED"
	if ForbiddenForAgent()[0] == "MODIFIED" {
		t.Error("ForbiddenForAgent should return a copy, not the original")
	}
}
