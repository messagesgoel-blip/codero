# Actor Boundaries and Hop Model

**Version:** 1.0
**Task:** BND-001
**Last Updated:** 2026-04-01
**Status:** canonical for Codero repo

## Purpose

This document freezes actor authority and hop boundaries across agent, OpenClaw,
Codero, and GitHub. It is the single authoritative reference for ownership
models within the Codero repo.

All other repo-local docs must align with this document. When ownership
questions arise, this document takes precedence over older inline statements in
other contracts or roadmaps.

**Canonical spec authority:** This document derives from and must not contradict
`/srv/storage/local/codero/specication_033126/codero-agent-task-execution-spec.md`.

## Canonical Hop Model

Every task execution follows this message path:

```
agent -> openclaw -> codero -> github -> codero -> openclaw -> agent
```

No actor may skip a hop. No actor may silently cross another actor's authority
boundary.

## Runtime Actors

Five actors participate in task execution:

| Actor | Boundary | Scope |
|-------|----------|-------|
| Operator | orchestration | worktree provisioning, agent launch, PTY termination, retention policy, operator alerts |
| Agent | agent runtime | file edits and tool use inside assigned worktree; emits `TASK_COMPLETE` only |
| OpenClaw | adapter | session registration, heartbeats, PTY transport, chat continuity, feedback injection, telemetry |
| Codero | control plane | durable state, workflow truth, policy, commit/push, PR lifecycle, merge authority, operator alerts |
| GitHub | external authority | CI checks, reviewer decisions, PR mergeability, merge acceptance |

## Authority Matrix

### Agent

**Owns:**

- file edits and tool execution inside the assigned worktree
- `TASK_COMPLETE` signal with structured summary block

**Does not own:**

- session registration (launcher/OpenClaw owns this)
- heartbeat (launcher/OpenClaw owns this)
- commit authority
- push authority
- GitHub mutation
- merge readiness evaluation
- merge execution
- recovery or handoff decisions

### OpenClaw

**Owns:**

- session registration and confirmation with Codero
- periodic heartbeats to Codero
- PTY transport through the shared bridge
- chat continuity for agent sessions
- feedback injection into live PTY sessions
- submit observation and structured payload delivery to Codero
- telemetry collection and dashboard flush

**Does not own:**

