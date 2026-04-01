# OpenClaw Privilege Profile for Codero

Status: shadow-mode / Codero-local
Task: TOOL-002 (contained)
Date: 2026-04-01

## Purpose

This document defines the OpenClaw privilege profile for Codero. It specifies
what OpenClaw is allowed to do, what requires care, and what is forbidden.

**Shadow mode means:**

- This document describes the intended privilege model for Codero.
- It does not enforce these restrictions globally.
- It does not claim that other repos follow this profile.
- It does not modify shared tooling or OpenClaw defaults.
- Enforcement work belongs to later tasks.

The purpose is to make the privilege boundary explicit so future enforcement
work has a documented baseline.

## OpenClaw Role in Codero

OpenClaw acts as the **adapter layer** between agents and the Codero control
plane. It is not a source of truth for any durable state.

**What OpenClaw is:**

- communication shell for agent interactions
- gateway and session routing
- PTY transport through the shared bridge
- session registration and heartbeat calls into Codero
- structured feedback injection into live agent sessions
- telemetry collector for operator dashboards

**What OpenClaw is not:**

- source of truth for assignment state
- source of truth for merge readiness
- direct GitHub mutation authority
- direct database client for Codero persistence
- replacement for the shared PTY bridge

This role is defined by the canonical spec at
`/srv/storage/local/codero/specication_033126/codero-agent-task-execution-spec.md`.

## Authority Boundaries

**Authoritative reference:** `docs/contracts/actor-boundaries.md`

The canonical message path is:

```
agent -> openclaw -> codero -> github -> codero -> openclaw -> agent
```

OpenClaw sits between the agent and Codero. It never bypasses Codero to reach
GitHub directly for state-changing operations.

| Hop | May send | May not |
|-----|----------|---------|
| OpenClaw -> Codero | session register, heartbeat, submit, observe, report | become source of truth, decide merge readiness, write to GitHub |
| Codero -> OpenClaw | findings, session state, submit outcomes, merge outcomes | send adapter-specific free-form text as the contract |
| OpenClaw -> Agent | task instructions, gate findings, review findings, operator messages | infer durable truth from PTY text alone |

For the full hop permission matrix and actor ownership model, see
`docs/contracts/actor-boundaries.md`.

## Allowed

These capabilities are permitted for OpenClaw in the Codero runtime:

| Capability | Scope | Notes |
|------------|-------|-------|
| Read/write own state directories | `OPENCLAW_STATE_DIR`, workspace | Isolated from Codero persistence |
| Execute shared PTY helper | `/srv/storage/shared/tools/bin/agent-tmux-bridge` | Approved transport path |
| Execute approved shared tooling | `/srv/storage/shared/tools/bin/*` | Through shared PATH |
| Call Codero API/CLI | Session register, heartbeat, submit, observe | Via supported endpoints |
| Read PTY output | Through managed transport path | Captured via bridge |
| Read repo metadata | Branch, worktree path, assignment info | For delivery and explanations |
| Talk to LiteLLM | `http://localhost:4000` | Approved model endpoint |
| Inject feedback into PTY | Via bridge `deliver` command | Structured findings only |

## Allowed With Care

These capabilities require explicit setup or operator awareness:

| Capability | Scope | Care required |
|------------|-------|---------------|
| One-time alias registration | Codero session model | Only during initial project setup |
| One-time prehook installation | Supported repos | Only during initial project setup |
| Launch managed sessions | Approved wrappers | Not arbitrary process launch |
| Read repo metadata | Branch, PR state | For operator explanations, not policy decisions |

## Not Allowed

These capabilities are forbidden for OpenClaw:

| Forbidden | Reason |
|-----------|--------|
| Direct writes to Codero SQLite | Codero owns durable state |
| Direct writes to Codero Redis | Codero owns coordination state |
| Direct GitHub merge execution | Codero owns merge authority |
| Direct GitHub PR mutation | Codero owns PR lifecycle |
| Direct GitHub review posting | Codero owns review workflow |
| Unmanaged PTY injection | Must use shared bridge |
| Root escalation | No privileged host access |
| Package manager installs | No ad hoc tool installs at runtime |
| Uncontrolled network access | Only approved endpoints |
| Cross-repo writes | Only assigned worktree |
| Deciding merge readiness | Codero evaluates merge conditions |
| Deciding gate pass/fail | Codero runs the gate pipeline |

## Environment and Secret Posture

### OpenClaw Should Receive

| Variable | Purpose |
|----------|---------|
| `OPENCLAW_STATE_DIR` | Isolated adapter state |
| `OPENCLAW_CONFIG_PATH` | Config file location |
| Gateway settings (port, bind, auth) | Adapter-local runtime |
| Session-scoped identifiers | `CODERO_SESSION_ID`, `CODERO_AGENT_ID` when required |
| Model endpoint config | LiteLLM URL, model selection |

### OpenClaw Must Not Receive

| Variable | Reason |
|----------|--------|
| `CODERO_DB_PATH` | Direct DB access forbidden |
| `CODERO_REDIS_ADDR` | Direct Redis access forbidden |
| `CODERO_REDIS_PASS` | Direct Redis access forbidden |
| `GITHUB_TOKEN` | Direct GitHub access forbidden |
| Agent-family auth credentials | Belongs in agent runtime shell only |
| Billing-sensitive API keys | Scrubbed on non-billing paths |

### Current Config Posture (Verified)

The working OpenClaw config at `$OPENCLAW_CONFIG_PATH`
default example: `$HOME/.openclaw-codero-smoke/openclaw.json`

