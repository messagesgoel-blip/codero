# codero

## Implementation Roadmap — Agents Complete, Task Layer Active

Status: active
Owner: you
Horizon: 6-12 months
Focus: finish the merged Agents v3 work with Task Layer v2 before broader platformization

---

## 1) Why this roadmap

The session/compliance layer from the Agents v3 spec is now merged on `main` through PR `#85` and PR `#86`.
The next execution priority is finishing the Task Layer v2 contract, not reopening older sequencing debates.

The archived detailed v5 roadmap remains available at `docs/roadmaps/archive/codero-roadmap-v5.md` as historical context for appendices, long-range structure, and prior planning notes.

For current execution, the order is simpler:

- Agents v3: complete on `main`
- Task Layer v2: active roadmap with `TL-001` through `TL-008`
- broader platform, tenant, and hardening work: deferred until the agent/task contract is complete

---

## 2) Non-Negotiable Principles

- New repo starts clean (no bulk copy from ghwatcher).
- Every imported capability must have a contract, parity tests, and rollback path.
- Durable state is source of truth; Redis is coordination only.
- Multi-repo support is a required Phase 2 outcome, not a future wish.
- No phase is complete without operations readiness: tests, observability, runbooks.

## 2.1) Current Execution Boundary

- The Agents v3 work is treated as merged baseline, not open backlog.
- The only active execution track is Task Layer v2 (`TL-001` through `TL-008`).
- The archived v5 plan is reference material only; it does not set next-step priority.
- Module-intake work outside the Task Layer is deferred unless the Task Layer exposes a concrete missing dependency.

---

## 3) Phase Plan

## Phase 0 — Program Setup and Clean Repo Bootstrap (Week 1)

Goal: establish execution discipline before building features.

Deliverables:

- New empty `codero` repo with baseline structure, CI, and governance docs.
- Architecture baseline doc (`docs/architecture.md`) with system boundaries.
- ADR process (`docs/adr/`), roadmap ownership, and release policy.
- Module intake registry (`docs/module-intake-registry.md`).

Required controls:

- PR template with "scope / tests / risk / rollback".
- Branch strategy and protected main.
- Required CI checks (lint, unit, contract tests).

Exit gate (must all pass):

- CI green on empty scaffold commit.
- Contribution workflow documented and usable by another engineer.
- At least 3 ADRs merged:
  - language/runtime choice
  - durable store choice
  - Redis role boundaries

---

## Phase 1 — Core Runtime (Single-Operator, Single-Repo) (Weeks 2-6)

Goal: build a minimal reliable control plane that works daily.

Scope:

- Daemon lifecycle (start/stop/status, graceful shutdown, crash recovery).
- Canonical branch lifecycle state machine.
- State machine specification is carried forward unchanged from v4 Canonical State Machine table (10 states, 20 transitions).
- Durable storage schema + migrations.
- Queue, lease, and heartbeat coordination.
- CLI submit/heartbeat/poll/why (minimum viable contract).
- Structured logging + health + metrics.
- MI-001 (lease semantics) contract + parity-test preparation is completed in Phase 1 (Sprint 3 tail), before Phase 2 integration.

Out of scope:

- SaaS, billing, tenant isolation, enterprise RBAC.

Exit gate:

- 14 consecutive days of personal daily use without manual DB repair.
- Zero data-loss incidents across restart/kill tests.
- Contract tests for all public CLI/API commands.
- Failure-mode tests for Redis outage, daemon restart, and lease expiry.

---

## Phase 2 — Task Layer and GitHub Abstraction (Weeks 6-12)

Goal: implement the Task Layer v2 contract on top of the merged v3 session/compliance layer before broader platformization work.

Sequencing note: Phase 1 session startup and assignment/compliance tracking are complete through PR `#86` on `main`. Phase 2 is the second handshake: task registration, stage emits, feedback polling, handoff, and task-scoped GitHub abstraction.

### 2.1 Binding inputs

- Canonical contributor roadmap: `docs/roadmap.md`
- Binding task-layer contract: `/srv/storage/local/Specifications/Codero/Tasks/codero_task_layer_v2.docx`
- Guardrail: keep the no-bulk-copy ghwatcher rule, but do not reopen already-implemented lease/webhook/session-liveness work as fresh Phase 2 backlog.

### 2.2 Task Layer v2 roadmap

