# Environment Ownership by Layer

**Version:** 1.1  
**Task:** BND-002  
**Last Updated:** 2026-04-01  
**Status:** enforced

## Purpose

This document defines environment variable ownership by execution layer. It
specifies which env vars each layer receives, which are forbidden, and how
filtering is implemented.

**Authority reference:** `docs/contracts/actor-boundaries.md`

## Env Groups

The following env groups are defined in the canonical spec:

| Group | Owner | Purpose |
|-------|-------|---------|
| `E-AGENT` | Agent / Launcher | session identity, worktree path, mode |
| `E-BOOTSTRAP` | Codero / Launcher | runtime root, daemon address, pilot config |
| `E-TRACKING` | Codero | tracking configuration |
| `E-OPENCLAW` | OpenClaw | state directory, config path |
| `E-CODERO` | Codero | database, Redis, API address, logging |
| `E-WEBHOOK` | Codero | webhook secret |
| `E-DASH` | Codero | dashboard port, base path |
| `E-GITHUB` | Codero | GitHub token, merge settings |

## Layer Definitions

### LayerAgent

Agent processes running inside managed sessions. These execute user code and
have file system access to the assigned worktree.

**Filtering approach:** **Strict allowlist.** Only explicit agent-safe vars pass
through. This prevents any Codero-owned control-plane vars from leaking into the
managed agent boundary. A helper for the agent allowlist is reused by wrapper-env
filtering and tests.

**Receives:**
- System vars: `PATH`, `HOME`, `USER`, `SHELL`, `TERM`, `LANG`, `LC_*`, `TMPDIR`, `XDG_*`, etc.
- Agent vars (explicit allowlist):
  - `CODERO_SESSION_ID` — session identity
  - `CODERO_AGENT_ID` — agent identity
  - `CODERO_DAEMON_ADDR` — daemon address for callbacks
  - `CODERO_WORKTREE` — assigned worktree path
  - `CODERO_SESSION_MODE` — session mode
  - `CODERO_STARTED_AT` — session start time
  - `CODERO_TMUX_NAME` — tmux session name
  - `CODERO_RUNTIME_SESSION_MD` — session metadata path
  - `CODERO_AGENT_WRITE_SESSION_LOG` — session log flag
- OpenClaw vars: `OPENCLAW_*` (for adapter integration)
- LiteLLM vars: `LITELLM_API_KEY`, `LITELLM_URL`, `LITELLM_BASE_URL`, `LITELLM_PROXY_URL`, `CODERO_LITELLM_API_KEY`

**Does NOT receive (explicitly forbidden + any unlisted CODERO_*):**
- `CODERO_DB_PATH` — SQLite database path
- `CODERO_REDIS_*` — all Redis config
- `CODERO_API_*` — internal API config
- `CODERO_WEBHOOK_*` — webhook config
- `GITHUB_TOKEN`, `GH_TOKEN` — GitHub tokens
- `CODERO_LITELLM_MASTER_KEY` — control-plane LiteLLM master key
- `CODERO_AUTO_MERGE_*`, `CODERO_MERGE_METHOD`, `CODERO_PR_AUTO_CREATE` — merge policy
- `CODERO_CODERABBIT_AUTO_REVIEW` — review automation
- `CODERO_LOG_PATH`, `CODERO_READY_FILE`, `CODERO_PID_FILE` — daemon lifecycle
- `CODERO_REPOS` — managed repo list
- `CODERO_OBSERVABILITY_*` — observability config
- `CODERO_DASHBOARD_*` — dashboard config
- `CODERO_CONFIG_PATH` — config file path

### LayerOpenClaw

OpenClaw adapter processes providing PTY transport and session management.

**Filtering approach:** **Strict allowlist.** Only explicit adapter-safe vars pass
through. This prevents Codero-owned control-plane vars from leaking into the
adapter boundary.

**Receives:**
- System vars: `PATH`, `HOME`, etc.
- Agent vars: `CODERO_SESSION_ID`, `CODERO_AGENT_ID`, `CODERO_DAEMON_ADDR`
- OpenClaw vars: `OPENCLAW_STATE_DIR`, `OPENCLAW_CONFIG_PATH`
- Webhook vars: `CODERO_WEBHOOK_*` (for receiving callbacks)

**Does NOT receive (forbidden):**
- `CODERO_DB_PATH` — SQLite database path
- `CODERO_REDIS_*` — all Redis config
- `GITHUB_TOKEN`, `GH_TOKEN` — GitHub tokens
- `LITELLM_API_KEY`, `CODERO_LITELLM_API_KEY`, `CODERO_LITELLM_MASTER_KEY` — LiteLLM secrets
- `CODERO_LOG_PATH`, `CODERO_READY_FILE`, `CODERO_PID_FILE` — daemon lifecycle
- `CODERO_REPOS` — managed repo list
- `CODERO_CONFIG_PATH` — config file path

### LayerCodero

Codero control plane processes (daemon, CLI in control-plane mode).

**Receives:** All env vars. No filtering applied.

## Implementation

Env filtering is implemented in `internal/session/env_filter.go`.

### Filter Functions

