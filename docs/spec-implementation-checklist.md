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
| Daemon Spec v2 | done | `cmd/codero/main.go`, `internal/webhook/reconciler.go`, `internal/scheduler/expiry.go`, `internal/config/config.go`, `proto/codero/daemon/v1/daemon.proto`, `internal/daemon/grpc/server.go`, `internal/daemon/grpc/sessions.go`, `internal/daemon/grpc/tasks.go`, `internal/daemon/grpc/assignments.go`, `internal/daemon/grpc/feedback.go`, `internal/daemon/grpc/gate.go`, `internal/daemon/grpc/health.go`, `internal/daemon/observability.go` | Startup, degraded-mode, sweeper, API config, and gRPC daemon surface are implemented. The gRPC surface (SessionService, TaskService, AssignmentService, FeedbackService, GateService, HealthService) is served via h2c on the API port with a readiness interceptor matching spec §3. |
| Repo-Context v1 | not_started | Audit found no `codero context` command group, no `.codero/context/graph.db`, and no first-class repo-context package on `origin/main`. | Implement the additive repo-context subsystem: local graph store plus `context index/status/find/grep/symbols/deps/rdeps/impact`. |
| Gate Config v1 | partial | `internal/gate/heartbeat.go` currently wires `CODERO_COPILOT_ENABLED` and related gate env propagation. | Machine-global `$HOME/.codero/config.env`, dashboard-backed edits, and the broader gate-config env matrix from the spec are still missing. |

## Next Branch Candidates

1. ~~`TL-V2-closeout`~~: **Done** — merged via `feat/tl-v2-closeout` branch.
2. ~~`DM-V2-closeout`~~: **Done** — merged via `feat/DM-V2-closeout` branch. gRPC daemon surface (6 services), h2c multiplexing, readiness gate, and tests.
3. `RC-V1`: build the repo-context subsystem as an additive, advisory-only slice.
4. `GATE-V1`: add machine-global gate config loading and dashboard/env parity.
