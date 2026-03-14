# codero

## Implementation Roadmap v5 — Repo-First, Contract-Bound, Module-Intake Driven

Status: proposed  
Owner: you  
Horizon: 6-12 months  
Revision intent: preserve the cleaner program structure of v5 while restoring the implementation contracts, failure semantics, and operator model that v4 captured well.

---

## 0) How to use this roadmap

This revision intentionally separates four layers that were previously mixed together:

1. **Roadmap body**: phases, sequencing, entry criteria, exit gates, and program priorities.
2. **Binding technical contracts**: state machine, Redis role boundaries, delivery semantics, pre-commit flow, operator actions, observability, and recovery rules.
3. **Module intake process**: the standing method for bringing validated ghwatcher behavior into codero without bulk copying.
4. **Execution backlog**: near-term sprint work. A detailed task ledger can exist separately, but the contracts in this document are the source of truth.

Order of precedence when there is any conflict:

1. Canonical state machine and binding appendices
2. Explicit resolved decisions in this roadmap
3. Phase descriptions and sprint plans
4. Separate execution backlog documents

This is the main conceptual correction to v5: it stays a roadmap, but it no longer leaves critical runtime behavior implicit.

---

## 1) Why this revised v5 exists

v4 was strong because it was implementation-ready: it named the state machine, Redis responsibilities, pre-commit review gates, operator actions, and concrete failure behavior. v5 was strong because it imposed cleaner sequencing: clean repo first, hard gates, deliberate module intake, and explicit architecture checkpoints.

The revised v5 keeps the v5 advantages and restores the v4 contracts.

What this revision changes relative to v5:

- restores the canonical state machine as an explicit appendix, not a pointer
- restores the Redis coordination contract and rebuild rules
- restores the pre-commit local review loop as a first-class workflow
- restores operator action semantics and observability contracts
- restores failure and recovery behavior as a deliverable, not an aspiration
- restores a decision and deferral register
- corrects phase sequencing so that "proof through daily use" is real, not nominal
- upgrades Phase 1 from "single repo" to "single operator, at least two active repos"

That last change is deliberate. Repo-qualified identity, fairness, and delivery routing should be proven during personal use, not discovered later in SaaS work.

---

## 2) Non-negotiable principles

- New repo starts clean. No bulk copy from ghwatcher.
- Every imported capability must have a contract, parity tests, and a rollback path.
- Durable state is always the source of truth.
- Redis is coordination only. Every Redis value must be either reconstructable from durable state or safe to lose because a durable secondary check exists.
- All branch lifecycle changes happen only through the canonical state machine.
- No model output may directly cause a state transition.
- Polling-only mode must remain fully functional. Webhooks are an optimization, not a requirement.
- Operator actions must have identical semantics across CLI, TUI, and later web UI.
- Multi-repo correctness is a Phase 1 proving requirement. Multi-tenant isolation is Phase 2.
- No phase is complete without tests, observability, runbooks, and recovery drills.
- The roadmap does not own agent lifecycle. codero manages review orchestration, not agent scheduling.

---

## 3) System boundaries and responsibility model

### 3.1 What codero owns

codero owns:

- branch registration and submission
- queue scoring, lease issuance, and retry handling
- PR review execution and finding normalization
- pre-commit local review orchestration
- feedback delivery and replay
- state tracking, transition auditing, and reconciliation against GitHub
- operator control surfaces and metrics

### 3.2 What codero does not own

codero does not own:

- starting agent sessions
- deciding when a human or cron launches an agent
- creating worktrees unless the operator explicitly runs the setup command
- external scheduler behavior
- direct merge decisions outside the deterministic state rules

There is no scheduler wrapper. Agent work is triggered manually or by external orchestration such as cron, launchd, CI, or another controller.

### 3.3 Full automation loop

The intended automation loop is:

operator or external orchestration starts agent -> agent works in isolated worktree -> agent requests pre-commit review -> Loop 1 LiteLLM review passes -> Loop 2 CodeRabbit pre-commit review passes -> pre-commit hook allows commit -> agent submits branch -> codero queues and dispatches PR review -> findings are delivered -> operator or orchestration re-triggers agent with findings -> agent fixes and re-submits -> codero confirms approval, CI green, zero pending events, and no unresolved review threads -> branch becomes merge_ready -> repo auto-merge completes the cycle.

### 3.4 Data layer by phase

