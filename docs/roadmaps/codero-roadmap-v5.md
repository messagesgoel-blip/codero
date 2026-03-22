# codero

## Implementation Roadmap v5 — Repo-First, Contract-Bound, Module-Intake Driven

Status: proposed  
Owner: you  
Horizon: 6-12 months  
Revision intent: preserve the cleaner program structure of v5 while restoring the implementation contracts, failure semantics, and operator model that v4 captured well.
Near-term execution after the merged v3 closeout now lives in `../roadmap.md`; treat this file as architecture and historical sequencing context unless a PR explicitly updates it.

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
- Operator actions must have identical semantics across CLI, TUI, and web UI.
- Multi-repo correctness is a Phase 1 proving requirement. Multi-tenant isolation is Phase 2.
- No phase is complete without tests, observability, runbooks, and recovery drills.
- The roadmap does not own agent process launch or graceful shutdown. codero does own session registration, assignment attachment, heartbeat tracking, and reconciliation once a session exists.

### 2.1 Product thesis and anti-goals

Product thesis by phase:

- Phase 1 primary user: a single operator running multiple active repos daily.
- Phase 2 primary user: small teams needing predictable review orchestration and auditability across repos.
- Phase 3+ primary user: platform and security-conscious teams requiring policy, controls, and trust boundaries.

codero must outperform "GitHub + scripts + good prompts" on:

- deterministic lifecycle semantics and replayable state
- policy-driven merge readiness and auditable overrides
- resilient degraded operation and reconciliation after failures
- measurable operator time reduction across repeated review loops

Anti-goals:

- not a general-purpose agent scheduler
- not a CI replacement
- not a code-hosting abstraction layer
- not a workflow engine unless clear complexity evidence requires it

---

## 3) System boundaries and responsibility model

### 3.1 What codero owns

codero owns:

- branch registration and submission
- agent session registration, liveness tracking, and assignment attachment
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
- guaranteeing graceful termination when a human closes a window
- creating worktrees unless the operator explicitly runs the setup command
- external scheduler behavior
- direct merge decisions outside the deterministic state rules

There is no scheduler wrapper. Agent work is triggered manually or by external orchestration such as cron, launchd, CI, or another controller.

### 3.3 Full automation loop

The intended automation loop is:

operator or external orchestration starts agent -> agent works in isolated worktree -> agent requests pre-commit review -> shared gate-heartbeat runs Copilot then LiteLLM (with Semgrep as deterministic blocker in the same pipeline) -> pre-commit hook allows commit on PASS -> agent submits branch -> codero queues and dispatches PR review -> findings are delivered -> operator or orchestration re-triggers agent with findings -> agent fixes and re-submits -> codero confirms approval, CI green, zero pending events, and no unresolved review threads -> branch becomes merge_ready -> repo auto-merge completes the cycle.

### 3.4 Session registration and tracking

codero treats session registration as a minimal identity handshake, not a task claim.

- `session.register` emits `session_id` and `agent_id` only.
- `session.heartbeat` updates liveness and may use Redis TTL as the fast path, with durable sync for audit and recovery.
- `task.claim` or `task.assign` attaches `repo`, `branch`, `worktree`, and optional `task_id`.
- a session may receive multiple assignments over time, but only one active assignment is allowed at once.
- if a window closes or the agent loses context, Codero marks the session expired or lost when heartbeats stop.
- missing webhooks or stale `waiting_*` states are repaired by reconciliation, not by trusting the agent to re-signal.
- assignment creation and compliance check creation must be atomic.

### 3.5 Data layer by phase

| Phase | Layer | Role | Owns |
|---|---|---|---|
| Phase 1 | SQLite (WAL) | Durable source of truth | Branch state records, feedback bundles, transition audit log, migration history, effectiveness records |
| Phase 1 | Redis (local) | Ephemeral coordination | Lease TTLs, WFQ queue, slot counters, heartbeat keys, seq counter, webhook dedup keys, circuit breaker state, pre-commit slots, daily caps |
| Phase 1 | inbox.log or repo-qualified delivery stream | Append-only delivery | Agent-facing feedback delivery with monotonic seq IDs |
| Phase 2+ | PostgreSQL | Durable source of truth | Multi-tenant state, metering, audit log, effectiveness records |
| Phase 2+ | Managed Redis (non-cluster baseline) | Coordination and job primitives | Phase 1 coordination roles plus Asynq queues, per-tenant rate limits, pub/sub |
| Phase 2+ | Object-store backed delivery streams | Append-only delivery | Per-tenant or per-repo delivery streams, with seq semantics preserved |