| Task ID | Scope | Spec anchors | Expected outcome |
|---|---|---|---|
| TL-001 | Atomic task acceptance | `§4.2`, `I-37` | Add durable `codero task accept` semantics with compare-and-swap assignment, same-session idempotency, and cross-session conflict handling. |
| TL-002 | Assignment-versioned stage emits | `§3`, `§4.3` | Introduce `assignment_version`, reject stale or out-of-order emits, and enforce valid substatus/state-group transitions on every emit. |
| TL-003 | Task registry and GitHub link abstraction | `§6.1`, `§6.2`, `§7` | Persist task-scoped GitHub linkage in `codero_github_links`, keep PR/issue/SHA values out of agent-facing contracts, and make task ID the authoritative resolver. |
| TL-004 | Feedback polling contract and cache | `§5`, `§6.3` | Extend `codero poll` to return normalized per-source `source_status`, bounded `context_block`, and durable task feedback cache semantics. |
| TL-005 | Handoff and recovery semantics | `§4.4`, `§9` | Implement push-by-default successor nomination, pull-based fallback, and handoff TTL recovery back to the queue. |
| TL-006 | Codero-outage buffering and replay | `§4.5` | Add bounded local emit buffering with idempotency keys and replay-on-reconnect behavior without breaking the durable task contract. |
| TL-007 | Feedback precedence and truncation rules | `§5.4`, `§5.5`, `§10` | Enforce explicit source precedence, ensure compliance blockers are never truncated out of context, and keep feedback reduction deterministic. |
| TL-008 | End-to-end pilot and regression pack | `§9`, `§10` | Cover `accept -> emit -> poll -> handoff/retry -> finalize` in integration tests and validate one live operator loop without manual DB repair. |

### 2.3 Carry-forward platform outcomes

- Repo registry and per-repo state isolation stay required.
- Repo-qualified API routes and queries remain `owner/repo + branch`, never `branch` alone.
- Delivery continues to avoid a single local inbox-file assumption.
- Task-layer feedback and handoff flows must work across multiple repositories without state collision.
- Cross-repo fairness and starvation protection should be validated after TL-001 through TL-008 land.

Removed from the active roadmap as duplicates or superseded work:

- MI-001 lease semantics and transition safety is already implemented.
- MI-002 webhook ingestion and dedup path is already implemented.
- MI-004 session heartbeat and stale-session handling is already implemented.
- MI-003 relay/claim/ack/resolve delivery work is superseded by TL-004 unless a concrete gap remains after the task-layer polling contract lands.

Exit gate:

- `codero task accept` enforces `I-37` and idempotency in durable tests.
- Stage emits reject stale `assignment_version` values and invalid substatus transitions.
- Poll responses always include per-source status, precedence-safe context, and deterministic truncation behavior.
- Handoff TTL recovery and outage replay are covered by integration tests.
- At least one live repository completes `accept -> emit -> poll -> retry/handoff -> finalize` with no manual DB repair.

---

## Phase 3 — Product Hardening and Operator Experience (Months 4-6)

Goal: make codero operationally boring and supportable.

Scope:

- Hardening and scale-readiness for existing TUI + dashboard operator surfaces.
- Runbooks for incident classes (stuck lease, webhook delay, Redis outage, queue stall).
- Backfill/reconcile jobs and consistency audits.
- Security hardening and secret handling.
- Performance profiling and capacity limits.

Exit gate:

- On-call style drills completed for top 5 failure modes.
- SLO dashboard live with alert thresholds.
- Restore/recovery runbooks tested from backup snapshots.
- Carry-forward SLO targets from v4 are met: zero missed deliveries, zero silent queue stalls, zero undetected stale branches.

---

## Phase 4 — Commercialization (Only after proven Phase 3)

Goal: convert proven core into SaaS capability safely.

Scope:

- Tenant model + isolation model
- Auth/RBAC + auditability
- Billing/metering
- Deployment model and upgrade/migration policy

Entry criteria:

- At least 3 months stable Phase 3 operation.
- Clear demand signal and pricing hypothesis.

---

## 4) Repo Structure Decision Framework

We decide structure after Phase 0 by scoring options against maintainability, testability, and migration speed.

Option A — Single service repo (recommended start):

- `cmd/` (entrypoints)
- `internal/` (domain modules)
- `pkg/` (shared contracts)
- `docs/` (ADRs, roadmap, runbooks)
- `tests/` (unit, integration, contract, simulation)

Why A first:

- fastest to enforce clean boundaries
- simplest CI and release path
- easiest module-intake control

Option B — Multi-repo split now (not recommended at start):

- daemon repo, cli repo, dashboard repo, shared contracts repo

Risk:

- slows Phase 1 and Phase 2 with coordination overhead before core behavior is stable.

Decision checkpoint:

- Choose A at Phase 0 exit.
- Re-evaluate split only after Phase 2 multi-repo success metrics are met.

---

## 5) Quality Gates by Layer

Code:

- lint + unit tests mandatory
- mutation or property tests on state transitions and lease logic
- Tooling alignment: for the current Go codebase, v4 Stage 1.5 ESLint/Prettier is superseded by Go-native gates (`gofmt`, `go vet`, `go test`) in CI.
- JS/TS linting and typecheck are mandatory for dashboard/frontend surfaces now that Phase 1E includes web UI delivery.

Contracts:

- API/CLI contract snapshots versioned
- backward compatibility checks in CI

Operations:

- health/metrics endpoints required
- chaos tests for Redis and webhook disruptions

Product:

- user-visible latency/error metrics
- documented manual override procedures

---

## 6) First 4 Execution Sprints

Sprint 1 (Phase 0):

- bootstrap repo
- ADRs + contribution + CI
- module-intake registry template
- adopt cross-repo two-pass pre-commit review gate (Mathkit-v2 pattern)

Sprint 2 (Phase 1):

- core state machine + durable schema + migrations
- daemon lifecycle and status surfaces

Sprint 3 (Phase 1):

- queue/lease/heartbeat
- CLI submit/poll/why
- crash and outage tests
- define MI-001 lease semantics contract (state transition + Redis lease-key behavior)
- build MI-001 parity-test harness before module integration

Sprint 4 (Phase 2 start):

- TL-001 atomic task acceptance and idempotent claim path
- TL-002 assignment-versioned emits and substatus-validity enforcement

### Current Implementation Snapshot (2026-03-22)

- `local_review` state transitions are implemented in `internal/state` (T02/T03/T04).
- `codero commit-gate` is implemented and wired to the shared heartbeat gate contract.
- `codero commit-gate` renders a `Copilot -> LiteLLM` heartbeat pipeline, while `scripts/review/two-pass-review.sh` still enforces Semgrep as a mandatory blocker pass.
- `/gate` observability endpoint is live for dashboard parity with CLI gate progress.
- Phase 1E web dashboard is implemented for operator parity (overview/settings/live activity/manual upload) and served at `/dashboard`.
- Proving-period metrics commands (`scorecard`, `record-event`, `record-precommit`) are implemented and `commit-gate` now auto-records provider outcomes.
- TUI v2-alpha is shipped for `codero gate-status --watch` with Bubble Tea 3-pane layout, keyboard-first controls, and authoritative/non-authoritative gate separation.
- TUI architecture and operator quickstart are documented in `docs/tui-v2-architecture.md`.
- PR `#86` is merged on `main`, closing the remaining v3 gaps with versioned `agent_rules` rows and blocked routing for `waiting_for_merge_approval`.

---

## 6.1) Task Layer v2 Near-Term Execution Backlog

This is the next active execution track after the merged v3 closeout. Use TL-001 through TL-008 as the task IDs for planning, issue creation, and sequencing.

- TL-001: atomic task acceptance
- TL-002: assignment-versioned stage emits
- TL-003: task registry and GitHub link abstraction
- TL-004: feedback polling contract and cache
- TL-005: handoff and recovery semantics
- TL-006: Codero-outage buffering and replay
- TL-007: feedback precedence and truncation enforcement
- TL-008: end-to-end pilot and regression pack

---

## 6.2) Deferred Post-v3 Hardening Backlog

These items are explicitly deferred for later implementation. They are not blockers for v3 session/compliance acceptance or the current PR `#86` closeout path.

- High availability / failover: revisit standby-region or multi-region failover only after the current single-region durable-store model reaches measured operational limits. This likely requires a durable-store redesign, not a small patch.
- Scalability under sustained load: plan backend horizontalization and possible sharding/priority separation for high-volume feedback aggregation and compliance workloads once single-writer SQLite throughput becomes a measured constraint.
- Feedback precedence / conflict resolution: publish an explicit source-priority contract for conflicting signals (hard compliance, merge readiness / CI, human review, operator annotations) and define whether any audited override path should exist for non-hard signals.
- Real-time feedback transport: evaluate SSE or WebSocket push delivery if polling and webhook catch-up become a measured latency bottleneck for operators or agents.
- Extended timeout escalation: build on the existing lost/stuck/TTL detection with abnormal-duration alerts, reassignment, and escalation policies for assignments that remain stalled too long without progress.
- Observability expansion: add queue saturation, reconciliation lag, rule-check latency, and operator troubleshooting views before attempting major scale-out or failover work.

---

## 7) Risks and Mitigations

Risk: hidden coupling when importing ghwatcher modules.
Mitigation: contract-first adapters + parity tests before integration.

Risk: Redis assumptions leak into durability model.
Mitigation: reconstructability tests from durable store after Redis wipe.

