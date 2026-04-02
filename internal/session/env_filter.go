// Package session provides env filtering for BND-002.
// EnvFilter implements environment variable ownership by layer.
package session

import (
	"os"
	"strconv"
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
// These contain Codero internal secrets, GitHub credentials, or control-plane config.
// The agent path is enforced with a strict allowlist, but this list remains the
// canonical reference for sensitive vars and tests.
var forbiddenForAgent = []string{
	// Persistence
	"CODERO_DB_PATH",
	"CODERO_REDIS_ADDR",
	"CODERO_REDIS_PASS",
	"CODERO_REDIS_MAX_RETRIES",
	"CODERO_REDIS_RETRY_INTERVAL",
	"CODERO_REDIS_HEALTH_INTERVAL",
	// API/webhook
	"CODERO_API_ADDR",
	"CODERO_API_READ_TIMEOUT",
	"CODERO_API_WRITE_TIMEOUT",
	"CODERO_API_SHUTDOWN_TIMEOUT",
	"CODERO_WEBHOOK_SECRET",
	"CODERO_WEBHOOK_ENABLED",
	// GitHub auth
	"GITHUB_TOKEN",
	"GH_TOKEN",
	// Merge policy
	"CODERO_AUTO_MERGE_ENABLED",
	"CODERO_AUTO_MERGE_METHOD",
	"CODERO_MERGE_METHOD",
	"CODERO_PR_AUTO_CREATE",
	"CODERO_CODERABBIT_AUTO_REVIEW",
	// Agent control and review secrets
	"CODERO_HEARTBEAT_SECRET",
	"CODERO_GITHUB_TOKEN",
	"CODERO_LITELLM_MASTER_KEY",
	"CODERO_AIDER_GEMINI_API_KEY",
	"CODERO_GEMINI_SECOND_PASS_API_KEY",
	// Control-plane config
	"CODERO_LOG_PATH",
	"CODERO_READY_FILE",
	"CODERO_PID_FILE",
	"CODERO_REPOS",
	"CODERO_OBSERVABILITY_HOST",
	"CODERO_OBSERVABILITY_PORT",
	"CODERO_DASHBOARD_BASE_PATH",
	"CODERO_DASHBOARD_PUBLIC_BASE_URL",
	"CODERO_DASHBOARD_URL",
	"CODERO_CONFIG_PATH",
}

// forbiddenForOpenClaw are env var names that must NOT reach OpenClaw/adapter processes.
// OpenClaw must not have direct access to Codero's persistence layer, control-plane
// lifecycle/config, or GitHub auth.
var forbiddenForOpenClaw = []string{
	// Persistence
	"CODERO_DB_PATH",
	"CODERO_REDIS_ADDR",
	"CODERO_REDIS_PASS",
	"CODERO_REDIS_MAX_RETRIES",
	"CODERO_REDIS_RETRY_INTERVAL",
	"CODERO_REDIS_HEALTH_INTERVAL",
	// GitHub auth
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"CODERO_GITHUB_TOKEN",
	"CODERO_HEARTBEAT_SECRET",
	// Model / review secrets
	"LITELLM_MASTER_KEY",
	"LITELLM_API_KEY",
	"OPENAI_API_KEY",
	"GEMINI_API_KEY",
	"MINIMAX_API_KEY",
	"CODERO_LITELLM_MASTER_KEY",
	"CODERO_LITELLM_API_KEY",
	"CODERO_AIDER_GEMINI_API_KEY",
	"CODERO_GEMINI_SECOND_PASS_API_KEY",
	// Control-plane config (OpenClaw doesn't need these)
	"CODERO_LOG_PATH",
	"CODERO_READY_FILE",
	"CODERO_PID_FILE",
	"CODERO_REPOS",
	"CODERO_OBSERVABILITY_HOST",
	"CODERO_OBSERVABILITY_PORT",
	"CODERO_DASHBOARD_BASE_PATH",
	"CODERO_DASHBOARD_PUBLIC_BASE_URL",
	"CODERO_DASHBOARD_URL",
	"CODERO_CONFIG_PATH",
}

// allowedForOpenClaw are env vars explicitly allowed for OpenClaw/adapter processes.
// These are the E-OPENCLAW vars plus the minimal session identity needed to
// register, heartbeat, and route feedback.
var allowedForOpenClaw = []string{
	"CODERO_SESSION_ID",
	"CODERO_AGENT_ID",
	"CODERO_DAEMON_ADDR",
	"CODERO_WEBHOOK_SECRET",
	"CODERO_WEBHOOK_ENABLED",
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
	// Allow agent-side model routing via LiteLLM.
	"LITELLM_BASE_URL",
	"LITELLM_URL",
	"LITELLM_PROXY_URL",
	"LITELLM_API_KEY",
	"CODERO_LITELLM_API_KEY",
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

var managedSessionKeys = map[string]struct{}{
	"CODERO_SESSION_ID":              {},
	"CODERO_AGENT_ID":                {},
	"CODERO_DAEMON_ADDR":             {},
	"CODERO_WORKTREE":                {},
	"CODERO_SESSION_MODE":            {},
	"CODERO_RUNTIME_SESSION_MD":      {},
	"CODERO_TMUX_NAME":               {},
	"CODERO_STARTED_AT":              {},
	"CODERO_AGENT_WRITE_SESSION_LOG": {},
}

// FilterEnv filters the current environment for the specified layer.
// Returns a new env slice with forbidden vars removed.
func FilterEnv(layer Layer) []string {
	return FilterEnvFrom(os.Environ(), layer)
}

// FilterEnvFrom filters the given environment for the specified layer.
// For LayerAgent, uses a strict allowlist over the managed agent boundary.
// For LayerOpenClaw, uses denylist approach.
func FilterEnvFrom(environ []string, layer Layer) []string {
	switch layer {
	case LayerAgent:
		// Agent uses the strict allowlist helper.
		return filterAgentStrict(environ)
	case LayerOpenClaw:
		// OpenClaw uses a strict allowlist to avoid inheriting Codero secrets.
		return filterOpenClawStrict(environ)
	case LayerCodero:
		// Codero layer receives everything
		return environ
	}
	return environ
}

// filterAgentStrict uses allowlist approach for agent layer.
// Only systemPassthrough + allowedForAgent + OPENCLAW_* pass through.
// All other vars are blocked by default.
func filterAgentStrict(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, e := range environ {
		key := envKey(e)
		if isAllowedForAgent(key) {
			result = append(result, e)
		}
	}
	return result
}

// filterDenylist filters using a denylist (block specific vars).
func filterDenylist(environ []string, forbidden []string) []string {
	result := make([]string, 0, len(environ))
	for _, e := range environ {
		key := envKey(e)
		if !isForbidden(key, forbidden) {
			result = append(result, e)
		}
	}
	return result
}

// filterOpenClawStrict uses allowlist approach for adapter layer.
// Only systemPassthrough + allowedForOpenClaw + OPENCLAW_* pass through.
// Everything else is blocked by default.
func filterOpenClawStrict(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, e := range environ {
		key := envKey(e)
		if isAllowedForOpenClaw(key) {
			result = append(result, e)
		}
	}
	return result
}

// FilterEnvStrict filters env for agent layer using a strict allowlist.
// It is kept as a direct helper for tests and future hardening work.
func FilterEnvStrict(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, e := range environ {
		key := envKey(e)
		if isAllowedForAgent(key) {
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

// isAllowedForAgent checks if a key is allowed for strict agent allowlist mode.
// Uses strict allowlist: system vars + agent-specific CODERO_ vars + OPENCLAW_.
// Everything else is denied by default.
func isAllowedForAgent(key string) bool {
	// Explicitly block GitHub tokens (not CODERO_ prefixed but still sensitive)
	if key == "GITHUB_TOKEN" || key == "GH_TOKEN" || key == "CODERO_GITHUB_TOKEN" {
		return false
	}
	// System passthrough
	for _, a := range systemPassthrough {
		if key == a {
			return true
		}
	}
	// Agent-allowed CODERO_ vars
	for _, a := range allowedForAgent {
		if key == a {
			return true
		}
	}
	// OPENCLAW_ vars for adapter integration
	if strings.HasPrefix(key, "OPENCLAW_") {
		return true
	}
	return false
}

// isAllowedForOpenClaw checks if a key is allowed for strict OpenClaw allowlist mode.
// Uses strict allowlist: system vars + minimal session identity + webhook vars + OPENCLAW_.
// Everything else is denied by default.
func isAllowedForOpenClaw(key string) bool {
	for _, a := range systemPassthrough {
		if key == a {
			return true
		}
	}
	for _, a := range allowedForOpenClaw {
		if key == a {
			return true
		}
	}
	if strings.HasPrefix(key, "OPENCLAW_") {
		return true
	}
	return false
}

// isAllowed checks if a key is in the allowed lists for agent.
// Deprecated: use isAllowedForAgent instead.
func isAllowed(key string) bool {
	return isAllowedForAgent(key)
}

// IsForbiddenForAgent returns true if the key should not reach agent processes.
func IsForbiddenForAgent(key string) bool {
	return !isAllowedForAgent(key)
}

// IsForbiddenForOpenClaw returns true if the key should not reach OpenClaw.
func IsForbiddenForOpenClaw(key string) bool {
	return !isAllowedForOpenClaw(key)
}

// ForbiddenForAgent returns the list of forbidden env var names for agent layer.
func ForbiddenForAgent() []string {
	return append([]string{}, forbiddenForAgent...)
}

// ForbiddenForOpenClaw returns the list of forbidden env var names for OpenClaw.
func ForbiddenForOpenClaw() []string {
	return append([]string{}, forbiddenForOpenClaw...)
}

// FilterWrapperEnvVars filters config-loaded env vars before adding to agent env.
// This prevents config files from re-introducing forbidden vars.
// Returns a filtered copy of the input map.
func FilterWrapperEnvVars(vars map[string]string, layer Layer) map[string]string {
	if vars == nil {
		return nil
	}
	result := make(map[string]string, len(vars))
	for k, v := range vars {
		switch layer {
		case LayerAgent:
			if isAllowedForAgent(k) {
				result[k] = v
			}
		case LayerOpenClaw:
			if isAllowedForOpenClaw(k) {
				result[k] = v
			}
		case LayerCodero:
			result[k] = v
		}
	}
	return result
}

// BuildAgentEnv builds the env slice for the managed agent launch path.
// It starts from the current process env filtered for the agent layer, then
// appends the explicit launch contract values so they override inherited copies.
func BuildAgentEnv(sessionID, agentID, daemonAddr, worktree, sessionMode, runtimeSessionMD, tmuxName, startedAt string, writeLog bool) []string {
	env := removeEnvKeys(FilterEnv(LayerAgent), managedSessionKeys)

	if sessionID != "" {
		env = append(env, "CODERO_SESSION_ID="+sessionID)
	}
	if agentID != "" {
		env = append(env, "CODERO_AGENT_ID="+agentID)
	}
	if daemonAddr != "" {
		env = append(env, "CODERO_DAEMON_ADDR="+daemonAddr)
	}
	if worktree != "" {
		env = append(env, "CODERO_WORKTREE="+worktree)
	}
	if sessionMode != "" {
		env = append(env, "CODERO_SESSION_MODE="+sessionMode)
	}
	if runtimeSessionMD != "" {
		env = append(env, "CODERO_RUNTIME_SESSION_MD="+runtimeSessionMD)
	}
	if tmuxName != "" {
		env = append(env, "CODERO_TMUX_NAME="+tmuxName)
	}
	if startedAt != "" {
		env = append(env, "CODERO_STARTED_AT="+startedAt)
	}
	env = append(env, "CODERO_AGENT_WRITE_SESSION_LOG="+strconv.FormatBool(writeLog))

	return env
}

func removeEnvKeys(environ []string, keys map[string]struct{}) []string {
	if len(environ) == 0 {
		return []string{}
	}

	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		if _, ok := keys[envKey(entry)]; ok {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