### 3.6 System invariants

- Branch identity is **repo + branch + HEAD**, never branch name alone.
- `merge_ready` is computed deterministically, never inferred by model output.
- Seq numbers are monotonic, but they do not need to be contiguous.
- Every invalid state transition is rejected and logged.
- Durable store rebuild after Redis loss is a supported path, not an edge case.
- Every manual operator intervention must leave an auditable trace.

### 3.6 Canonical domain model

Core entities and relationships:

- **tenant**: top-level ownership and policy scope (single operator in Phase 1, multi-tenant in Phase 2+).
- **repo**: code host repository under a tenant; owns repo-level policy and onboarding state.
- **branch instance**: repo-qualified branch identity plus HEAD and lifecycle state.
- **PR**: external code host review artifact linked to branch instance.
- **review run**: one execution attempt against a branch instance or PR, with backend, timing, and outcome.
- **lease**: exclusive dispatch claim for a branch instance with TTL and heartbeat semantics.
- **feedback bundle**: normalized review findings and system events delivered to agents/operators with seq ordering.
- **agent session**: external actor context for a live agent instance; may be unattached until a task is claimed or assigned, then linked to one active task branch at a time.
- **operator action**: explicit human control action with actor, reason, and audit trace.
- **policy set**: hierarchical configuration for queue, review, gating, and merge semantics.
- **suppression/waiver**: time-scoped acceptance of noisy or intentionally accepted findings, with owner and expiry.

Model constraints:

- branch instance is the unit of queueing, leasing, review, and merge readiness
- review runs and feedback bundles are append-only records
- policy sets are resolved deterministically by hierarchy at decision time
- suppressions never delete findings; they alter gating/visibility outcomes with audit trace

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
- Shared gate-heartbeat pipeline: Copilot then LiteLLM, with Semgrep deterministic blocking checks.
- Hook-based commit enforcement.
- Independent per-gate timeout budgets and shared heartbeat progress reporting.

#### 1E. Operator surfaces and observability

- CLI submit, heartbeat, poll, why, reactivate, init-worktree, commit-gate.
- TUI queue, branch detail, event log, and effectiveness views.
- Web dashboard with parity to core TUI operator actions (overview, settings, live activity, manual upload flow).
- Health, queue, metrics, and agent-metrics endpoints.
- `/dashboard` and `/gate` semantics remain contract-aligned with TUI/CLI gate state rendering.

#### 1F. Hardening and readiness verification

- Failure-mode audit.
- End-to-end integration cycle.
- Redis loss and restart drills.
- Webhook replay tests.
- Unresolved review thread cross-check tests.
- readiness sign-off with explicit activity thresholds and recovery evidence.

#### 1G. Agent session registration and task attachment

- Minimal `session.register` handshake: emit `session_id` and `agent_id` only.
- Durable `agent_sessions` row with `started_at`, `last_seen_at`, and presence state.
- `session.heartbeat` as the liveness path, with Redis TTL as the fast presence layer and durable sync for audit/recovery.
- `task.claim` and `task.assign` attach `repo`, `branch`, `worktree`, and optional `task_id`.
- Reconciliation for missed webhooks and stale `waiting_*` assignment states.
- Atomic creation of assignments and their compliance checks.
- Dashboard/TUI session views that show active, waiting, blocked, expired, and lost states without requiring graceful shutdown.

### Out of scope

- Tenant billing and metering
- Enterprise RBAC
- GitHub App onboarding for external customers
- Plan enforcement and formal SaaS packaging

### Exit gate

All of the following must be true:

- Daily use is active across at least two active repositories, with scorecard evidence captured during hardening.
- Minimum operating thresholds achieved: 3 branches reviewed per week, 2 stale detections observed, 1 lease-expiry recovery observed, and 10 pre-commit reviews per project per week.
- Zero manual DB repair incidents.
- Zero missed feedback deliveries.
- Zero silent queue stalls.
- Zero undetected stale branches.
- Agent session registration, heartbeat expiry, and assignment attachment are exercised end to end in at least one live pilot.
- Recovery tests pass for Redis restart, daemon restart, SIGKILL aftermath, and duplicate webhook delivery.
- Pre-commit loops are enforced by hook, not policy alone.

### Phase gate to Phase 2

Phase 2 may begin immediately after Phase 1 implementation completes, once the Phase 1 exit gate evidence is complete.

Default gate:

- all Phase 1 exit criteria are satisfied and documented
- release owner approves progression based on evidence, without a fixed calendar wait

The gate is evidence-based rather than time-based.

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

- RBAC and immutable operator audit log.
- Live queue views via Redis pub/sub.
- Status checks, inline annotations, and GitHub re-run triggers.
- Secrets management, restore drills, and backup validation.
- Stateless processing audit for code content.
- Data residency and deletion policies.
- SOC 2 readiness gap analysis.

### Exit gate

- Existing web UI/operator surfaces remain semantically consistent with CLI/TUI under Phase 3 controls.
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

### Historical initial priority intake queue

The original MI-001 through MI-004 queue is no longer the active next-step plan:

- MI-001 lease semantics and transition safety is already implemented.
- MI-002 webhook ingestion and dedup is already implemented.
- MI-004 session heartbeat and stale-session handling is already implemented.
- MI-003 relay / claim / ack / resolve delivery work is superseded by the Task Layer v2 feedback and polling roadmap unless a concrete gap remains after that work lands.

Current near-term execution is tracked in `../roadmap.md` as TL-001 through TL-008.

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

## 8) Product and operating doctrine

### 8.1 Policy hierarchy and configuration model

Policy precedence (highest to lowest):

1. Temporary operator override (time-scoped, auditable)
2. PR or branch exception
3. Repo policy
4. Tenant policy
5. Global defaults

Policy domains:

- severity thresholds and merge blockers
- WFQ coefficients and queue aging behavior
- pre-commit slot caps and daily cost caps
- allowed review backends and fallback order
- auto-merge eligibility rules
- polling versus webhook operating mode
- review scope rules (changed files, full scan, retry behavior)
- suppression eligibility and approval requirements

### 8.2 Suppression and waiver model

Suppressions/waivers are explicit policy objects with:

- scope: line, file, rule, repo, or backend
- reason and expected risk
- owner (human) and optional proposing agent
- expiry timestamp (required unless explicitly permanent and approved)
- audit trail: created, modified, approved, expired, revoked

Behavior:

- suppressions can hide repeated noise in operator views
- gating impact is policy-controlled (visibility-only vs merge-impacting)
- agents may propose suppressions; only approved human/operator roles may activate merge-affecting waivers
- expired suppressions automatically revert to normal finding behavior

### 8.3 Repo onboarding and lifecycle model

Repo lifecycle states:

- proposed -> onboarded -> validating -> active -> paused -> archived

Onboarding checks:

- required token scopes verified
- branch protection compatibility verified
- webhook configured or polling fallback confirmed
- repo policy loaded and resolved
- reconciliation drill executed successfully

### 8.4 Degraded operating modes

Named operating modes:

- healthy
- degraded: Redis unavailable
- degraded: GitHub unavailable
- degraded: Copilot unavailable
- degraded: LiteLLM unavailable
- degraded: webhook unavailable (polling fallback)
- read-only/drain
- recovery/rebuild

Each mode must define:

- allowed operations
- blocked operations
- operator-visible status and guidance
- safe fallback workflow

### 8.5 Project kill and revisit criteria

Program-level criteria (reviewed at phase gates):

- if operator intervention rate does not materially improve over baseline
- if false-positive burden remains above agreed threshold after stabilization window
- if codero does not outperform a simpler script-based workflow in day-to-day use
- if support/ops burden outweighs demonstrated product value