- durable assignment state (Codero owns this)
- workflow truth (Codero owns this)
- merge readiness evaluation (Codero owns this)
- merge execution (Codero owns this)
- direct GitHub mutation (Codero owns this)
- direct database access (Codero owns this)
- direct Redis access (Codero owns this)
- PTY family-specific submit heuristics as source of truth (these support
  Codero's contract but do not replace it)

### Codero

**Owns:**

- durable session, assignment, and branch state
- assignment lifecycle and state machine
- delivery pipeline FSM (submit → merge flow)
- local gate evaluation
- commit under policy
- push under policy
- PR creation and PR lifecycle
- reviewer request and CODEOWNERS matching
- remote review monitoring
- merge readiness evaluation
- merge execution
- operator alerts
- feedback packaging and structured findings
- `reply_to` endpoint for feedback injection routing

**Does not own:**

- chat continuity (OpenClaw owns this)
- PTY transport details (OpenClaw + bridge own this)
- PTY family detection and busy markers (adapter layer owns this)
- agent launch and termination (Operator owns this)
- CI execution (GitHub owns this)
- reviewer decisions (GitHub owns this)
- merge acceptance (GitHub owns this)

### GitHub

**Owns:**

- CI check execution and results
- reviewer decisions and approval state
- PR mergeability computation
- merge acceptance or rejection

**Does not own:**

- interpretation of PR state for Codero workflow (Codero owns this)
- retry decisions (Codero owns this)
- feedback assembly (Codero owns this)

## Hop Permissions

| Hop | May send | May not |
|-----|----------|---------|
| Agent -> OpenClaw | `TASK_COMPLETE` with summary block, PTY output | GitHub mutation, merge request, durable state write |
| OpenClaw -> Codero | session register, session confirm, heartbeat, submit payload, observe, report | become source of truth, decide merge readiness, write to GitHub, write to Codero DB/Redis |
| Codero -> GitHub | PR create, PR lookup, review fetch, merge readiness check, merge execute | perform state changes outside the GitHub client contract |
| GitHub -> Codero | CI status, review state, PR mergeability, merge result | replace Codero's interpretation of PR state |
| Codero -> OpenClaw | structured findings, session state, submit outcomes, merge outcomes | send adapter-specific free-form text as the contract |
| OpenClaw -> Agent | task instructions, local gate findings, remote review findings, operator follow-ups | infer durable truth from PTY text alone |

## Adapter Operations

OpenClaw adapter responsibilities toward Codero are frozen around these
operations:

| Operation | Direction | Purpose |
|-----------|-----------|---------|
| `register` | OpenClaw -> Codero | create or refresh session record |
| `confirm` | OpenClaw -> Codero | verify session identity |
| `heartbeat` | OpenClaw -> Codero | update liveness, keep session alive |
| `observe` | OpenClaw -> Codero | read assignment state without mutation |
| `deliver` | OpenClaw -> Agent | inject feedback into live PTY |
| `submit` | OpenClaw -> Codero | hand structured submit payload after `TASK_COMPLETE` |
| `feedback inject` | Codero -> OpenClaw -> Agent | route structured findings into live session |

These operations are the adapter contract. Additional operations must be
explicitly documented before use.

## Control Plane Operations

Codero control plane responsibilities are frozen around these operations:

| Operation | Direction | Purpose |
|-----------|-----------|---------|
| accept registration | Codero <- OpenClaw | persist session record, issue heartbeat secret |
| validate heartbeat | Codero <- OpenClaw | update `last_seen_at`, verify secret |
| attach assignment | Codero | link session to repo/branch/worktree |
| receive submit | Codero <- OpenClaw | create Submission Record, begin gate loop |
| run local gate | Codero | evaluate worktree against gate pipeline |
| commit | Codero -> git | commit under Codero policy |
| push | Codero -> git | push branch under Codero policy |
| create PR | Codero -> GitHub | create or lookup PR |
| monitor PR | Codero <- GitHub | poll or receive webhook for state changes |
| evaluate merge readiness | Codero | check CI, approval, unresolved threads, mergeable_state |
| execute merge | Codero -> GitHub | call GitHub merge API after final checks |
| package feedback | Codero | assemble structured findings from gate, CI, review |
| route feedback | Codero -> OpenClaw | send findings to `reply_to` endpoint |
| finalize | Codero | write outcome, release lock, archive session |

## Environment Ownership

| Env group | Owner | Purpose |
|-----------|-------|---------|
| `E-AGENT` | Agent / Launcher | session identity, worktree path, mode |
| `E-BOOTSTRAP` | Codero / Launcher | runtime root, daemon address, pilot config |
| `E-TRACKING` | Codero | tracking configuration |
| `E-OPENCLAW` | OpenClaw | state directory, config path |
| `E-CODERO` | Codero | database, Redis, API address, logging |
| `E-WEBHOOK` | Codero | webhook secret |
| `E-DASH` | Codero | dashboard port, base path |
| `E-GITHUB` | Codero | GitHub token, merge settings |

OpenClaw must not receive `E-CODERO` or `E-GITHUB` credentials. See
`docs/runtime/openclaw-privilege-profile.md` for the detailed privilege model.

## Durable State Ownership

| State | Owner | Storage |
|-------|-------|---------|
| `agent_sessions` | Codero | SQLite |
| `agent_assignments` | Codero | SQLite |
| `branch_states` | Codero | SQLite |
| `session_archives` | Codero | SQLite |
| `delivery_events` | Codero | SQLite |
| `findings` | Codero | SQLite |
| `review_runs` | Codero | SQLite |
| coordination keys | Codero | Redis |
| OpenClaw workspace | OpenClaw | filesystem |
| PTY session state | tmux / adapter | runtime |

OpenClaw may read its own workspace and PTY output. It may not read or write
Codero's SQLite or Redis directly.

## PTY Transport Boundary

The shared PTY bridge at `/srv/storage/shared/tools/bin/agent-tmux-bridge` is
the approved transport path.

- OpenClaw uses the bridge for PTY read/write
- OpenClaw must not inject into PTY outside the bridge
- Codero routes feedback through OpenClaw via `reply_to` endpoint
- Codero does not inject into PTY directly

## Submission Record Ownership

The Submission Record is Codero-owned:

- created by Codero on `submit` receipt
- contains `submission_id`, `head_sha`, `diff_hash`, attempt indices, state
- not duplicated or owned by OpenClaw
- not accessible to agent directly

## Merge Authority

Merge authority belongs to Codero exclusively:

1. Codero evaluates merge readiness from live PR state
2. Codero calls GitHub merge API after all predicates pass
3. Codero writes final outcome to durable store

OpenClaw does not decide merge readiness. Agent does not request merge. GitHub
does not auto-merge without Codero's explicit call.

## What This Document Does Not Cover

- plugin allowlist (see `docs/runtime/openclaw-plugin-policy.md`)
- update cadence (see `docs/runtime/openclaw-update-policy.md`)
- detailed privilege profile (see `docs/runtime/openclaw-privilege-profile.md`)
- tooling baseline (see `docs/runtime/codero-tooling-baseline.md`)
- global enforcement (not yet implemented)
- other repos (this document is Codero-local)

## Validation

To verify alignment:

1. Grep for contradictory ownership statements:
   ```bash
   rg -i 'openclaw.*(owns?|authority|truth)' docs/
   rg -i 'agent.*(merge|github).*authority' docs/
   ```

2. Verify no doc claims OpenClaw owns durable state or merge authority.

3. Verify no doc claims agent owns commit, push, or GitHub mutation.

4. Confirm the six-hop model is consistent across:
   - `docs/contracts/actor-boundaries.md` (this document)
   - `docs/runtime/openclaw-privilege-profile.md`
   - `docs/contracts/agent-handling-contract.md`

## References

- Canonical spec: `/srv/storage/local/codero/specication_033126/codero-agent-task-execution-spec.md`
- Roadmap: `/srv/storage/local/codero/specication_033126/codero-agent-task-execution-roadmap.md`
- BND-001 task definition: freeze actor authority and hop boundaries

## Change Log

| Date | Version | Change |
|------|---------|--------|
| 2026-04-01 | 1.0 | Initial freeze (BND-001) |
