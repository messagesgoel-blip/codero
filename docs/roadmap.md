# Codero Roadmap

Status: active
Owner: sanjay
Updated: 2026-03-28

---

## Current State

Codero is a single-operator code review orchestration control plane.
All 11 normative specs are certified and signed off (2026-03-25).
The system runs daily across two active repositories (codero, whimsy).

**Operator surfaces:** CLI commands + web dashboard (served at `/dashboard`).
The Bubble Tea terminal TUI was removed in v1.2.5 — the web dashboard is the
sole interactive operator interface. CLI commands (`gate-status`, `queue`,
`branch`, `events`, `scorecard`, `why`) remain for scripting and quick checks.

**Release:** v1.2.4 is the latest tagged release. v1.2.5 is in progress (TUI
removal + roadmap consolidation).

---

## Phase 1 — Core Runtime (In Progress)

**Goal:** Production-quality single-operator control plane running daily across 2+ repos.

### What's done

- Daemon lifecycle, crash recovery, structured logging
- 10-state / 20-transition branch state machine
- SQLite WAL durable store with migrations
- Redis coordination (leases, WFQ queue, heartbeats, dedup)
- Pre-commit gate pipeline (Copilot → LiteLLM, Semgrep deterministic blockers)
- PR review runner with CodeRabbit provider
- Webhook receiver, dedup, and reconciliation loop
- Session registration, heartbeat, assignment attachment
- Delivery pipeline (submit-to-merge FSM)
- Web dashboard (overview, settings, live activity, chat, session drill-down, archives)
- CLI: submit, heartbeat, poll, why, commit-gate, gate-status, gate-check, queue, branch, events, scorecard
- Auto-merge (opt-in squash merge on `merge_ready`)
- Proving period metrics and scorecard
- All 11 specs certified

### What remains for Phase 1 exit

The exit gate is evidence-based, not time-based:

- [ ] 14 consecutive days of daily use without manual DB repair
- [ ] Zero missed feedback deliveries
- [ ] Zero silent queue stalls
- [ ] Minimum: 3 branches reviewed/week, 10 pre-commit reviews/project/week
- [ ] Recovery drills: Redis restart, daemon restart, SIGKILL, duplicate webhook
- [ ] Pre-commit enforcement by hook, not policy alone

---

## Phase 2 — Platformization

**Goal:** Tenant-ready infrastructure without changing proven runtime semantics.

**Entry:** Phase 1 exit gate evidence complete.

### Scope

- PostgreSQL migration with `tenant_id` on all durable tables
- Managed Redis (non-cluster baseline)
- Asynq as job primitive
- GitHub App + OAuth + tenant provisioning
- Per-tenant queue isolation, slot isolation, rate limiting
- Object-store-backed delivery streams

### Exit gate

- New org installs and receives first review without manual intervention
- Two tenants cannot affect each other's queue/slots/rates
- Managed Redis: keyspace notifications, lease expiry, recovery all verified
- Migration from Phase 1 personal env to tenant-ready deployment tested

---

## Phase 3 — Enterprise Controls

**Goal:** Inspectable, supportable, safe to run for others.

### Scope

- RBAC and immutable operator audit log
- Live queue views via Redis pub/sub
- Status checks, inline annotations, GitHub re-run triggers
- Secrets management, restore drills, backup validation
- Data residency and deletion policies
- SOC 2 readiness gap analysis

### Exit gate

- Viewer role cannot execute state-changing actions
- Audit log covers every operator action
- Restore and deletion workflows tested
- CLI and web dashboard remain semantically consistent under Phase 3 controls

---

## Phase 4 — Commercialization

**Entry:** Phase 3 complete + clear demand signal + pricing hypothesis.

- Billing, metering, plan enforcement
- Packaging, support model, onboarding workflow

---

## Deferred Items

| Item | Deferred to | Reason |
|---|---|---|
| PostgreSQL + managed infra | Phase 2 | SQLite sufficient for personal proof |
| GitHub App onboarding | Phase 2 | Not needed for personal proof |
| RBAC + audit log | Phase 3 | Single operator doesn't need role separation |
| Billing | Phase 4 | Commercial work after product proves out |
| Temporal workflow engine | Conditional | Only if Asynq can't model required workflows |
| Managed Redis cluster mode | Scale-triggered | Only after materially higher tenant scale |

---

## Binding Contracts (Appendices)

The following contracts are normative. Changes require an ADR, migration note,
compatibility impact note, regression tests, and a rollback path.

### A. Canonical State Machine

10 states, 20 transitions. See `docs/roadmaps/codero-roadmap-v5.md` Appendix A
for the full table. Key rules:

- Branch identity is `repo + branch + HEAD`, never branch alone
- `merge_ready` requires: approved AND ci_green AND pending_events=0 AND no unresolved threads
- Invalid transitions are rejected and logged
- No model output may directly cause a state transition

### B. Redis Coordination

Redis is ephemeral coordination only — never source of truth.
Every Redis value is either reconstructable from durable state or safe to lose.
All Redis commands centralized in `internal/redis`.
See Appendix B of the v5 roadmap for the full key/primitive table.

### C. Delivery and Reconciliation

- Delivery is append-only with monotonic seq numbers
- Polling-only mode is default and fully functional
- Reconciliation runs every 60s (polling) or 5m (webhook mode)
- Merge readiness is deterministic (4 conditions, no model bypass)

### D. Pre-Commit Gate

- Fixed order: Copilot → LiteLLM (Semgrep as deterministic blocker in pipeline)
- Independent per-gate timeouts
- Hook-based enforcement (`commit-gate` via pre-commit hook)
- Findings returned synchronously to agent

### E. Operator Actions

Consistent semantics across CLI and web dashboard:
reprioritize, pause, resume, drain, release, reactivate, abandon, close,
replay, why, release-slot.

### F. Observability

Required endpoints: `/health`, `/queue`, `/metrics`, `/gate`, `/api/v1/agent-metrics`.
SLOs: zero missed deliveries, zero silent queue stalls, zero undetected stale branches.

### G. Failure Recovery

Named failure cases with required behavior documented in v5 Appendix G.
Phase 1 sign-off requires drills for all cases.

---

## Historical Reference

Previous roadmap versions and completed backlog are preserved as read-only
historical documents:

- `docs/roadmaps/codero-roadmap-v5.md` — full binding contracts and appendices (reference only)
- `docs/roadmaps/v1.2.4-backlog.md` — completed UI modernization and hardening release
- `docs/adr/0006-tui-shell-architecture.md` — superseded TUI architecture decision
