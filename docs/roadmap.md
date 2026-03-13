# codero

## Implementation Roadmap v5 — Repo-First, Module-Intake Driven

Status: proposed
Owner: you
Horizon: 6-12 months

---

## 1) Why v5

v4 has strong engineering detail, but it mixes implementation tasks, product scope, and long-term commercialization in one stream.
For disciplined execution, we need:

- a clean repo bootstrap phase first
- hard entry/exit gates per phase
- a formal process for importing modules from ghwatcher one-by-one
- explicit structure decision checkpoints

v5 keeps the core architecture direction from v4 (durable store + Redis coordination + explicit state machine), but changes execution order.

---

## 2) Non-Negotiable Principles

- New repo starts clean (no bulk copy from ghwatcher).
- Every imported capability must have a contract, parity tests, and rollback path.
- Durable state is source of truth; Redis is coordination only.
- Multi-repo support is a required Phase 2 outcome, not a future wish.
- No phase is complete without operations readiness: tests, observability, runbooks.

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
- Durable storage schema + migrations.
- Queue, lease, and heartbeat coordination.
- CLI submit/heartbeat/poll/why (minimum viable contract).
- Structured logging + health + metrics.

Out of scope:

- SaaS, billing, tenant isolation, enterprise RBAC.

Exit gate:

- 14 consecutive days of personal daily use without manual DB repair.
- Zero data-loss incidents across restart/kill tests.
- Contract tests for all public CLI/API commands.
- Failure-mode tests for Redis outage, daemon restart, and lease expiry.

---

## Phase 2 — Multi-Repo and Module Intake from ghwatcher (Weeks 6-12)

Goal: achieve true multi-repo orchestration while reusing proven parts of ghwatcher deliberately.

### 2.1 Module Intake Workflow (for every module)

1. Define target contract in codero (input/output/errors).
2. Identify source module in ghwatcher.
3. Write parity tests in codero before integration.
4. Integrate as adapter or direct port (smallest change first).
5. Validate parity + load behavior + failure behavior.
6. Record decision in ADR and registry.

No module is "adopted" without all 6 steps complete.

### 2.2 Priority Intake Queue

Priority A (required for core value):

- Event lease semantics and transition safety
- Webhook ingestion + dedup path
- Relay/claim/ack/resolve event delivery model
- Session heartbeat and stale session handling

Priority B (operator leverage):

- Review routing policy engine
- Active agent relay worker model
- Overview/docs generation surfaces

Priority C (advanced):

- LLM-assisted routing
- Advanced watchdog heuristics

### 2.3 Multi-Repo Required Outcomes

- Repo registry and per-repo state isolation.
- Repo-qualified API routes and queries (`owner/repo + branch`, never `branch` alone).
- Delivery model that does not assume a single local inbox file.
- Cross-repo fairness + starvation protection validated by simulation.

Exit gate:

- At least 3 real repositories running concurrently for 30 days.
- No cross-repo state collision incidents.
- p95 event delivery latency and queue wait SLOs defined and met.

---

## Phase 3 — Product Hardening and Operator Experience (Months 4-6)

Goal: make codero operationally boring and supportable.

Scope:

- TUI + dashboard parity for queue, state, failures, and interventions.
- Runbooks for incident classes (stuck lease, webhook delay, Redis outage, queue stall).
- Backfill/reconcile jobs and consistency audits.
- Security hardening and secret handling.
- Performance profiling and capacity limits.

Exit gate:

- On-call style drills completed for top 5 failure modes.
- SLO dashboard live with alert thresholds.
- Restore/recovery runbooks tested from backup snapshots.

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

Sprint 4 (Phase 2 start):

- ingest first Priority A module (lease semantics)
- parity tests and rollout checklist

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

This v5 satisfies those conditions.
