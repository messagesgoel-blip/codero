# Codero Agent Task Execution Roadmap

Status: **superseded**
Owner: sanjay
Updated: 2026-04-03
Superseded by: `docs/roadmaps/dogfood-execution-roadmap.md` (2026-04-03)

> **This document is retained for historical reference only.** The active
> execution sequence is defined in the dogfood execution roadmap, beginning
> with WIRE-001. Tasks SUB-001 through FIN-001 below are replaced by
> WIRE-001 through PRV-011 in the new roadmap.

## Purpose (Historical)

This was the canonical implementation-sequencing roadmap for Codero.

This file is retained for historical reference. Use
`docs/roadmaps/dogfood-execution-roadmap.md` for active task selection and
implementation sequencing.

`docs/roadmap.md` remains the roadmap index and strategic/release view. It does
not override this file for active implementation order.

## Source-Of-Truth Rules

- Use the earliest incomplete task in wave order unless the user explicitly
  overrides it.
- Treat `docs/roadmaps/*-backlog.md` as candidate planning only.
- Do not use untracked local planning files as the next-task source.
- If work lands slightly out of suggested order, resume from the earliest
  incomplete task still listed here and note the exception in continuity.

## Current Baseline On Main

- `origin/main` head: `db3c716` (includes SES-003 PR `#157`, SES-002 PR
  `#156`, and SES-001 PR `#154`)
- This branch closes the remaining agent/setup + session-core gap by landing
  `SES-004`.
- Completed on the current `main` branch:
  - `TOOL-001` through `TOOL-005`
  - `BND-001` through `BND-004`
  - `SET-001` and `SET-002`
  - `SES-001`, `SES-002`, `SES-003`, and `SES-004`
- Agent/setup set is complete.
- Session set is complete.
- Next unmerged tasks in canonical order:
  1. `SUB-001` through `SUB-005`
  2. `REV-001` through `REV-005`
  3. `FIN-001`

## Critical Path

1. `TOOL-001` through `TOOL-004`
2. `BND-001` through `BND-004`
3. `SET-001` through `SET-002`
4. `SES-001` through `SES-004`
5. `SUB-001` through `SUB-005`
6. `REV-001` through `REV-005`
7. `FIN-001`
8. `DASH-001` through `DASH-004`
9. `CERT-001`, `CERT-002`, `SEC-001`, `SEC-002`, `CUT-002`

`DASH-005`, `OCL-002`, and `OCL-003` remain important but are outside the
shortest path to an OpenClaw-default runtime.

## Execution Waves

### Wave 0: Tooling Foundation

| Task | Title | Status | Notes |
|---|---|---|---|
| `TOOL-001` | Standardize the shared tooling baseline across repos | `done` | Landed with the tooling-baseline merge set |
| `TOOL-002` | Define the OpenClaw privilege profile and denylist | `done` | Landed with the tooling-baseline merge set |
| `TOOL-003` | Pin the OpenClaw plugin allowlist and intended usage | `done` | Landed with the tooling-baseline merge set |
| `TOOL-004` | Pin OpenClaw update cadence and change-control checks | `done` | Landed with the tooling-baseline merge set |
| `TOOL-005` | Define a repo onboarding note for shared tooling and OpenClaw | `done` | PR landed; see `docs/runtime/repo-onboarding.md` |

### Wave 1: Boundary Freeze And Setup

| Task | Title | Status | Notes |
|---|---|---|---|
| `BND-001` | Freeze actor authority and hop boundaries | `done` | PR `#147` |
| `BND-002` | Enforce environment ownership by layer | `done` | PR `#148` |
| `BND-003` | Standardize managed session launch and identity contract | `done` | PR `#149` |
| `BND-004` | Formalize event envelope and `reply_to` boundary | `done` | PR `#149` |
| `SET-001` | Formalize one-time alias registration setup | `done` | PR `#152` |
| `SET-002` | Formalize one-time prehook installation setup | `done` | Follows `SET-001` |

### Wave 2: Session And Local Submit Core

| Task | Title | Status | Notes |
|---|---|---|---|
| `SES-001` | Complete session register, confirm, heartbeat, and finalize parity | `done` | Landed with PR #154 |
| `SES-002` | Implement idempotent observe and attach behavior | `done` | Landed with PR #156 |
| `SES-003` | Expose a Codero-owned deliver contract backed by the bridge | `done` | Bridge-backed delivery implemented in `replyToDirectClient` |
| `SES-004` | Preserve session continuity across adapter restart and phase shift | `done` | Implemented session recovery service in `internal/daemon/grpc/sessions_recovery.go` |
| `SUB-001` | Parse `TASK_COMPLETE` and structured summary blocks | `planned` | |
| `SUB-002` | Add Submission Record persistence and lineage | `planned` | |
| `SUB-003` | Reject duplicate or invalid submissions before lock acquisition | `planned` | |
| `SUB-004` | Implement the local gate loop with explicit local attempts | `planned` | |
| `SUB-005` | Commit, push, PR bootstrap, and reviewer request under policy | `planned` | |