---

## 9) First six execution sprints

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

### Sprint 5 ✓ (feat/COD-014-sprint5-review-delivery-reconcile)

- PR review runner: `internal/runner` — consumes queued_cli, acquires lease, executes provider, normalizes findings, transitions state deterministically
- finding normalizer: `internal/normalizer` — canonical schema (severity, category, file, line, message, source, timestamp, rule_id); stable for same input
- append-only delivery and replay: `internal/delivery` — Redis INCR seq + SQLite durable floor; replay API; InitSeqFloor on restart
- webhook receiver, dedup, reconciliation loop: `internal/webhook` — HMAC-SHA256 sig verify; Redis NX + durable DB dedup; reconciler polls every 60s (polling-only) or 5m (webhook mode)
- heartbeat expiry and abandoned-state handling: `internal/scheduler/expiry.go` — session TTL (1800s) → abandoned (T14); lease audit goroutine (30s) → re-queue/block (T07/T16)
- polling-only mode default: daemon boots and operates fully with `webhook.enabled: false`; reconciler remains correctness backstop in all modes
- new DB migration: `000002_sprint5_delivery.up.sql` — delivery_events, webhook_deliveries, review_runs, findings tables
- new contracts: `docs/contracts/review-finding-schema.md`, `docs/contracts/delivery-replay-contract.md`
- new runbook: `docs/runbooks/webhook-outage-polling-fallback.md`

### Sprint 6

- `local_review` activation (`coding -> local_review -> queued_cli`) - implemented
- shared pre-commit gate wiring via `commit-gate` + heartbeat progress (`Copilot -> LiteLLM`) - implemented
- pre-commit hook enforcement using shared toolkit hooks and gate-heartbeat - implemented
- TUI/operator surface parity for gate progress and interventions - **implemented**
  - `codero gate-status` shows run status, active gate, progress bar (identical icons to `commit-gate`), and blocker comments
  - `--watch` flag now runs TUI v2-alpha (Bubble Tea 3-pane layout with adaptive split and keyboard-first navigation)
  - `--logs` flag to inspect gate log directory
  - interactive prompt for retry/logs/branch-view actions
  - `/gate` endpoint and TUI share the same `progress.env` source and `gate.RenderBar()` call — verified by unit tests
  - TUI v2 architecture and operator quickstart documented in `docs/tui-v2-architecture.md`
- effectiveness metrics baseline (`scorecard`, `record-event`, `record-precommit`) - implemented; automatic gate-to-metric writes - **implemented**
  - `commit-gate` auto-records per-provider (`copilot`, `litellm`) outcomes on every terminal result
  - idempotent writes (`INSERT OR IGNORE`) using `run_id + provider` as deterministic ID
  - manual `record-precommit` no longer required in normal flow
- hardening matrix closure and first end-to-end test execution evidence - **complete**
  - all 8 failure-mode scenarios verified (FR-001 through FR-008 in failure-recovery-matrix.md)
  - TestSprint6_E2E_Lifecycle documents the full T02→T04→T06→T08→T10 lifecycle path
  - proving-evidence-2026-03.md updated with complete evidence log

After Sprint 6, the work shifts from implementation breadth to proving behavior under daily use.

### Sprint 7 (COD-030) — Close the review loop

**Goal:** Replace every stub integration point with real implementations so the automation loop (agent → commit → codero → CodeRabbit → agent) completes end-to-end without manual steps.

Four gaps closed in this sprint:

#### 7A. Real GitHub client (`internal/github`)

- New package `internal/github` with `Client` implementing `webhook.GitHubClient`.
- Methods: `GetPRState`, `FindOpenPR`, `ListPRReviews`, `ListPRReviewComments`, `ListCheckRuns`.
- Same `net/http` + Bearer token pattern as `internal/config/scopes.go` — no SDK dependency.
- Replaces `webhook.StubGitHubClient{}` in `daemonCmd`.

#### 7B. Real CodeRabbit review provider (`internal/runner`)