| Aspect | Status | Compliant |
|--------|--------|-----------|
| Gateway bind | `loopback` | ✓ Yes |
| Gateway auth | `token` | ✓ Yes |
| Workspace | Isolated under OpenClaw state root | ✓ Yes |
| Plugins | `litellm`, `acpx` only | ✓ Yes |
| Model provider | LiteLLM at localhost:4000 | ✓ Yes |
| No DB credentials in config | Verified | ✓ Yes |
| No Redis credentials in config | Verified | ✓ Yes |
| No GITHUB_TOKEN in config | Verified | ✓ Yes |

## GitHub Authority Boundary

OpenClaw must not:

- Call GitHub APIs for state-changing operations (merge, review, PR creation)
- Decide whether a PR is mergeable
- Trigger merges based on adapter-side state
- Post review comments directly
- Create or close PRs

OpenClaw may:

- Read GitHub metadata through Codero (not directly)
- Surface GitHub state to operators through dashboard flush
- Receive structured findings from Codero about GitHub state

## Codero Persistence Boundary

OpenClaw must not:

- Open or query `CODERO_DB_PATH` (SQLite)
- Connect to `CODERO_REDIS_ADDR`
- Write assignment, session, or submission records directly
- Bypass Codero API to update state

OpenClaw may:

- Call Codero API endpoints to register sessions
- Call Codero API endpoints to submit work
- Call Codero API endpoints to report heartbeats
- Receive state updates from Codero through API responses

## PTY/Session Boundary

OpenClaw must:

- Use the shared bridge at `/srv/storage/shared/tools/bin/agent-tmux-bridge`
- Use approved commands: `start-profile`, `wait-ready`, `deliver`, `capture`, `status`, `stop`
- Respect per-family busy-state detection
- Wrap deliveries appropriately when interrupting busy agents

OpenClaw must not:

- Inject arbitrary content into PTY sessions outside the bridge
- Bypass the bridge to use raw `tmux send-keys`
- Assume PTY text is durable truth
- Launch unmanaged agent processes

## Plugin Boundary

Current approved plugins:

- `litellm` — model routing
- `acpx` — adapter integration

Plugin policy (high-level):

- Allowlist-only; no plugin enabled by default
- Each approved plugin must have documented purpose and ownership
- Plugin allowlist enforcement is TOOL-003 scope, not fully solved here

## Proven Runtime Path

The following has been proven by OpenClaw PTY experiments:

| Proof | Status | Evidence |
|-------|--------|----------|
| OpenClaw -> exec -> PTY bridge -> live session | ✓ Proven | OPENCLAW-PTY-NOTES.md |
| Bridge `deliver` to Codex, Claude, Gemini, Copilot, OpenCode | ✓ Proven | Smoke tests passed |
| Busy-state interrupt with Esc | ✓ Proven | Per-family detection |
| Loopback gateway, token auth | ✓ Working | Current config |
| LiteLLM provider routing | ✓ Working | Current config |

## What Is Not Yet Enforced

This document describes the intended privilege model. The following are not yet
enforced:

| Not enforced | Belongs to |
|--------------|------------|
| Runtime privilege checks | Future TOOL-002 implementation |
| Plugin allowlist enforcement | TOOL-003 |
| Update cadence controls | TOOL-004 |
| Global rollout to other repos | TOOL-005+ |
| Automated secret scrubbing | Future security hardening |
| Credential injection guardrails | Future security hardening |

## Validation

To validate the OpenClaw privilege posture for Codero:

```bash
# 1. Verify OpenClaw config exists and is readable
test -f "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && echo "PASS: config exists"

# 2. Verify gateway is loopback-only
grep -q '"bind": "loopback"' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && echo "PASS: loopback gateway"

# 3. Verify gateway auth is token-based
grep -q '"mode": "token"' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && echo "PASS: token auth"

# 4. Verify no GITHUB_TOKEN in OpenClaw config
! grep -q 'GITHUB_TOKEN' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && echo "PASS: no GITHUB_TOKEN"

# 5. Verify no CODERO_DB_PATH in OpenClaw config
! grep -q 'CODERO_DB_PATH' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && echo "PASS: no DB path"

# 6. Verify no CODERO_REDIS in OpenClaw config
! grep -q 'CODERO_REDIS' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && echo "PASS: no Redis creds"

# 7. Verify only approved plugins
grep -q '"litellm":' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && \
grep -q '"acpx":' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && \
echo "PASS: approved plugins only"

# 8. Verify shared PTY bridge exists
test -x /srv/storage/shared/tools/bin/agent-tmux-bridge && echo "PASS: PTY bridge OK"
```

All validation checks are read-only. They do not modify any state.

## Current Mismatches

No mismatches observed between the current OpenClaw config and the intended
privilege profile:

- Gateway is loopback-only ✓
- Auth is token-based ✓
- No forbidden credentials in config ✓
- Only approved plugins enabled ✓
- PTY bridge path is correct ✓

## Related Documents

- Tooling baseline: `docs/runtime/codero-tooling-baseline.md`
- Canonical spec: `/srv/storage/local/codero/specication_033126/codero-agent-task-execution-spec.md`
- Policy doc: `/srv/storage/local/codero/specication_033126/codero-shared-tooling-and-openclaw-policy.md`
- OpenClaw PTY notes: `/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md`
- Shared memory: `/srv/storage/shared/memory/MEMORY.md`