| Phase | Layer | Role | Owns |
|---|---|---|---|
| Phase 1 | SQLite (WAL) | Durable source of truth | Branch state records, feedback bundles, transition audit log, migration history, effectiveness records |
| Phase 1 | Redis (local) | Ephemeral coordination | Lease TTLs, WFQ queue, slot counters, heartbeat keys, seq counter, webhook dedup keys, circuit breaker state, pre-commit slots, daily caps |
| Phase 1 | inbox.log or repo-qualified delivery stream | Append-only delivery | Agent-facing feedback delivery with monotonic seq IDs |
| Phase 2+ | PostgreSQL | Durable source of truth | Multi-tenant state, metering, audit log, effectiveness records |
| Phase 2+ | Managed Redis (non-cluster baseline) | Coordination and job primitives | Phase 1 coordination roles plus Asynq queues, per-tenant rate limits, pub/sub |
| Phase 2+ | Object-store backed delivery streams | Append-only delivery | Per-tenant or per-repo delivery streams, with seq semantics preserved |

### 3.5 System invariants

- Branch identity is **repo + branch + HEAD**, never branch name alone.
- `merge_ready` is computed deterministically, never inferred by model output.
- Seq numbers are monotonic, but they do not need to be contiguous.
- Every invalid state transition is rejected and logged.
- Durable store rebuild after Redis loss is a supported path, not an edge case.
- Every manual operator intervention must leave an auditable trace.

---

## 4) Phase plan

## Phase 0 — Program Setup and Clean Repo Bootstrap

Goal: establish execution discipline before feature work.

### Deliverables

- New empty `codero` repo with baseline structure, CI, and governance docs.
- `docs/architecture.md` defining system boundaries, runtime components, and durable versus ephemeral state.
- ADR process under `docs/adr/`.
- Module intake registry under `docs/module-intake-registry.md`.
- Failure and recovery matrix skeleton.
- Initial observability contract stub.

### Required controls

- PR template with scope, tests, risk, rollback.
- Protected main and branch strategy.
- Required CI checks.
- Release policy and compatibility policy.

### Exit gate

- CI green on empty scaffold commit.
- Contribution workflow is usable by a second engineer.
- ADRs merged for language/runtime, durable store, Redis role boundaries, and repo-qualified identity.
- Module intake template exists and is tested on at least one pilot intake stub.

---

## Phase 1 — Core Runtime and Personal Proof (Single Operator, At Least Two Active Repos)

Goal: build a minimal but production-quality control plane that works daily across at least two real repositories.

This is another intentional conceptual correction. Multi-repo identity and fairness should be validated before SaaS, because even personal use already spans multiple active projects.

### Scope

#### 1A. Runtime and storage

- Single binary with daemon and CLI surfaces.
- SQLite WAL durable store and embedded migrations.
- Centralized Redis wrapper package with all key naming and Lua scripts.
- Startup crash recovery and structured internal logging.

#### 1B. Canonical branch lifecycle

- Full 10-state, 20-transition state machine.
- Invalid-transition rejection and audit logging.
- Repo-qualified state records.
- Deterministic `merge_ready` computation.

#### 1C. Queue, lease, and delivery

- WFQ queue and priority ceiling.
- Lease issuance, expiry, and safety-net audit.
- Slot counters and queue-stalled detection.
- Append-only feedback delivery with replay semantics.
- Polling-first reconciliation path, with optional webhook acceleration.

#### 1D. Pre-commit local review loops

- `local_review` state.
- Sequential LiteLLM then CodeRabbit local review.
- Hook-based commit enforcement.
- Separate pre-commit slot limits and daily cost cap.

#### 1E. Operator surfaces and observability

- CLI submit, heartbeat, poll, why, reactivate, init-worktree, commit-gate.
- TUI queue, branch detail, event log, and effectiveness views.
- Health, queue, metrics, and agent-metrics endpoints.

#### 1F. Hardening and proving period

- Failure-mode audit.
- End-to-end integration cycle.
- Redis loss and restart drills.
- Webhook replay tests.
- Unresolved review thread cross-check tests.
- 30-day real-use sign-off with explicit activity thresholds.

### Out of scope

- Tenant billing and metering
- Enterprise RBAC
- GitHub App onboarding for external customers
- Plan enforcement and formal SaaS packaging

### Exit gate

All of the following must be true:

