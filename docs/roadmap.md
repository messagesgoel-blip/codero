# Codero Roadmap

Status: active
Owner: sanjay
Updated: 2026-04-02

---

## Roadmap Precedence

This file is the roadmap index and strategic release view.

Use `docs/roadmaps/dogfood-execution-roadmap.md` for:

- next-task selection
- implementation sequencing
- shared continuity / orchestrator updates
- determining the earliest incomplete execution slice

The previous execution roadmap (`docs/roadmaps/agent-task-execution-roadmap.md`)
is superseded as of 2026-04-03. Its task IDs (SUB-001 through FIN-001) are
replaced by the dogfood roadmap's WIRE/OCL/SUB/REV/MRG/PRV tasks.

Do not use `docs/roadmaps/*-backlog.md` or untracked local planning files as the
next-task source unless a PR explicitly promotes them into the execution
roadmap.

## Current Execution Baseline

- `origin/main` head is `9350203` (PR closeout 2026-04-02).
- Completed on `main`: `TOOL-001` through `TOOL-005`, `BND-001` through
  `BND-004`, `SET-001`, `SET-002`, `SES-001` through `SES-004`.
- Agent/setup set is complete. Session set is complete.
- **Dogfood-first initiative active (2026-04-03).** The system is running but
  not in any critical path — sessions register with empty repo/branch, zero
  gate runs are recorded, scorecard is blank.
- Next execution tasks (from dogfood roadmap):
  - Wave A: `WIRE-001` (session binding), `WIRE-002` (gate reporting),
    `WIRE-003` (PR tracking)
  - Then Waves B–G: OpenClaw adapter, findings delivery, submit pipeline,
    remote review, merge, proving period.
- See `docs/roadmaps/dogfood-execution-roadmap.md` for the full 20-task
  sequence.

## Candidate Backlog After The Current Execution Roadmap

The latest proposed post-cutover backlog items are retained here so they are no
longer hidden in local-only planning files. They are not the current next-task
source.

- Operator UX: `UX-004` Kanban board view, `UX-005` attention routing,
  `UX-006` diff-first assignment review, `UX-007` live session terminal
  drilldown, `UX-008` read-only Git inspector
- Control-plane hardening: `COD-064` feedback precedence contract, `COD-065`
  extended timeout escalation, `COD-066` durable task dependency links,
  `COD-067` successor auto-start
- Cleanup: `UI-004` TUI dead shortcuts and view contract cleanup

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
- Agent status detection: `inferred_status` column with precedence model (PR #137)
  - `codero session heartbeat --status=working|waiting_for_input|idle`
  - `codero agent hooks --install|--print` (Claude Code hook generator)
  - `codero agent list [--status] [--json] [--quiet]`
  - `codero agent next [--json|--print-id|--print-url]`
  - Idle transition in sessmetrics monitor (guarded, precedence-aware)
  - Dashboard: Agent Status column, filter chip strip, attention-first sort, stale-waiting badge
- Auto-merge (opt-in squash merge on `merge_ready`)
- Proving period metrics and scorecard
- All 11 specs certified

---

## Task Tracker — Phase 1 Exit

Tasks are trackable units with explicit IDs and definitions of done.
Status: `not_started` | `in_progress` | `blocked` | `done`

### P1-A: Dashboard API Gaps

| ID | Task | Status | Definition of Done |
|---|---|---|---|
| P1-A-001 | Wire `/api/v1/dashboard/tasks` endpoint | `done` | `GET /api/v1/dashboard/tasks` returns 200 with `{tasks: [], schema_version: "1"}`; Tasks tab shows data |
| P1-A-002 | Update `dashboard-architecture.md` with new endpoints | `done` | Doc lists all current endpoints including `/tracking-config`, `/node-repos`, `/tasks`, `/agents`, `/scorecard` |
| P1-A-003 | Add env_vars validation tests | `done` | Test cases for: empty key, key with `=`, NUL bytes, valid key; all pass in `go test ./internal/dashboard/...` |

### P1-B: Agent Discovery & Setup

| ID | Task | Status | Definition of Done |
|---|---|---|---|
| P1-B-001 | Install agent shims for active agents | `done` | `codero agent list --json` returns ≥1 agent with `installed: true` |
| P1-B-002 | Verify agent heartbeat flow | `done` | Running a test session creates heartbeat entries; dashboard shows session in Agents tab |
| P1-B-003 | Document agent setup workflow | `done` | `docs/agent-setup.md` exists with step-by-step for Claude Code, Aider, Cursor |

### P1-C: Proving Period Evidence

| ID | Task | Status | Definition of Done |
|---|---|---|---|
| P1-C-001 | 14 consecutive days without DB repair | `not_started` | Log audit shows no manual DB intervention for 14 days; `grep -c "manual.*repair" logs/` = 0 |
| P1-C-002 | Zero missed feedback deliveries | `not_started` | Query `SELECT COUNT(*) FROM delivery_events WHERE status='missed'` returns 0 |
| P1-C-003 | Zero silent queue stalls | `not_started` | No queue item stuck >30min without heartbeat; Redis lease expiry recovery tested |
| P1-C-004 | Min 3 branches reviewed/week | `not_started` | `SELECT COUNT(DISTINCT branch) FROM review_runs WHERE started_at > now() - INTERVAL '7 days'` ≥ 3 |
| P1-C-005 | Min 10 pre-commit reviews/project/week | `not_started` | `SELECT COUNT(*) FROM precommit_reviews WHERE created_at > now() - INTERVAL '7 days'` ≥ 10 per repo |
| P1-C-006 | Scorecard shows real metrics | `not_started` | `/api/v1/dashboard/scorecard` returns non-zero counts for `branches_reviewed_7_days`, `precommit_reviews_7_days` |

### P1-D: Recovery Drills

| ID | Task | Status | Definition of Done |
|---|---|---|---|
| P1-D-001 | Redis restart drill | `not_started` | Redis killed and restarted; daemon reconnects within 30s; no data loss; documented in runbook |
| P1-D-002 | Daemon restart drill | `not_started` | `docker restart codero`; container healthy within 60s; queue state preserved |
| P1-D-003 | SIGKILL recovery drill | `not_started` | `kill -9` daemon process; container restarts; SQLite WAL replay succeeds; no corruption |
| P1-D-004 | Duplicate webhook drill | `not_started` | Send duplicate webhook; second event deduped; no duplicate queue entries |
| P1-D-005 | Document drill results | `not_started` | `docs/runbooks/recovery-drills.md` exists with dated pass/fail for each drill |

### P1-E: Pre-Commit Enforcement

| ID | Task | Status | Definition of Done |
|---|---|---|---|
| P1-E-001 | Pre-commit hook enforcement verified | `not_started` | `git commit` without passing gate fails; `git commit --no-verify` blocked by server-side hook or policy |
| P1-E-002 | Hook installation documented | `done` | `AGENTS.md` and `docs/agent-preflight.md` document hook setup |

---

## Phase 1 Exit Gate Summary

| Gate | Blocking Tasks | Status |
|---|---|---|
| 14 days without DB repair | P1-C-001 | `not_started` |
| Zero missed deliveries | P1-C-002 | `not_started` |
| Zero silent queue stalls | P1-C-003 | `not_started` |
| 3+ branches reviewed/week | P1-C-004 | `not_started` |
| 10+ pre-commit reviews/week | P1-C-005 | `not_started` |
| Recovery drills complete | P1-D-* | `not_started` |
| Pre-commit enforcement | P1-E-001 | `not_started` |

**Phase 1 Exit:** All tasks in P1-C and P1-D marked `done`.

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