```go
// FilterEnv filters os.Environ() for the specified layer.
func FilterEnv(layer Layer) []string

// FilterEnvFrom filters the given env slice for the specified layer.
func FilterEnvFrom(environ []string, layer Layer) []string

// FilterWrapperEnvVars filters config-loaded env vars before adding to agent env.
// Prevents config from re-introducing forbidden vars.
func FilterWrapperEnvVars(vars map[string]string, layer Layer) map[string]string

// BuildAgentEnv builds a minimal env slice for agent processes.
// Use for tmux sessions to avoid inheriting parent env.
func BuildAgentEnv(sessionID, agentID, daemonAddr string) []string

// IsForbiddenForAgent returns true if key should not reach agent.
func IsForbiddenForAgent(key string) bool

// IsForbiddenForOpenClaw returns true if key should not reach OpenClaw.
func IsForbiddenForOpenClaw(key string) bool
```

### Usage in Launch Code

`cmd/codero/agent_run.go` uses the filter when spawning agent processes and re-applies the resolved agent identity in degraded launches:

```go
// BND-002: Filter env using strict allowlist.
env := session.FilterEnv(session.LayerAgent)

// BND-002: Filter wrapper env vars to prevent config from re-introducing forbidden vars
if uc, err := config.LoadUserConfig(); err == nil && uc != nil {
    if w, ok := uc.Wrappers[agentID]; ok && w.EnvVars != nil {
        filtered := session.FilterWrapperEnvVars(w.EnvVars, session.LayerAgent)
        for k, v := range filtered {
            env = append(env, k+"="+v)
        }
    }
}
child.Env = append(env,
    "CODERO_SESSION_ID="+sessionID,
    "CODERO_AGENT_ID="+agentID,
    "CODERO_DAEMON_ADDR="+daemonAddr,
)
```

`cmd/codero/agent_run.go` also filters the degraded/fallback path and keeps the resolved agent ID:

```go
// execBinary: BND-002: Filter env even in degraded path.
// buildFallbackEnv preserves the resolved agent label after filtering.
env := buildFallbackEnv(agentID)
syscall.Exec(binaryPath, argv, env)
```

`cmd/codero/agent_launch.go` uses `tmux respawn-window` with `env -i` to clear inherited env:

```go
// BND-002: Use env -i to clear inherited env, then pass only agent-safe vars.
respawnArgs := []string{
    "env", "-i",
    "CODERO_SESSION_ID="+sessionID,
    "CODERO_AGENT_ID="+cfg.AgentID,
    "CODERO_DAEMON_ADDR="+cfg.DaemonAddr,
    // ... other managed-launch vars ...
    "/bin/bash", "-lc", "exec " + agentCmd,
}
```

## Validation

### Certification Tests

Location: `cmd/codero/env_filter_bnd002_test.go`

| Test | Purpose |
|------|---------|
| `TestBND002_EnvFiltering_AgentDoesNotReceiveForbiddenVars` | Agent layer forbidden vars |
| `TestBND002_EnvFiltering_OpenClawDoesNotReceiveForbiddenVars` | OpenClaw layer forbidden vars |
| `TestBND002_EnvFiltering_CoderoReceivesAll` | Codero layer receives all |
| `TestBND002_EnvFiltering_RealEnvironment` | Filter against real os.Environ() |
| `TestBND002_ForbiddenLists_Completeness` | All sensitive vars in lists |
| `TestBND002_DefaultBehavior_UnsetVar` | Unset var handling |
| `TestBND002_Precedence_ExplicitOverImplicit` | Explicit additions after filter |

Location: `internal/session/env_filter_test.go`

| Test | Purpose |
|------|---------|
| `TestFilterEnvFrom_Agent` | Agent filter allowlist |
| `TestFilterEnvFrom_OpenClaw` | OpenClaw filter denylist |
| `TestFilterEnvFrom_Codero` | Codero receives all |
| `TestIsForbiddenForAgent` | Individual key checks |
| `TestIsForbiddenForOpenClaw` | Individual key checks |
| `TestFilterEnvStrict` | Strict allowlist mode |
| `TestForbiddenLists` | List integrity |

### Run Validation

```bash
# Run all BND-002 certification tests
go test ./cmd/codero/... ./internal/session/... -v -run 'BND002|Filter|Forbidden'
```

### Expected Results

All tests must pass. Forbidden vars must be absent from filtered output.

## Default/On/Off/Invalid Behavior

| Scenario | Behavior |
|----------|----------|
| Var unset | Not in filtered output (correct) |
| Var set normally | Filtered per layer rules |
| Var set to empty string | Filtered per layer rules (key still checked) |
| Invalid var format | Passed through if not forbidden (edge case) |
| New CODERO_* var added | **Blocked by default** for agent (allowlist) |

## Precedence

When the same var appears multiple times (e.g., from filter + explicit append),
Go's `exec.Cmd` uses the **last occurrence**. This allows explicit overrides:

```go
env := session.FilterEnv(session.LayerAgent)          // may have CODERO_SESSION_ID
env = append(env, "CODERO_SESSION_ID="+newSessionID)  // override wins
```

## Dashboard-Visible Proof

Env ownership is not directly visible in the dashboard, but its effects are:

- Sessions cannot access DB/Redis directly (errors if they try)
- GitHub operations only happen through Codero API
- Agent processes cannot merge or mutate PRs

## References

- Actor boundaries: `docs/contracts/actor-boundaries.md`
- Privilege profile: `docs/runtime/openclaw-privilege-profile.md`
- Canonical spec: `/srv/storage/local/codero/specication_033126/codero-agent-task-execution-spec.md`
- BND-002 task: Enforce environment ownership by layer

## Change Log

| Date | Version | Change |
|------|---------|--------|
| 2026-04-01 | 1.0 | Initial enforcement (BND-002) |
| 2026-04-01 | 1.1 | Fix tmux inheritance, execBinary fallback, wrapper reintroduction, expand denylist |
