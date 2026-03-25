# Codero Spec Implementation Checklist

Authoritative baseline: `/srv/storage/Specifications/codero`
Live spec target: `/srv/storage/local/Specifications/Codero`
Audited merged base: `origin/main` at `d9f40198f79ed848e2a39e3231d1049ca6d3617b` on `2026-03-24`
Working certification branch: `cert/agent-v3`

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
| Agent Spec v3 | certified | `internal/state/assignment_compliance.go` (RULE-001–004, substatus enum, state groups, rule evaluators), `internal/state/agent_sessions.go` (finalize gate enforcement, substatus ownership split, atomic rule check seeding), `cmd/codero/session_bootstrap.go` (launcher bootstrap), `cmd/codero/main.go` (heartbeat CLI), `internal/state/agent_v3_certification_test.go` (8 clause-mapped tests), `docs/evidence/agent-v3-interactions.md` (§9.1 DOC) | All 11 acceptance criteria pass. `certified` (not `done`) because dependency specs (Daemon v2, Task Layer v2) remain `certification_pending`; Agent v3 cannot advance to `done` until its explicit dependency chain is also certified. |
| Task Layer v2 | certification_pending | `cmd/codero/task.go` (accept, emit, submit), `internal/state/agent_sessions.go`, `internal/state/github_links.go`, `internal/state/feedback_cache.go`, `internal/feedback/aggregator.go`, `internal/feedback/writer.go`, `internal/webhook/processor.go` (link upsert + cache invalidation), `internal/state/migrations/000011_task_layer_schema.*`, `000012_tl_v2_closeout.*`, `internal/state/lifecycle_test.go` | Closeout implementation is merged, but strict DoD recertification remains pending before the slice can be restored to `done`. |
| Daemon Spec v2 | certification_pending | `cmd/codero/main.go`, `internal/daemon/grpc/server.go`, `internal/daemon/observability.go`, `internal/webhook/reconciler.go`, `internal/scheduler/expiry.go`, `internal/config/config.go` | Daemon closeout is merged on `origin/main`, but the stricter DoD requires clause-to-test certification before the slice can be called `done`. |
| Repo-Context v1 | certification_pending | `internal/context/` (store, indexer, queries, types), `cmd/codero/context_cmd.go` (8 CLI commands), `.codero/context/graph.db` store, `docs/contracts/mi-006-repo-context.md`, `docs/module-intake-registry.md` (`MI-006`) | RC-V1 now includes the missing MI-006 intake artifacts, spec-aligned CLI JSON/error contract, dedicated contract tests, and a green strict-DoD validation pass on `feat/RC-V1-closeout`; merge to `main` is still pending. |
| Gate Config v1 | certification_pending | `internal/gate/config.go` (registry, parse, resolve, save, drift), `internal/gate/heartbeat.go` (effective config wiring), `internal/dashboard/handlers.go` (GET/PUT gate-config endpoints). | Gate Config v1 is merged, but strict DoD recertification remains pending before the slice can be restored to `done`. |

## Next Branch Candidates

1. Re-certify `Task Layer v2`, `Daemon Spec v2`, and `Gate Config v1` against the stricter DoD.
2. Certify `Repo-Context v1` after merge.
3. After certification of dependency chain, advance `Agent Spec v3` from `certified` to `done`.
4. Return to the still-partial specs from the authoritative index (Execution Loop v1, Review Gate v1, Dashboard API v1, Session Lifecycle v1, LiteLLM Chat v1, Real-Time Views v1).