- 30 days of daily use completed across at least two active repositories.
- Minimum operating thresholds achieved: 3 branches reviewed per week, 2 stale detections observed, 1 lease-expiry recovery observed, and 10 pre-commit reviews per project per week.
- Zero manual DB repair incidents.
- Zero missed feedback deliveries.
- Zero silent queue stalls.
- Zero undetected stale branches.
- Recovery tests pass for Redis restart, daemon restart, SIGKILL aftermath, and duplicate webhook delivery.
- Pre-commit loops are enforced by hook, not policy alone.

### Phase gate to Phase 2

Phase 2 does **not** begin immediately after implementation completes. It begins only after a proving period.

Default gate:

- formal 30-day sign-off complete, and
- sustained boring daily use for a longer proving window, target 3-6 months, with 6 months as the default bar unless there is a strong reason to accelerate.

That preserves the original v4 principle: build it for yourself first and prove it in unremarkable daily use.

---

## Phase 2 — Platformization and Tenant-Ready Foundation

Goal: replace local-only infrastructure with tenant-ready infrastructure without changing the proven runtime semantics.

### Scope

- PostgreSQL migration with `tenant_id` on all durable tables.
- Managed Redis in non-cluster mode as baseline.
- Asynq as the Phase 2 job primitive.
- GitHub App, OAuth, installation flow, and tenant provisioning.
- Per-tenant queue isolation, slot isolation, and rate limiting.
- Multi-repo and org-level routing as first-class product features.
- Object-store backed delivery streams.

### Exit gate

- New org can install and receive first review without manual intervention.
- Two tenants cannot affect each other’s queue scores, slot allocation, or rate limits.
- Managed Redis validation confirms keyspace notifications, lease expiry behavior, and recovery behavior end to end.
- Migration from personal Phase 1 environment to tenant-ready deployment is tested.

---

## Phase 3 — Operator Product, Hardening, and Enterprise Controls

Goal: make codero inspectable, supportable, and safe to run for others.

### Scope

- Web dashboard with parity to core TUI operator actions.
- RBAC and immutable operator audit log.
- Live queue views via Redis pub/sub.
- Status checks, inline annotations, and GitHub re-run triggers.
- Secrets management, restore drills, and backup validation.
- Stateless processing audit for code content.
- Data residency and deletion policies.
- SOC 2 readiness gap analysis.

### Exit gate

- All Phase 1 operator actions are available with identical semantics in web UI.
- Viewer role cannot execute state-changing actions.
- Audit log covers every operator action.
- Restore and deletion workflows are tested.
- Status checks and merge blocking work deterministically.

---

## Phase 4 — Commercialization and Selective Architecture Upgrades

Goal: commercialize only after semantics, operations, and tenant isolation are already proven.

### Scope

- Billing and metering
- Plan enforcement
- Packaging and support model
- Pricing and onboarding workflow
- Optional workflow-engine upgrade only if trigger conditions are met

### Hard entry criteria

- Phase 3 complete
- clear demand signal
- pricing hypothesis exists
- operator burden is acceptable
- tenant isolation and deletion guarantees already tested

Temporal or another workflow engine is not Phase 4 by default. It is conditional and must earn its way in.

---

## 5) Repo structure decision

Recommended start: single service repo with strict internal boundaries.

```text
cmd/        entrypoints
internal/   state, scheduler, delivery, webhook, tui, precommit, redis, github
pkg/        shared contracts only
docs/       architecture, ADRs, runbooks, module intake registry
tests/      unit, integration, contract, simulation
```

Rationale:

- fastest path to clean boundaries
- simplest CI and release flow
- easiest place to enforce module intake discipline
- avoids coordination tax before semantics are stable

Decision checkpoint:

- choose single-repo structure at Phase 0 exit
- revisit a repo split only after Phase 2 metrics demonstrate a real coordination problem

---

## 6) Module intake workflow (standing process, not a separate late phase)

Module intake is a permanent process used whenever codero adopts proven behavior from ghwatcher.

### Intake steps

1. Define target contract in codero: inputs, outputs, errors, invariants.
2. Identify source module and exact behavior to preserve.
3. Write parity tests in codero before integration.
4. Integrate via adapter or direct port, preferring the smallest safe change.
5. Validate parity, load behavior, and failure behavior.
6. Record decision, owner, rollback path, and status in the registry.

### Guardrails

