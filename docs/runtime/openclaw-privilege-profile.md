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

```text
agent -> openclaw -> codero -> github -> codero -> openclaw -> agent
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
