# Codero Spec Implementation Checklist

Authoritative baseline: `/srv/storage/Specifications/codero`
Live spec target: `/srv/storage/local/Specifications/Codero`
Audited merged base: `origin/main` at `7e6879d550cb16a5e616f5a5b030f413cea48c20` on `2026-03-24T14:52:03Z`
Working certification branch: `feat/RC-V1-closeout` at `393c026ef4c1af9e96b277e44d9f499e897fbfce`

This checklist is the current implementation and certification audit against the
authoritative Codero spec set. Under the stricter Definition of Done, `done`
means explicit spec acceptance criteria are satisfied, the universal DoD passes,
and spec-targeted tests proving those criteria have been run.

## Status values

- `done`: explicit acceptance criteria are satisfied on merged `main`, the universal DoD passes, and the relevant spec-targeted tests have been run.
- `certification_pending`: implementation exists, but strict DoD certification and/or merge evidence is still pending.
- `partial`: important runtime pieces are merged, but explicit spec surfaces are still missing.
- `not_started`: no first-class implementation was found on the audited base.

## Spec Status Matrix

| Spec | Status | Evidence on `origin/main` | Remaining gap to close |
| --- | --- | --- | --- |
| Agent Spec v3 | certification_pending | `cmd/codero/session_bootstrap.go`, `internal/session/session.go`, `internal/state/assignment_compliance.go`, `internal/state/migrations/000008_assignment_substatus_rules.*`, `000009_assignment_lifecycle_compliance.*`, `000010_agent_rule_versions_waiting_state.*`, `internal/scheduler/expiry.go` | Merged implementation exists on `origin/main`, but the stricter DoD requires clause-to-test certification before the slice can be called `done`. |
| Task Layer v2 | certification_pending | `cmd/codero/task.go` (accept, emit, submit), `internal/state/agent_sessions.go`, `internal/state/github_links.go`, `internal/state/feedback_cache.go`, `internal/feedback/aggregator.go`, `internal/feedback/writer.go`, `internal/webhook/processor.go` (link upsert + cache invalidation), `internal/state/migrations/000011_task_layer_schema.*`, `000012_tl_v2_closeout.*`, `internal/state/lifecycle_test.go` | Closeout implementation is merged, but strict DoD recertification remains pending before the slice can be restored to `done`. |
| Daemon Spec v2 | certification_pending | `cmd/codero/main.go`, `internal/daemon/grpc/server.go`, `internal/daemon/observability.go`, `internal/webhook/reconciler.go`, `internal/scheduler/expiry.go`, `internal/config/config.go` | Daemon closeout is merged on `origin/main`, but the stricter DoD requires clause-to-test certification before the slice can be called `done`. |
| Repo-Context v1 | certification_pending | `internal/context/` (store, indexer, queries, types), `cmd/codero/context_cmd.go` (8 CLI commands), `.codero/context/graph.db` store, `docs/contracts/mi-006-repo-context.md`, `docs/module-intake-registry.md` (`MI-006`) | RC-V1 now includes the missing MI-006 intake artifacts, spec-aligned CLI JSON/error contract, dedicated contract tests, and a green strict-DoD validation pass on `feat/RC-V1-closeout`; merge to `main` is still pending. |
| Gate Config v1 | certification_pending | `internal/gate/config.go` (registry, parse, resolve, save, drift), `internal/gate/heartbeat.go` (effective config wiring), `internal/dashboard/handlers.go` (GET/PUT gate-config endpoints). | Gate Config v1 is merged, but strict DoD recertification remains pending before the slice can be restored to `done`. |

## Next Branch Candidates

1. Re-certify `Agent Spec v3` against the stricter DoD with explicit clause-to-test evidence.
2. Re-certify `Task Layer v2`, `Daemon Spec v2`, and `Gate Config v1` against the stricter DoD.
3. Merge `RC-V1` only after the certification branch results are accepted and `main` is updated.
4. After certification, return to the still-partial specs from the authoritative index (Execution Loop v1, Review Gate v1, Dashboard API v1, Session Lifecycle v1, LiteLLM Chat v1, Real-Time Views v1).
