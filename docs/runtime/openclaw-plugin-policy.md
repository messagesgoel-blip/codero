# OpenClaw Plugin Policy for Codero

Status: shadow-mode / Codero-local  
Task: TOOL-003 (contained)  
Date: 2026-04-01

## Purpose

This document defines the OpenClaw plugin allowlist for Codero. It specifies
which plugins are approved, why they are needed, and what plugin behaviors are
acceptable within the Codero adapter model.

**Shadow mode means:**

- This document describes the intended plugin policy for Codero.
- It does not enforce this policy globally.
- It does not claim that other repos follow this allowlist.
- It does not modify shared OpenClaw defaults.
- It does not disable any bundled upstream plugins globally.
- Enforcement work belongs to later tasks.

The purpose is to make the approved plugin set explicit so future enforcement
work has a documented baseline.

## Policy Principle

**Allowlist-only.** No plugin is enabled by default just because it is bundled
upstream. Plugins are enabled only when they are required for the documented
adapter role.

OpenClaw is an adapter, not a workflow authority. Plugins must not:

- Become a source of truth for Codero-owned state
- Bypass the Codero API to mutate durable state
- Bypass the shared PTY bridge for session delivery
- Hold secrets that belong only in Codero or agent runtimes

## Approved Plugin Allowlist

The following plugins are approved for the Codero OpenClaw baseline:

| Plugin | Purpose | Required | Privilege scope |
|--------|---------|----------|-----------------|
| `litellm` | Model routing through local LiteLLM proxy | Yes | Model API calls only |
| `acpx` | Adapter integration for Codero runtime | Yes | Adapter IPC only |

### litellm

**Purpose:** Routes model requests through the local LiteLLM proxy at
`http://localhost:4000`. This provides model abstraction, cost tracking, and
fallback handling.

**Why needed:** OpenClaw must talk to models through a controlled endpoint.
Direct API keys to upstream providers are not handed to the adapter layer.
LiteLLM provides the approved routing path.

**Privilege scope:**

- HTTP calls to `localhost:4000`
- No direct upstream provider credentials
- No persistent state beyond in-memory request tracking

**Config shape:**

```json
"litellm": {
  "enabled": true,
  "config": {}
}
```

### acpx

**Purpose:** Adapter integration plugin for Codero runtime interop. Provides
the communication path between OpenClaw and Codero for session management,
heartbeats, and submit workflows.

**Why needed:** OpenClaw must call Codero for session register, heartbeat,
submit, and observe operations. The acpx plugin provides this integration
path without hardcoding adapter-specific IPC in the core OpenClaw runtime.

**Privilege scope:**

- IPC to Codero endpoints
- Session lifecycle calls
- No direct database or Redis access
- No direct GitHub access

**Config shape:**

```json
"acpx": {
  "enabled": true,
  "config": {}
}
```

## Non-Approved Plugin Categories

The following plugin categories are **not part of the Codero baseline** and
must not be enabled without explicit exception approval:

| Category | Reason for exclusion |
|----------|---------------------|
| Direct GitHub plugins | Codero owns GitHub authority |
| Database/persistence plugins | Codero owns durable state |
| Direct API provider plugins | Model routing goes through LiteLLM |
| Billing/payment plugins | Not in adapter scope |
| Social/chat platform plugins | Not in Codero runtime scope |
| Analytics plugins | Telemetry goes through Codero dashboard |
| Custom automation plugins | Risk of privilege escalation |

### Why bundled plugins may not be approved

OpenClaw may bundle plugins for broad use cases that do not apply to Codero:

- Codero has its own GitHub client; OpenClaw must not bypass it
- Codero has its own persistence layer; OpenClaw must not duplicate it
- Codero has its own telemetry surface; OpenClaw should not route around it
- Some bundled plugins assume direct API access that Codero intentionally does
  not grant to the adapter layer

If a bundled plugin is not on the approved list, it is disabled for Codero
even if it ships enabled upstream.

## Acceptable Plugin Behavior

Plugins in the approved set may:

- Route model requests through approved endpoints
- Make IPC calls to Codero
- Read session-scoped context for delivery and telemetry
- Participate in the OpenClaw startup and shutdown lifecycle

Plugins must not:

