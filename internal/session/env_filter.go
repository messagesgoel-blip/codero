// Package session provides env filtering for BND-002.
// EnvFilter implements environment variable ownership by layer.
package session

import (
	"os"
	"strings"
)

// Layer represents an execution context with specific env requirements.
type Layer int

const (
	// LayerAgent receives only agent-safe env vars.
	// Must NOT receive: CODERO_DB_*, CODERO_REDIS_*, GITHUB_TOKEN, CODERO_WEBHOOK_*
	LayerAgent Layer = iota

	// LayerOpenClaw receives adapter-safe env vars.
	// Must NOT receive: CODERO_DB_*, CODERO_REDIS_*, GITHUB_TOKEN
	LayerOpenClaw

	// LayerCodero receives all Codero env vars (control plane).
	LayerCodero
)

// forbiddenForAgent are env var prefixes/names that must NOT reach agent processes.
// These contain Codero internal secrets or GitHub credentials.
var forbiddenForAgent = []string{
	"CODERO_DB_PATH",
	"CODERO_REDIS_ADDR",
	"CODERO_REDIS_PASS",
	"CODERO_REDIS_MAX_RETRIES",
	"CODERO_REDIS_RETRY_INTERVAL",
	"CODERO_REDIS_HEALTH_INTERVAL",
	"CODERO_API_ADDR",
	"CODERO_API_READ_TIMEOUT",
	"CODERO_API_WRITE_TIMEOUT",
	"CODERO_API_SHUTDOWN_TIMEOUT",
	"CODERO_WEBHOOK_SECRET",
	"CODERO_WEBHOOK_ENABLED",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"CODERO_AUTO_MERGE_ENABLED",
	"CODERO_AUTO_MERGE_METHOD",
	"CODERO_MERGE_METHOD",
	"CODERO_PR_AUTO_CREATE",
	"CODERO_CODERABBIT_AUTO_REVIEW",
}

// forbiddenForOpenClaw are env var names that must NOT reach OpenClaw/adapter processes.
// OpenClaw must not have direct access to Codero's persistence layer.
var forbiddenForOpenClaw = []string{
	"CODERO_DB_PATH",
	"CODERO_REDIS_ADDR",
	"CODERO_REDIS_PASS",
	"CODERO_REDIS_MAX_RETRIES",
	"CODERO_REDIS_RETRY_INTERVAL",
	"CODERO_REDIS_HEALTH_INTERVAL",
	"GITHUB_TOKEN",
	"GH_TOKEN",
}

// allowedForAgent are env vars explicitly allowed for agent processes.
// These are the E-AGENT group vars.
var allowedForAgent = []string{
	"CODERO_SESSION_ID",
	"CODERO_AGENT_ID",
	"CODERO_WORKTREE",
	"CODERO_RUNTIME_SESSION_MD",
	"CODERO_SESSION_MODE",
	"CODERO_DAEMON_ADDR",
	"CODERO_STARTED_AT",
	"CODERO_TMUX_NAME",
	"CODERO_AGENT_WRITE_SESSION_LOG",
}

// systemPassthrough are env vars that should pass through to all layers.
// These are standard system vars needed for process execution.
var systemPassthrough = []string{
	"PATH",
	"HOME",
	"USER",
	"SHELL",
	"TERM",
	"LANG",
	"LC_ALL",
	"LC_CTYPE",
	"TMPDIR",
	"TMP",
	"TEMP",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_CACHE_HOME",
	"XDG_RUNTIME_DIR",
	"SSH_AUTH_SOCK",
	"DISPLAY",
	"COLORTERM",
	"EDITOR",
	"VISUAL",
	"PAGER",
}

// FilterEnv filters the current environment for the specified layer.
// Returns a new env slice with forbidden vars removed.
func FilterEnv(layer Layer) []string {
	return FilterEnvFrom(os.Environ(), layer)
}

// FilterEnvFrom filters the given environment for the specified layer.
func FilterEnvFrom(environ []string, layer Layer) []string {
	var forbidden []string
	switch layer {
	case LayerAgent:
		forbidden = forbiddenForAgent
	case LayerOpenClaw:
		forbidden = forbiddenForOpenClaw
	case LayerCodero:
		// Codero layer receives everything
		return environ
	}

	result := make([]string, 0, len(environ))
	for _, e := range environ {
		key := envKey(e)
		if !isForbidden(key, forbidden) {
			result = append(result, e)
		}
	}
	return result
}

// FilterEnvStrict filters env for agent layer using allowlist approach.
// Only allows explicit passthrough vars + allowed agent vars.
// This is stricter than FilterEnv which uses a denylist.
func FilterEnvStrict(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, e := range environ {
		key := envKey(e)
		if isAllowed(key) {
			result = append(result, e)
		}
	}
	return result
}

// envKey extracts the key from a "KEY=value" string.
func envKey(e string) string {
	if idx := strings.Index(e, "="); idx >= 0 {
		return e[:idx]
	}
	return e
}

// isForbidden checks if a key matches any forbidden pattern.
func isForbidden(key string, forbidden []string) bool {
	for _, f := range forbidden {
		if key == f {
			return true
		}
	}
	return false
}

// isAllowed checks if a key is in the allowed lists for agent.
func isAllowed(key string) bool {
	for _, a := range systemPassthrough {
		if key == a {
			return true
		}
	}
	for _, a := range allowedForAgent {
		if key == a {
			return true
		}
	}
	// Allow OPENCLAW_ vars for adapter integration
	if strings.HasPrefix(key, "OPENCLAW_") {
		return true
	}
	return false
}

// IsForbiddenForAgent returns true if the key should not reach agent processes.
func IsForbiddenForAgent(key string) bool {
	return isForbidden(key, forbiddenForAgent)
}

// IsForbiddenForOpenClaw returns true if the key should not reach OpenClaw.
func IsForbiddenForOpenClaw(key string) bool {
	return isForbidden(key, forbiddenForOpenClaw)
}

// ForbiddenForAgent returns the list of forbidden env var names for agent layer.
func ForbiddenForAgent() []string {
	return append([]string{}, forbiddenForAgent...)
}

// ForbiddenForOpenClaw returns the list of forbidden env var names for OpenClaw.
func ForbiddenForOpenClaw() []string {
	return append([]string{}, forbiddenForOpenClaw...)
}
