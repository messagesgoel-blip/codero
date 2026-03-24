# Codero Spec Implementation Checklist

Authoritative baseline: `/srv/storage/Specifications/codero`
Live spec target: `/srv/storage/local/Specifications/Codero`
Audited code base: `origin/main` at `2ae2557f60fff07455788ecd826e09af56f70f32` on `2026-03-24T01:05:03Z`

This checklist is the current implementation audit against the authoritative Codero spec set.
Use it alongside the specs when selecting the next branch from `main`.

## Status values

- `done`: the required core slice is materially present on `origin/main`; only later-phase deferrals remain.
- `partial`: important runtime pieces are merged, but explicit spec surfaces are still missing.
- `not_started`: no first-class implementation was found on the audited base.

## Spec Status Matrix

| Spec | Status | Evidence on `origin/main` | Remaining gap to close |
| --- | --- | --- | --- |
| Agent Spec v3 | done | `cmd/codero/session_bootstrap.go`, `internal/session/session.go`, `internal/state/assignment_compliance.go`, `internal/state/migrations/000008_assignment_substatus_rules.*`, `000009_assignment_lifecycle_compliance.*`, `000010_agent_rule_versions_waiting_state.*`, `internal/scheduler/expiry.go` | Broader HA, scale, and realtime-feedback items remain deferred by product sequencing, not blocked by missing core runtime wiring. |
| Task Layer v2 | done | `cmd/codero/task.go` (accept, emit, submit), `internal/state/agent_sessions.go`, `internal/state/github_links.go`, `internal/state/feedback_cache.go`, `internal/feedback/aggregator.go`, `internal/feedback/writer.go`, `internal/webhook/processor.go` (link upsert + cache invalidation), `internal/state/migrations/000011_task_layer_schema.*`, `000012_tl_v2_closeout.*`, `internal/state/lifecycle_test.go` | Closeout complete (2026-03-24). Live `codero_github_links` CRUD, `task_feedback_cache` with source_status, feedback aggregator with precedence/truncation, worktree file writer, `codero task submit`, webhook-driven link/cache updates, import boundary enforcement, and normative lifecycle tests are all implemented. |
| Daemon Spec v2 | partial | `cmd/codero/main.go`, `internal/webhook/reconciler.go`, `internal/scheduler/expiry.go`, `internal/config/config.go` | Startup, degraded-mode, sweeper, and API config are merged, but the audited base does not yet show an explicit gRPC surface or a daemon-v2 closeout audit. |
| Repo-Context v1 | not_started | Audit found no `codero context` command group, no `.codero/context/graph.db`, and no first-class repo-context package on `origin/main`. | Implement the additive repo-context subsystem: local graph store plus `context index/status/find/grep/symbols/deps/rdeps/impact`. |
| Gate Config v1 | done | `internal/gate/config.go` (registry, parse, resolve, save, drift), `internal/gate/heartbeat.go` (effective config wiring), `internal/dashboard/handlers.go` (GET/PUT gate-config endpoints). | Machine-global `$HOME/.codero/config.env` source of truth, 20-key env matrix, spec precedence (env > file > defaults), dashboard read/write parity, atomic saves, drift detection, backward-compatible legacy env vars. |

## Next Branch Candidates

1. ~~`TL-V2-closeout`~~: **Done** — merged via `feat/tl-v2-closeout` branch.
2. `DM-V2-closeout`: decide and implement the remaining daemon-v2 contract surface, including the documented gRPC question.
3. `RC-V1`: build the repo-context subsystem as an additive, advisory-only slice.
4. `GATE-V1`: add machine-global gate config loading and dashboard/env parity.