- no bulk copy from ghwatcher
- no adopted module without parity tests
- no adopted module without a rollback note
- no silent semantic changes during import

### Initial priority intake queue

Priority A:

- MI-001 lease semantics and transition safety
- MI-002 webhook ingestion and dedup
- MI-003 relay / claim / ack / resolve delivery model
- MI-004 session heartbeat and stale-session handling

Priority B:

- review routing policy engine
- active agent relay worker model
- overview and documentation surfaces

Priority C:

- LLM-assisted routing
- advanced watchdog heuristics

### Registry fields

At minimum each intake entry records:

- module ID
- target contract doc
- source module path
- owner
- integration mode: adapter or port
- parity test location
- rollback method
- status
- ADR link
- notes on semantic deviations

---

## 7) Quality gates by layer

### Code

- formatting, lint, and unit tests mandatory
- property or mutation-style tests on state transitions and lease logic
- queue and Redis wrapper tests run in CI

### Contracts

- CLI and API contract snapshots versioned
- backward compatibility checks in CI once external surfaces stabilize

### Operations

- health and metrics endpoints mandatory
- recovery drills mandatory
- reconstructability from durable state after Redis loss mandatory

### Product behavior

- documented manual override procedures
- deterministic merge-readiness rules
- no silent drops in delivery or reconciliation

### AI usage

- model outputs are advisory or normalized, never directly state-changing
- hot path timeouts are explicit
- LiteLLM logging of sensitive payloads disabled in production paths

---

## 8) First six execution sprints

### Sprint 1

- bootstrap repo
- ADRs for runtime, durable store, Redis role boundaries, repo-qualified identity
- architecture baseline
- module intake registry template
- contribution and release policy

### Sprint 2

- single binary layout
- config loader and startup validation
- SQLite schema and migrations
- Redis wrapper package
- canonical state machine implementation
- initial test harness

### Sprint 3

- CLI submit, heartbeat, poll, why, reactivate, init-worktree
- startup crash recovery
- structured internal log
- repo-qualified identity wired end to end

### Sprint 4

- WFQ scorer and queue
- lease issuance and expiry
- slot counter and queue-stalled detection
- lease audit goroutine
- observability skeleton: `/health`, `/queue`, `/metrics`

### Sprint 5

- PR review runner
- finding normalizer
- append-only delivery and replay
- webhook receiver, dedup, reconciliation loop
- heartbeat expiry and abandoned-state handling
- polling-only mode default

### Sprint 6

- `local_review` activation
- LiteLLM and CodeRabbit pre-commit loops
- pre-commit hook enforcement and commit-gate
- TUI operator surface
- effectiveness metrics
- hardening matrix and first end-to-end test

After Sprint 6, the work shifts from implementation breadth to proving behavior under daily use.

---

## Appendix A — Canonical state machine (binding)

This is the single source of truth for branch lifecycle behavior.

Invalid transitions are rejected with `InvalidTransition(from_state, to_state)` and logged.

| # | From state | To state | Trigger | Notes |
|---|---|---|---|---|
| T01 | (new) | coding | `codero-cli register` or first submit | Branch enters the system |
| T02 | coding | local_review | Agent signals working tree changes ready | Pre-commit loops begin |
| T03 | local_review | coding | Either pre-commit loop fails | Agent must fix and re-submit |
| T04 | local_review | queued_cli | Both pre-commit loops pass | Agent may commit; branch enters queue |
| T05 | coding | queued_cli | `codero-cli submit` direct path | Skip pre-commit when loops not configured |
| T06 | queued_cli | cli_reviewing | Lease issued, review invoked | Dispatch picks branch from WFQ |
| T07 | cli_reviewing | queued_cli | Lease expires | `retry_count` incremented |
| T08 | cli_reviewing | reviewed | Review completes, findings delivered | Findings recorded and delivered |
| T09 | reviewed | coding | Agent picks up findings | Re-enters development cycle |
| T10 | reviewed | merge_ready | `approved=true` AND `ci_green=true` AND `pending_events=0` AND no unresolved threads | Recomputed on every watch tick |
| T11 | merge_ready | coding | New findings arrive or approval revoked | Reverts to active development |
| T12 | any active | stale_branch | HEAD hash mismatch on dispatch | Force-push detected |
| T13 | stale_branch | queued_cli | Agent re-submits with new HEAD | `retry_count` resets to 0 |
| T14 | any active | abandoned | Heartbeat TTL expires (1800s) | Operator must reactivate |
| T15 | abandoned | queued_cli | `codero-cli reactivate` | `retry_count` resets |
| T16 | any active | blocked | `retry_count >= max_retries` | Operator intervention required |
| T17 | blocked | queued_cli | Operator release via CLI/TUI/UI | `retry_count` resets |
| T18 | any | closed | PR merged, PR closed, or operator closes | Terminal state |
| T19 | queued_cli | paused | Operator pauses branch | Blocks dispatch, does not cancel active lease |
| T20 | paused | queued_cli | Operator resumes branch | Re-enters queue |

