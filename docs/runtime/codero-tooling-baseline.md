# Codero Tooling Baseline

Status: shadow-mode / Codero-local  
Task: TOOL-001 (contained)  
Date: 2026-04-01

## Purpose

This document captures the shared tooling baseline that Codero depends on today.
It is a Codero-local documentation artifact created as part of TOOL-001 in
contained shadow mode.

**Shadow mode means:**

- This document describes what Codero currently depends on.
- It does not change any shared tooling behavior.
- It does not enforce anything globally.
- It does not claim that other repos have migrated to this baseline.
- It does not modify `/srv/storage/shared/tools/`, the host-level shared root,
  or any other
  repo.

The purpose is to make Codero's tooling dependencies explicit, validate that
they exist, and identify any drift or exceptions before any global enforcement
work begins.

## Current Shared Baseline

The following paths are the current source of truth for Codero shared tooling:

### Shared Env Bootstrap

| Path | Status | Mandatory |
|------|--------|-----------|
| `$CODERO_SHARED_ENV_BOOTSTRAP` | exists | yes |

This script sets shared PATH, cache locations, and env vars. Codero runtime
helpers and tests assume this environment when running from managed launches.

### Shared Tool Binaries

| Path | Status | Mandatory |
|------|--------|-----------|
| `/srv/storage/shared/tools/bin/` | exists | yes |

Key binaries Codero references:

- `/srv/storage/shared/tools/bin/agent-tmux-bridge` — exists, mandatory
- `/srv/storage/shared/agent-toolkit/bin/gate-heartbeat` — exists, mandatory
- `/srv/storage/shared/agent-toolkit/bin/codero-finish.sh` — exists, mandatory
- `/srv/storage/shared/agent-toolkit/bin/install-hooks` — exists, optional

### Shared Virtual Environments

| Path | Status | Mandatory |
|------|--------|-----------|
| `/srv/storage/shared/tools/venvs/` | exists | yes |

Available venvs:

- `aider` — exists
- `pr-agent` — exists
- `tooling` (ruff, poetry, semgrep, pre-commit) — exists
- `shared-lint` — exists

Codero does not directly invoke these venvs but expects shared tooling to use
them instead of per-agent installs.

### Shared Caches

| Path | Status | Mandatory |
|------|--------|-----------|
| `$CODERO_SHARED_PIP_CACHE` | exists | optional |
| `$CODERO_SHARED_GO_MOD_CACHE` | exists | yes (Codero is Go) |
| `$CODERO_SHARED_NPM_CACHE` | exists | optional |
| `$CODERO_SHARED_SEMGREP_CACHE` | exists | optional |

The Go module cache is mandatory because Codero is a Go project. Other caches
are optional but expected for consistency.

### Shared Browsers

| Path | Status | Mandatory |
|------|--------|-----------|
| `$CODERO_SHARED_PLAYWRIGHT_ROOT` | exists | optional |

Codero does not directly use Playwright, but the shared baseline includes it.

### Shared PTY Transport Helper

| Path | Status | Mandatory |
|------|--------|-----------|
| `/srv/storage/shared/tools/bin/agent-tmux-bridge` | exists | yes |

This is the approved PTY transport path for managed agent sessions. Codero's
OpenClaw integration depends on this helper for session delivery.

### Shared Memory

| Path | Status | Mandatory |
|------|--------|-----------|
| `/srv/storage/shared/memory/MEMORY.md` | exists | yes |
| `/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md` | exists | yes |

Agents and operators read shared memory at session start. Codero status and
runtime notes are maintained here.

## Codero-Local References

The following files in the Codero repo reference shared tooling paths:

| File | Reference | Purpose |
|------|-----------|---------|
| `tests/contract/finish_loop_contract_test.go` | `/srv/storage/shared/agent-toolkit/bin/codero-finish.sh` | contract test |
| `internal/gate/heartbeat.go` | `/srv/storage/shared/agent-toolkit/bin/gate-heartbeat` | default heartbeat bin |
| `cmd/codero/preflight_cmd.go` | `/srv/storage/shared/agent-toolkit/bin/gate-heartbeat` | preflight check |
| `cmd/codero/agent_launch_test.go` | `/srv/storage/shared/agent-toolkit/bin/codero-agent` | test helper |
| `AGENTS.md` | multiple toolkit refs | agent policy |
| `docs/evidence/*` | toolkit refs | evidence artifacts |
| `docs/runbooks/*` | toolkit refs | operational runbooks |
| `docs/roadmaps/*` | toolkit refs | roadmap docs |