### Wave 3: Remote Review And Finalization

| Task | Title | Status |
|---|---|---|
| `REV-001` | Add webhook wake dedupe and stale-SHA filtering | `planned` |
| `REV-002` | Build merge readiness evaluation from live PR state | `planned` |
| `REV-003` | Normalize unresolved review findings for agent feedback | `planned` |
| `REV-004` | Route `mergeable_state` explicitly | `planned` |
| `REV-005` | Complete reviewer routing and PR metadata ownership | `planned` |
| `FIN-001` | Finalize assignments, locks, and archive state cleanly | `planned` |

### Wave 4: Operator View And Adapter Polish

| Task | Title | Status |
|---|---|---|
| `DASH-001` | Implement Session And Activation slice | `planned` |
| `DASH-002` | Implement Submit And Local Gate slice | `planned` |
| `DASH-003` | Implement Remote Review And Merge Readiness slice | `planned` |
| `DASH-004` | Implement Finalization And Agent Performance slice | `planned` |
| `OCL-001` | Implement durable OpenClaw `reply_to` endpoint handling | `planned` |
| `OCL-002` | Add queued operator note behavior | `planned` |
| `OCL-003` | Add operator query audit log | `planned` |
| `OCL-004` | Formalize PTY family detectors and maintenance workflow | `planned` |

### Wave 5: Hardening And Cutover

| Task | Title | Status |
|---|---|---|
| `CERT-001` | Automate the runtime certification suite as a ship gate | `planned` |
| `CERT-002` | Fix daemon versus direct-DB registration parity | `planned` |
| `SEC-001` | Trim env, auth, and billing exposure aggressively | `planned` |
| `SEC-002` | Build env toggle matrix and quirk register | `planned` |
| `CUT-001` | Trim overlapping Codero chat and adapter responsibilities | `planned` |
| `CUT-002` | Stabilize dashboard-facing APIs before React and Vite migration | `planned` |
| `DASH-005` | Add repo scanner and repository observability | `planned` |

## Suggested Claim Order From Here

1. `SUB-001`
2. `SUB-002`
3. `SUB-003`

## Parallelization Rule

After `BND-001` through `BND-004` are settled, work can split safely into:

- session and submit core
- remote review and merge
- dashboard and operator read models
- validation and certification

No track may invent a boundary that contradicts this roadmap or the committed
contracts.

## Minimum Ship Gate

The minimum ship gate for adopting the OpenClaw-default runtime is:

- `TOOL-001` through `TOOL-004` complete
- `BND-001` through `BND-004` complete
- `SES-001` through `SES-004` complete
- `SUB-001` through `SUB-005` complete
- `REV-001` through `REV-004` complete
- `FIN-001` complete
- `DASH-001` through `DASH-004` complete
- `CERT-001`, `CERT-002`, `SEC-001`, `SEC-002`, and `CUT-002` complete

`DASH-005`, `OCL-002`, and `OCL-003` remain important, but they are not
required to make the execution contract viable.

## Supporting Documents

- Strategic/index roadmap: `docs/roadmap.md`
- Historical roadmap snapshots: `docs/roadmaps/codero-roadmap-v4.md`,
  `docs/roadmaps/codero-roadmap-v5.md`
- Runtime/tooling docs landed for Wave 0:
  - `docs/runtime/codero-tooling-baseline.md`
  - `docs/runtime/openclaw-privilege-profile.md`
  - `docs/runtime/openclaw-plugin-policy.md`
  - `docs/runtime/openclaw-update-policy.md`
  - `docs/runtime/repo-onboarding.md` — repo onboarding note for the shared tooling and OpenClaw baseline (`TOOL-005`)
- Boundary docs landed for Wave 1:
  - `docs/contracts/actor-boundaries.md`
  - `docs/contracts/env-ownership.md`
  - `docs/contracts/session-lifecycle-contract.md`
  - `docs/contracts/bot-pty-delivery-contract.md`
- Setup docs for Wave 1:
  - `docs/contracts/alias-registration.md`
  - `docs/contracts/prehook-installation.md`