Definitions:

- **any active** = `coding`, `local_review`, `queued_cli`, `cli_reviewing`, `reviewed`, `merge_ready`
- **terminal state** = `closed`
- **operator-only transitions** = T15, T17, T18 when operator-closing, T19, T20

---

## Appendix B — Redis coordination contract (binding)

Redis is the ephemeral coordination layer in every phase. It never replaces the durable store.

| Concern | Redis primitive | Durable source or recovery rule | Notes |
|---|---|---|---|
| Lease acquire/release | `SET NX + TTL` | Rebuild lease truth from durable state plus lease audit | No double-lease on NX failure |
| WFQ dispatch queue | Sorted set (`ZADD`, `ZPOPMAX`) | Recompute all scores from durable state and repopulate | Queue order must be reproducible |
| Concurrent slot counter | `INCR` / `DECR` via Lua | Rebuild from active durable review state on restart | Never rely on in-process mutex only |
| Heartbeat TTL | `SET EX 1800` + keyspace notification | Durable state remains authoritative; expiry drives transition and system bundle | Keyspace notifications are fast path, not correctness path |
| Delivery seq number | `INCR seq:*` | Read durable seq floor on startup and continue upward | Seq is monotonic, not necessarily contiguous |
| Webhook dedup hot path | `SET NX EX 86400` on delivery ID | Durable secondary idempotency check on normalized bundle or event content | Loss of Redis dedup cannot cause durable corruption |
| Stale check trigger | keyspace notification + 30s audit fallback | Durable branch state and current HEAD verification | Polling remains fallback in all modes |
| LiteLLM circuit breaker | Redis hash | Safe to lose; missing state must not block agent path | On Redis loss during pre-commit, treat breaker as closed |
| Pre-commit rate limiting | Sorted set + Lua | Rebuild from live active pre-commit state if needed | Separate from PR review slot accounting |
| Daily pre-commit cap | `INCR` with 86400 TTL | Loss resets the cap day; acceptable only with invoice-backed thresholding and durable metrics | Cost estimate still surfaced in metrics |
| Phase 2 job queue | Asynq on Redis | Durable workflow state remains outside Redis | Redis supports queueing; it does not become source of truth |
| Phase 2 live dashboard | Pub/sub | UI can resync from durable state and `/queue` snapshot | Push is convenience, not sole truth |

Implementation constraints:

- all Redis commands and Lua scripts are centralized in `internal/redis`
- raw Redis command scattering across packages is disallowed
- every Redis key uses consistent namespacing
- every Redis-backed feature must document how it recovers after Redis restart

---

## Appendix C — Queue, delivery, webhook, and reconciliation contract (binding)

### C.1 Queue scoring

WFQ score is computed at dispatch time as:

`queue_priority + (minutes_waiting * wait_factor) - (retry_count * retry_penalty) - (active_jobs * concurrency_penalty)`

Rules:

- `queue_priority` is capped at 20
- scores are recomputed on dispatch ticks
- Phase 1 uses a single-goroutine dispatcher
- if Phase 2 introduces parallel dispatch, `ZPOPMAX` usage must be replaced or wrapped with a Lua pop-verify-lease sequence

### C.2 Lease semantics

- Lease issuance is atomic via `SET ... NX EX`
- NX failure means skip and log, never double-lease
- Lease expiry transitions `cli_reviewing -> queued_cli`, increments retry count, appends system bundle
- Lease audit goroutine runs every 30 seconds and corrects missing or inconsistent lease state

### C.3 Queue stall semantics

`queue_stalled` fires when **all** eligible queued items are exhausted or blocked by retry limits, not merely when a single pop fails.

When `queue_stalled` fires:

- dispatch halts
- an event is emitted
- operator intervention is required

### C.4 Delivery semantics

- Delivery is append-only
- replay re-appends historical bundles; it does not re-run review
- seq numbers are monotonic, not contiguous
- compaction writes durable seq floor first and Redis seq floor second

### C.5 Polling and webhook modes

Polling-only mode is the default and must remain fully functional.

Rules:

- in polling-only mode, webhook receiver is not required
- reconciliation loop runs every 60 seconds in polling-only mode
- in webhook mode, reconciliation still runs every 5 minutes as catch-up and correctness backstop
- webhook receiver must return 2XX within 10 seconds
- duplicate deliveries are dropped in Redis before JSON parse or DB read where possible

### C.6 Reconciliation rules

Reconciliation against GitHub is required in all modes.

At minimum it must detect:

- PR closed outside codero -> transition to `closed`
- approvals revoked -> recompute away from `merge_ready`
- new findings or unresolved threads -> revert `merge_ready -> coding`
- stale HEAD or force-push divergence -> `stale_branch`

### C.7 Merge readiness

`merge_ready` is true only when all four conditions hold simultaneously:

- approval present
- CI green
- pending events = 0
- zero unresolved review threads

This rule is deterministic and cannot be bypassed by summarization or model opinion.

---

## Appendix D — Pre-commit local review contract (binding)

Before any agent can commit, it must pass two sequential local review loops.

### D.1 State behavior

- branch enters `local_review` when working-tree changes are ready
- branch cannot enter `queued_cli` until both loops pass
- any loop failure sends branch back to `coding`

### D.2 Loop order

The order is fixed and permanent:

1. LiteLLM local review
2. CodeRabbit pre-commit review

They are never run in parallel. LiteLLM acts as a cheap filter before expensive CodeRabbit slot consumption.

### D.3 Working tree diff rules

The diff source is:

1. `git add -N` for untracked files
2. `git diff HEAD`

This includes staged, unstaged, and newly tracked files without requiring a temporary commit.

### D.4 Enforcement mechanism

- `codero-cli init-worktree` installs the pre-commit hook
- hook calls `codero-cli commit-gate`
- non-zero exit aborts commit
- policy without hook enforcement does not count as completion

### D.5 Loop semantics

LiteLLM loop:

- synchronous
- returns structured findings directly to the agent
- does not consume CodeRabbit slot budget
- malformed model output is discarded and logged
- if Redis is unavailable while checking circuit breaker state, treat breaker as closed and allow call timeout to govern

CodeRabbit pre-commit loop:

- runs only after LiteLLM pass
- uses separate slot accounting from PR review dispatch
- rate limited via Redis and Lua
- subject to daily hard cap until real invoice data validates cost assumptions

### D.6 Delivery of findings

- pre-commit findings are returned synchronously to the agent
- `inbox.log` is reserved for PR-level feedback, not pre-commit findings

### D.7 Initial economic guardrail

Until real invoice data is known, the daily CodeRabbit pre-commit cap defaults to 50 per day and is treated as a hard safety limit.

---

## Appendix E — Operator action semantics (binding)

These semantics must remain consistent across CLI, TUI, and later web UI.

| Action | Effect | Constraints |
|---|---|---|
| reprioritize `<0-20>` | sets `queue_priority` | takes effect next dispatch cycle, no effect on in-flight lease |
| pause | moves `queued_cli -> paused` | blocks dispatch, does not cancel in-flight lease |
| resume | moves `paused -> queued_cli` | re-enters queue |
| drain queue | halts new lease issuance | in-flight leases complete; new submissions may still be accepted |
| release | moves `blocked -> queued_cli` | resets retry count |
| reactivate | moves `abandoned -> queued_cli` | resets retry count and restores queue eligibility |
| abandon | moves active branch to `abandoned` | operator action, queue slot freed |
| close | moves branch to `closed` | terminal unless re-submitted as new work |
| replay `--since=N` | re-appends bundles from seq N | does not rerun review |
| why | shows live score breakdown | must read live queue data |
| release slot | manually frees known-dead slot | emergency use only |

Additional operator surface rules:

- advisory assistant is advisory only
- any suggested command requires explicit human confirmation
- no advisory surface may execute actions directly
- every operator action must be auditable

---

## Appendix F — Observability contract (binding)

At minimum, codero exposes these runtime surfaces:

| Endpoint | Contract |
|---|---|
| `/health` | JSON health with uptime, durable-store status, Redis status, and webhook receiver status where applicable |
| `/queue` | JSON snapshot of queue positions and live scores |
| `/metrics` | Prometheus metrics, including Redis latency, reconnects, daily cost estimate, pre-commit slot wait, and first-pass rate |
| `/api/v1/agent-metrics` | JSON effectiveness metrics per agent and project |

Additional requirements:

- every state transition increments or records metrics in real time
- Redis operation latency histogram is instrumented
- alert when Redis p99 exceeds 10ms for meaningful windows
- cost estimate is surfaced in both metrics and operator UI
- degraded mode is visible externally, not only in logs

Carry-forward SLOs for Phase 1 proving:

- zero missed feedback deliveries
- zero silent queue stalls
- zero undetected stale branches

---

## Appendix G — Failure and recovery contract (binding)

Failure behavior is a deliverable.

| Failure case | Required behavior |
|---|---|
| Redis unavailable at startup | daemon does not start; exit with named `REDIS_UNAVAILABLE`-style error |
| Redis transiently unavailable after startup | no new dispatches; in-flight work may finish; operator notified; reconnect with backoff |
| Redis restart mid-dispatch | on recovery, rebuild queue and slot state from durable store; resume automatically |
| SIGTERM | stop accepting new submissions, drain in-flight work up to configured grace period, exit cleanly |
| SIGKILL | on restart, audit durable `cli_reviewing` state against Redis lease keys and repair |
| Lease key expired | terminate hung review path, increment retry count, requeue branch, append system bundle |
| Duplicate webhook delivery | drop via Redis NX fast path when possible; secondary durable idempotency still required |
| Out-of-order webhook arrival | reconciliation and content-level idempotency preserve correctness |
| Missing keyspace notifications | safety-net audit goroutine preserves correctness |
| Redis unavailable during pre-commit circuit-breaker check | missing breaker state must not block agent; timeout governs |
| Crash between seq increment and append | harmless seq gap; pollers must tolerate missing numbers |
| Compaction | write durable seq floor first, Redis seq floor second, and recover using max of both |

Phase 1 sign-off requires explicit drills for all of the above, not only unit tests.

---

## Appendix H — Resolved decisions and deferral register

### H.1 Resolved decisions

| Decision | Resolution |
|---|---|
| Queue primitive in Go Phase 2 | Use Asynq rather than BullMQ or River |
| Managed Redis baseline | Non-cluster mode first; revisit cluster mode only when scale justifies redesign |
| CodeRabbit cost assumptions | Do not trust estimates; validate against real invoice before relaxing cap |
| Scheduler ownership | No scheduler wrapper; codero does not own agent lifecycle |
| TUI longevity | TUI remains first-class for self-hosted and personal use |
| Workflow engine upgrade | Temporal is conditional, not default |

### H.2 Deferred items

| Deferred item | Deferred to | Why |
|---|---|---|
| PostgreSQL and managed infra | Phase 2 | Local SQLite and Redis are sufficient for personal proof |
| GitHub App onboarding | Phase 2 | External installation flow is not required for personal proof |
| RBAC and immutable audit log | Phase 3 | Single-operator Phase 1 does not need role separation |
| Billing and metering | Phase 4 | Commercial work starts only after product and operations prove out |
| Direct model backend | Phase 3 or 4 | CodeRabbit plus LiteLLM wrapper is enough initially |
| Temporal | conditional revisit only | Adopt only if workflow complexity or drift makes it necessary |

### H.3 Revisit triggers

- managed Redis cluster mode only after materially higher tenant scale
- workflow engine only if Asynq cannot model required workflow cleanly or drift appears under load
- repo split only if single-repo coordination cost becomes material

---

## 9) Definition of done for the roadmap itself

This roadmap is acceptable only if:

- critical runtime behavior is explicit, not implied by reference to older docs
- phase sequencing respects the proving period, rather than compressing it away
- module intake is a standing discipline rather than a one-time migration event
- multi-repo correctness is validated during personal proof
- the pre-commit local review loop is fully specified and enforceable
- operator semantics and observability are stable contracts
- failure and recovery behavior is testable and named
- deferred items are recorded with reasons and revisit triggers

This revised v5 is meant to be the program document you actually execute, while the appendices preserve the implementation truths that v4 got right.