These references are expected. They represent Codero's intentional dependency on
the shared toolkit.

## Current OpenClaw Usage Shape

The working OpenClaw configuration for Codero smoke tests is at:

`$OPENCLAW_CONFIG_PATH`  
default example: `$HOME/.openclaw-codero-smoke/openclaw.json`

Key shape:

- Gateway: loopback-only (`127.0.0.1:18789`)
- Gateway auth: token-based
- Workspace: isolated under OpenClaw state root
- Tool profile: `coding`
- Provider: LiteLLM (`http://localhost:4000`)
- Plugins enabled: `litellm`, `acpx`

This matches the intended configuration shape from the shared tooling policy.

## Current Approved Shared Bridge Path

The approved PTY transport path is:

```
/srv/storage/shared/tools/bin/agent-tmux-bridge
```

This helper supports `start-profile`, `wait-ready`, `deliver`, `capture`,
`status`, and `stop` commands for managed agent families (codex, claude, gemini,
opencode, copilot).

## Current Shared Env/Bootstrap Assumptions

When Codero runs in a managed context, it expects:

1. `agent-env.sh` has been sourced
2. PATH includes the shared tools bin under `/srv/storage/shared/tools/bin`
3. Go module cache resolves through `$CODERO_SHARED_GO_MOD_CACHE`
4. Shared toolkit binaries are executable
5. Shared PTY bridge is available

## Known Drift or Exceptions

### Drift from Policy Doc

None observed. Current shared paths match the policy baseline at
`/srv/storage/local/codero/specication_033126/codero-shared-tooling-and-openclaw-policy.md`.

### Codero-Specific Exceptions

| Exception | Status | Reason |
|-----------|--------|--------|
| Codero venv at `/srv/storage/shared/tools/venvs/codero` | referenced in AGENTS.md, not present | Codero is Go, not Python |

The AGENTS.md references a Codero venv that does not exist. This is not a
functional issue because Codero is a Go project and does not need a Python venv.
The reference should be removed or clarified in a future cleanup.

### Missing Paths

All documented shared baseline paths exist in the current Codero environment.

## What Is Intentionally Not Enforced Yet

This document does not:

- Enforce privilege restrictions on OpenClaw
- Enforce plugin allowlist policy
- Enforce update cadence
- Modify shared wrappers or PATH behavior
- Change any global tooling defaults
- Claim that other repos have adopted this baseline

These are in scope for TOOL-002 through TOOL-005.

## Validation Section

To validate the Codero tooling baseline:

```bash
# 1. Verify shared env bootstrap exists
test -f "$CODERO_SHARED_ENV_BOOTSTRAP" && echo "PASS: agent-env.sh exists"

# 2. Verify shared tool bin directory
test -d /srv/storage/shared/tools/bin/ && echo "PASS: shared tools bin exists"

# 3. Verify PTY bridge exists and is executable
test -x /srv/storage/shared/tools/bin/agent-tmux-bridge && echo "PASS: PTY bridge OK"

# 4. Verify gate-heartbeat exists
test -x /srv/storage/shared/agent-toolkit/bin/gate-heartbeat && echo "PASS: gate-heartbeat OK"

# 5. Verify codero-finish.sh exists
test -x /srv/storage/shared/agent-toolkit/bin/codero-finish.sh && echo "PASS: codero-finish OK"

# 6. Verify Go module cache
test -d "$CODERO_SHARED_GO_MOD_CACHE" && echo "PASS: Go mod cache exists"

# 7. Verify shared memory
test -f /srv/storage/shared/memory/MEMORY.md && echo "PASS: shared memory exists"
```

All validation checks are read-only. They do not modify any state.

## Related Documents

- Policy: `/srv/storage/local/codero/specication_033126/codero-shared-tooling-and-openclaw-policy.md`
- Roadmap: `/srv/storage/local/codero/specication_033126/codero-agent-task-execution-roadmap.md`
- Shared memory: `/srv/storage/shared/memory/MEMORY.md`
- OpenClaw PTY notes: `/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md`