- New `GitHubProvider` in `internal/runner/github_provider.go` implementing `runner.Provider`.
- Fetches CodeRabbit inline review comments and review-body summaries from the open PR.
- Maps comment bodies to `normalizer.RawFinding` via heuristic severity inference.
- Replaces `runner.NewStubProvider(0)` in `daemonCmd`.

#### 7C. Real webhook event processor (`internal/webhook`)

- New `EventProcessor` in `internal/webhook/processor.go` implementing `webhook.Processor`.
- Handles `pull_request` (closed → T18, synchronize → T12), `pull_request_review` (approved/changes_requested), and `check_run` (completed) events.
- Updates merge-readiness fields and appends system events to the delivery stream.
- Replaces `&webhook.NopProcessor{}` in `daemonCmd`.

#### 7D. `codero poll` and `codero why` CLI commands (`cmd/codero`)

- `codero poll [--repo] [--branch]`: on-demand GitHub reconciliation for one branch.
  Forces the same cycle the background reconciler runs every 60 s, outputs what changed.
- `codero why [--repo] [--branch] [--limit N] [--json]`: explains current branch state.
  Shows merge-readiness fields, latest findings, and recent delivery events.
- Also adds `RunOnce(ctx)` to `Reconciler` (used by `poll` and tests).

#### 7E. Auto-merge (`internal/webhook`, `internal/github`, `internal/config`)

- Closes the final step of the automation loop: "branch becomes `merge_ready` → repo auto-merge completes the cycle" (roadmap 3.3).
- New `AutoMerger` interface in `internal/webhook`: `MergePR(ctx, repo string, prNumber int, sha, mergeMethod string) error`.
- `github.Client.MergePR` implements `AutoMerger` via `PUT /repos/{repo}/pulls/{number}/merge`.
- `webhook.GitHubState` gains `PRNumber int` so the reconciler has the PR number without an extra API call.
- `Reconciler.WithAutoMerge(merger, method)` enables auto-merge (functional option, existing `NewReconciler` signature unchanged).
- On `→ merge_ready` (T10) and on each subsequent reconcile of a branch already in `merge_ready`, the reconciler calls `MergePR`. On success: `merge_ready → closed` (T18). On failure (conflict, SHA mismatch): error is logged and branch stays in `merge_ready` so the next cycle retries.
- `Config.AutoMerge` (`auto_merge.enabled`, `auto_merge.method`) with env overrides `CODERO_AUTO_MERGE_ENABLED` and `CODERO_AUTO_MERGE_METHOD`. Default: disabled; method defaults to `squash`. Invalid methods are rejected at startup.

**Invariants preserved:**
- All state transitions still go through `state.TransitionBranch` — no raw SQL in new code.
- Polling-only mode remains fully functional; real GitHub client is used by the reconciler in both modes.
- Webhook processor uses the same delivery stream and state DB as the runner.
- Auto-merge is opt-in (default disabled); setting `auto_merge.enabled: false` keeps the previous behaviour exactly.

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
| WFQ dispatch queue | Sorted set (`ZADD`, `ZPOPMIN`) | Recompute all scores from durable state and repopulate | Queue order must be reproducible |
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

`queue_priority - (minutes_waiting * wait_factor) + (retry_count * retry_penalty) + (active_jobs * concurrency_penalty)`

Rules:

- `queue_priority` is capped at 20
- scores are recomputed on dispatch ticks
- Phase 1 uses a single-goroutine dispatcher
- if Phase 2 introduces parallel dispatch, `ZPOPMIN` usage must be replaced or wrapped with a Lua pop-verify-lease sequence

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

Before any agent can commit, it must pass the shared pre-commit gate pipeline.

### D.1 State behavior

- branch enters `local_review` when working-tree changes are ready
- branch cannot enter `queued_cli` until both loops pass
- any loop failure sends branch back to `coding`

### D.2 Loop order

The order is fixed and permanent in the heartbeat contract:

1. Copilot gate
2. LiteLLM gate

Semgrep deterministic checks run in the same shared gate pipeline as hard blockers.
The AI gates are never run in parallel.

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

Copilot gate:

- synchronous
- returns structured findings directly to the agent
- malformed model output is discarded and logged
- if Redis is unavailable while checking circuit breaker state, treat breaker as closed and allow call timeout to govern