Risk: roadmap bloat slows delivery.
Mitigation: phase exit gates and strict out-of-scope list per phase.

Risk: premature SaaS work.
Mitigation: hard entry criteria before Phase 4.

---

## 8) Definition of Done for "Proper Roadmap"

A roadmap is accepted only if:

- each phase has measurable entry and exit gates
- each imported module has an intake contract and test gate
- repo structure decision point is explicit
- next 2-4 sprints are immediately executable

This roadmap satisfies those conditions.

---

## 9) Release Tracking (v1.2.x)

### v1.2.2 — Surface Parity Harness (COD-050, PR #54)
**Status:** ✅ Merged + promoted  
**Commit:** `6f6be45734f07438d1bd2d7bb9fc23a8573df379`

- `gate-check --tui-snapshot`: deterministic headless TUI surface
- `dashboard --serve-fixture`: local dashboard fixture server
- Surface parity contract tests: CLI / JSON / TUI / dashboard all verified identical

**Pilot rerun batch 2 results (2026-03-18):**

| Pilot | Verdict |
|---|---|
| P1 Contract Rerun | ✅ PASS |
| P2 Hardblock Rerun | ✅ PASS |
| P3 Surface Parity Rerun | ✅ PASS |
| P4 Strict Policy Rerun | ✅ PASS |
| P5 Surface Parity (dashboard+TUI) | ✅ PASS |

Evidence: pilot rerun batch 2 evidence directory (local CI run artifacts, not tracked in repo)

---

### v1.2.3 — Functional Hardening / Non-UI Release (COD-052)
**Status:** Released (tag: v1.2.3)  
**Scope:** Non-UI functional release only

**Included:**
- BUG-001 fix: forbidden-paths disabled reason message distinguishes missing enforce flag vs missing regex
- Pilot rerun batch 2 validation (all 5 pilots pass)
- Release notes: `docs/runbooks/v1.2.3-release-notes.md`
- Env contract doc update: two-var requirement for forbidden-paths

**Not included (deferred to v1.2.4):**
- TUI visual design refresh
- Dashboard UI component refresh

---

### v1.2.4 — UI Modernization + Feedback Loop Hardening
**Status:** In progress
**Scope:** TUI/dashboard visual refresh + infra/docs/log/session clarity; feedback-loop hardening shipped
**Spec:** `docs/roadmaps/v1.2.4-backlog.md`

Key items:
- ✅ COD-NEW-A: Post-push CI watcher (`ci-watch.sh`) — completed (PR #60)
- ✅ COD-NEW-B: Pre-push test gate (`pre-push` hook) — completed (PR #60)
- ✅ COD-NEW-C: Autonomous finish-loop (`codero-finish.sh`) — completed (PR #62)
- COD-055: Finish-loop stabilization follow-up (`codero-finish.sh`) — in progress (PR #63 open)
- UI-001: TUI layout and visual design modernization (Bubble Tea)
- UI-002: Dashboard UI component refresh
- UI-003: Current activity session panel for the GUI
- INFRA-001: Clarify `/gate` vs `/gate-check` endpoint naming in docs
- LOG-001: Define structured heartbeat/log states for the full pre-commit step matrix via `gate-checks` (in progress — COD-057)
- DOC-001: Gate-check activation guide

**Active implementation direction (2026-03-20):**
- TUI is the current UI focus for v1.2.4; dashboard follow-up should preserve parity but is not the lead surface for the next slice.
- Preserve the current Codero strengths: review assistant shell, findings/routing pane, pipeline pane, and event stream/architecture pane.
- Rework the TUI shell around:
  - a compact operator top strip
  - shared panel/metric primitives
  - a true mode-aware bottom action bar
  - stronger selected-row and active-pane treatment
  - centered help/detail overlays where a persistent pane would add clutter
- Current UI-001 gap audit should drive the first implementation slice:
  - fix SSE / `DeliveryEvent` flow and log rendering first
  - add the persistent merge-status footer from `BlockReason`
  - bind live agent row state and findings detail data
  - add the missing findings/history/detail affordances before extra polish
- Borrow implementation ideas from existing terminal UIs rather than inventing new interaction patterns:
  - `gh-dash`: Bubble Tea workflow ergonomics, selected-row treatment, action hints
  - `campfire`: realtime event/log presentation
  - `kue`: overview/detail/modals and contextual help flow
  - `lazygcs`: search/preview navigation patterns
  - `k9s` / `lazydocker`: dense operator framing and split-pane discipline
  - `Helius`: visual density, metric cards, and status/command bar ideas only; do not copy code
- Dashboard follow-up should align terminology and hierarchy with the final TUI shell and may borrow density/quick-action ideas from Cacheflow dashboard polish work.