- Hold or request `GITHUB_TOKEN`
- Hold or request `CODERO_DB_PATH` or `CODERO_REDIS_*`
- Write to Codero persistence directly
- Call GitHub APIs for state-changing operations
- Bypass the shared PTY bridge
- Auto-enable other plugins
- Phone home to external telemetry endpoints outside approved paths
- Store credentials beyond the current session

## Relationship to Privilege Profile

This plugin policy is subordinate to the privilege profile defined in
`docs/runtime/openclaw-privilege-profile.md`. Approved plugins must not imply
or require privileges that the profile forbids.

| Forbidden privilege | Plugin must not |
|--------------------|-----------------|
| Direct DB access | Hold `CODERO_DB_PATH` |
| Direct Redis access | Hold `CODERO_REDIS_*` |
| Direct GitHub mutation | Hold `GITHUB_TOKEN` or call GitHub APIs |
| Unmanaged PTY injection | Bypass shared bridge |
| Root escalation | Require elevated permissions |

If a plugin requires a forbidden privilege to function, it cannot be approved
for the Codero baseline.

## Relationship to Shared PTY Bridge

The approved plugins do not replace or bypass the shared PTY bridge. The bridge
remains the only approved path for:

- Session launch (`start-profile`)
- Readiness detection (`wait-ready`)
- Message delivery (`deliver`)
- Output capture (`capture`)
- Status checks (`status`)
- Session stop (`stop`)

Plugins may coordinate with the bridge through OpenClaw, but must not:

- Directly invoke `tmux send-keys`
- Directly inject content into sessions
- Assume direct PTY access

## Current Config Evidence

The working OpenClaw config at `$OPENCLAW_CONFIG_PATH`
default example: `$HOME/.openclaw-codero-smoke/openclaw.json`
matches this plugin policy:

```json
"plugins": {
  "entries": {
    "litellm": {
      "enabled": true,
      "config": {}
    },
    "acpx": {
      "enabled": true,
      "config": {}
    }
  }
}
```

**Verification:**

| Check | Status |
|-------|--------|
| Only `litellm` and `acpx` enabled | ✓ Pass |
| No additional plugins enabled | ✓ Pass |
| Plugin configs are empty (no forbidden creds) | ✓ Pass |
| Model provider uses localhost | ✓ Pass |

## Adding a New Plugin

To add a plugin to the Codero baseline, the following must be documented:

1. **Purpose** — What does the plugin do for Codero?
2. **Owner** — Who maintains the plugin?
3. **Privileges needed** — What access does it require?
4. **Env it consumes** — What environment variables does it read?
5. **Forbidden check** — Does it require any forbidden privilege?
6. **Update path** — How is the plugin updated?
7. **Rollback plan** — How do we revert if the plugin fails?

A plugin cannot be added if:

- It requires a forbidden privilege
- It duplicates Codero-owned functionality
- It has no clear owner or update path
- It auto-enables other unapproved plugins

## What Is Not Yet Enforced

This document describes the intended plugin policy. The following are not yet
enforced:

| Not enforced | Belongs to |
|--------------|------------|
| Runtime plugin validation | Future enforcement work |
| Startup plugin allowlist check | Future enforcement work |
| Automated plugin disabling | Future enforcement work |
| Global plugin rollout | TOOL-005+ |
| Update cadence | TOOL-004 |

## Validation

To validate the OpenClaw plugin policy for Codero:

```bash
# 1. Verify only approved plugins are enabled
jq '.plugins.entries | keys' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}"

# 2. Verify litellm is enabled
jq '.plugins.entries.litellm.enabled' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}"

# 3. Verify acpx is enabled
jq '.plugins.entries.acpx.enabled' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}"

# 4. Count enabled plugins (should be exactly 2)
jq '[.plugins.entries | to_entries[] | select(.value.enabled == true)] | length' \
   "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}"

# 5. Verify no forbidden credentials in plugin configs
! grep -E 'GITHUB_TOKEN|CODERO_DB_PATH|CODERO_REDIS' \
   "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && echo "PASS: no forbidden creds"
```

All validation checks are read-only. They do not modify any state.

## Related Documents

- Tooling baseline: `docs/runtime/codero-tooling-baseline.md`
- Privilege profile: `docs/runtime/openclaw-privilege-profile.md`
- Canonical spec: `/srv/storage/local/codero/specication_033126/codero-agent-task-execution-spec.md`
- Policy doc: `/srv/storage/local/codero/specication_033126/codero-shared-tooling-and-openclaw-policy.md`
- OpenClaw PTY notes: `/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md`