LiteLLM gate:

- runs only after Copilot gate
- uses an independent timeout budget from Copilot
- emits progress status consumed by both CLI and `/gate` endpoint

### D.6 Delivery of findings

- pre-commit findings are returned synchronously to the agent
- `inbox.log` is reserved for PR-level feedback, not pre-commit findings

### D.7 Initial guardrails

Use independent per-gate timeout budgets and an overall gate wall-clock budget from the heartbeat contract.
Cost and quota guardrails remain configurable, but progression is blocked strictly by deterministic/AI gate status.

---

## Appendix E — Operator action semantics (binding)

These semantics must remain consistent across CLI, TUI, and web UI.

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
| Pre-commit gate baseline | Use shared heartbeat contract (`Copilot -> LiteLLM`) with deterministic blockers in the same pipeline |
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
| Additional local review providers | Phase 3 or 4 | Shared heartbeat baseline is sufficient for Phase 1/2 execution |
| Temporal | conditional revisit only | Adopt only if workflow complexity or drift makes it necessary |

### H.3 Revisit triggers

- managed Redis cluster mode only after materially higher tenant scale
- workflow engine only if Asynq cannot model required workflow cleanly or drift appears under load
- repo split only if single-repo coordination cost becomes material

---

## Appendix I — Security and trust boundaries (binding)

Trust-boundary rules apply in every phase:

- raw source content may flow only to explicitly allowed review backends and configured model endpoints
- raw source content must not be durably persisted outside approved durable stores
- secrets, tokens, credentials, and provider auth artifacts must never be emitted in findings, logs, or metrics
- operational logs and metrics contain metadata and counters, not full code payloads
- all external backend usage must be policy-gated and auditable by backend and model

Storage and handling requirements:

- durable stores persist normalized findings, transition records, and policy decisions
- ephemeral coordination layers may cache operational metadata but not become sole source of truth
- key material is stored in designated secret stores or environment injection paths with rotation support
- "stateless processing" means request/response review content is not retained by codero durable stores unless explicitly required for replay contracts

Phase implications:

- Phase 1: local-only defaults and explicit backend allowlist
- Phase 2+: tenant isolation and policy-enforced backend allowlists are mandatory
- Phase 3+: immutable audit and enterprise retention controls are mandatory

---

## Appendix J — Semantic change policy (binding)

Changes to binding semantics require elevated change control.

High-impact semantic surfaces:

- canonical state machine transitions
- merge-ready computation rules
- operator action semantics
- delivery/seq semantics
- Redis recovery and rebuild rules
- pre-commit loop ordering and gating semantics

Required artifacts for semantic changes:

- ADR documenting rationale and alternatives
- migration note describing data and behavior impact
- compatibility impact note (forward/backward)
- regression tests covering old and new behavior
- rollback path with operator-safe fallback

No semantic change is "minor docs-only" if it affects runtime decisions.

---

## Appendix K — Product and economic scorecard (mandatory)

Operational correctness is necessary but insufficient. codero must also demonstrate product value.

Track at minimum:

- operator minutes saved per reviewed branch
- first-pass clean rate trend
- PR review turnaround improvement
- false-positive rate by backend
- manual interventions per 100 branches
- stale-branch catch rate
- percent of branches reaching `merge_ready` without human rescue
- cost per successful reviewed branch

Use scorecard trends at phase gates:

- promote when value and reliability both improve
- hold when reliability improves but value does not
- reconsider scope when value and reliability both stall

---

## 10) Definition of done for the roadmap itself

This roadmap is acceptable only if:

- critical runtime behavior is explicit, not implied by reference to older docs
- phase sequencing is evidence-based and avoids fixed calendar delays
- module intake is a standing discipline rather than a one-time migration event
- multi-repo correctness is validated during personal proof
- the pre-commit local review loop is fully specified and enforceable
- operator semantics and observability are stable contracts
- failure and recovery behavior is testable and named
- deferred items are recorded with reasons and revisit triggers

This revised v5 is meant to be the program document you actually execute, while the appendices preserve the implementation truths that v4 got right.
