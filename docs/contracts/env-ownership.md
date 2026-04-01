# Environment Ownership by Layer

**Version:** 1.0  
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

**Receives:**
- System vars: `PATH`, `HOME`, `USER`, `SHELL`, `TERM`, `LANG`, `LC_*`, `TMPDIR`, etc.
- Agent vars: `CODERO_SESSION_ID`, `CODERO_AGENT_ID`, `CODERO_DAEMON_ADDR`, `CODERO_WORKTREE`
- OpenClaw vars: `OPENCLAW_*` (for adapter integration)

**Does NOT receive (forbidden):**
- `CODERO_DB_PATH` — SQLite database path
- `CODERO_REDIS_ADDR` — Redis connection address
- `CODERO_REDIS_PASS` — Redis password
- `CODERO_REDIS_*` — all Redis config
- `CODERO_API_ADDR` — internal API address
- `CODERO_WEBHOOK_SECRET` — webhook signing secret
- `GITHUB_TOKEN` — GitHub API token
- `GH_TOKEN` — GitHub CLI token
- `CODERO_AUTO_MERGE_*` — merge policy
- `CODERO_MERGE_METHOD` — merge method
- `CODERO_PR_AUTO_CREATE` — PR automation
- `CODERO_CODERABBIT_AUTO_REVIEW` — review automation

### LayerOpenClaw

OpenClaw adapter processes providing PTY transport and session management.

**Receives:**
- System vars: `PATH`, `HOME`, etc.
- Agent vars: `CODERO_SESSION_ID`, `CODERO_AGENT_ID`, `CODERO_DAEMON_ADDR`
- OpenClaw vars: `OPENCLAW_STATE_DIR`, `OPENCLAW_CONFIG_PATH`
- Webhook vars: `CODERO_WEBHOOK_*` (for receiving callbacks)

**Does NOT receive (forbidden):**
- `CODERO_DB_PATH` — SQLite database path
- `CODERO_REDIS_ADDR` — Redis connection address  
- `CODERO_REDIS_PASS` — Redis password
- `CODERO_REDIS_*` — all Redis config
- `GITHUB_TOKEN` — GitHub API token
- `GH_TOKEN` — GitHub CLI token

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

// IsForbiddenForAgent returns true if key should not reach agent.
func IsForbiddenForAgent(key string) bool

// IsForbiddenForOpenClaw returns true if key should not reach OpenClaw.
func IsForbiddenForOpenClaw(key string) bool
```

### Usage in Launch Code

`cmd/codero/agent_run.go` uses the filter when spawning agent processes:

```go
// BND-002: Filter env to prevent control-plane secrets from leaking to agent.
env := session.FilterEnv(session.LayerAgent)
child.Env = append(env,
    "CODERO_SESSION_ID="+sessionID,
    "CODERO_AGENT_ID="+agentID,
    "CODERO_DAEMON_ADDR="+daemonAddr,
)
```

`cmd/codero/agent_launch.go` exports only agent-safe vars via tmux:

```go
// BND-002: Only export agent-safe vars. The tmux session starts with a clean
// shell that does not inherit parent env.
envExport := fmt.Sprintf("export CODERO_SESSION_ID='%s' CODERO_AGENT_ID='%s'", ...)
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
| `TestFilterEnvFrom_Agent` | Agent filter denylist |
| `TestFilterEnvFrom_OpenClaw` | OpenClaw filter denylist |
| `TestFilterEnvFrom_Codero` | Codero receives all |
| `TestIsForbiddenForAgent` | Individual key checks |
| `TestIsForbiddenForOpenClaw` | Individual key checks |
| `TestFilterEnvStrict` | Allowlist mode |
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
