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
| Task Layer v2 | certified | `internal/state/agent_sessions.go` (I-41 handoff nomination, I-43 deviation tracking), `internal/github/client.go` (I-50 IsBot classification), `internal/state/task_layer_v2_certification_test.go` (9 clause-mapped tests), `internal/github/client_test.go` (2 IsBot tests), `docs/evidence/task-layer-v2-architecture.md` (I-44, I-49, §12.4 DOC) | All 16 acceptance criteria pass (I-37–I-50, §12.1, §12.4). `certified` (not `done`) because dependency specs (Daemon v2) remain `certification_pending`. |
| Daemon Spec v2 | certified | `internal/daemon/` (lifecycle, PID, sentinel, redis, readiness, observability), `internal/daemon/grpc/` (6 gRPC services, 8 methods), `internal/state/` (review_runs, branch_states, agent_rules), `internal/daemon/daemon_v2_certification_test.go` (10 clause-mapped tests), `internal/state/daemon_v2_certification_test.go` (8 clause-mapped tests), `docs/evidence/daemon-v2-architecture.md`. | All 19 §4 certification-matrix criteria pass (18 tests + DOC evidence for scope-boundary items D-29/D-32). Cannot advance to `done` until dependency specs are also certified. |
| Repo-Context v1 | certified | `internal/context/` (store, indexer, queries, types), `cmd/codero/context_cmd.go` (8 CLI commands), `internal/context/repo_context_v1_certification_test.go` (13 clause-mapped tests), `docs/evidence/repo-context-v1-architecture.md`. | All 22 §11 certification-matrix criteria pass. Cannot advance to `done` until dependency specs are also certified. |
| Gate Config v1 | certified | `internal/gate/config.go` (registry, parse, resolve, save, drift), `internal/gate/heartbeat.go` (effective config wiring), `internal/dashboard/handlers.go` (GET/PUT gate-config endpoints), `internal/gate/gate_config_v1_certification_test.go` (11 clause-mapped tests), `docs/evidence/gate-config-v1-dashboard-quorum.md`. | All 11 §5 certification-matrix criteria pass. Cannot advance to `done` until Daemon Spec v2, Task Layer v2, and Review Gate v1 are also certified. |
| Review Gate v1 | certified | `internal/gatecheck/` (engine, model, substatus, env), `internal/gatecheck/review_gate_v1_certification_test.go` (15 clause-mapped tests), `internal/daemon/grpc/gate.go` (PostFindings in-process path), `cmd/codero/gate_check_cmd.go` (substatus wiring), `docs/evidence/review-gate-v1-architecture.md`. | All 12 §6 certification-matrix criteria pass (15 tests + DOC evidence). Cannot advance to `done` until all dependency specs in the certification chain are also certified. |
| Dashboard API v1 | certified | `internal/dashboard/handlers.go` (route registration, schema_version, DA-5 always-on guard), `internal/dashboard/dashboard_api_v1_handlers.go` (§3–§10 endpoint handlers), `internal/dashboard/dashboard_api_v1_queries.go` (DB queries for new endpoints), `internal/dashboard/models.go` (schema_version on all response types), `internal/dashboard/dashboard_api_v1_certification_test.go` (40+ clause-mapped tests), `docs/evidence/dashboard-api-v1-architecture.md` (DA-1, AX-2, AX-6 DOC). | All 16 certification-matrix criteria pass. §3–§10 endpoint coverage, DA-4 force-merge RULE-001, DA-5 always-on 403, DA-9 config.env write-through, AX-1 prefix compliance, AX-7 schema_version on all responses. Cannot advance to `done` until all dependency specs are also certified. |
| Session Lifecycle v1 | certified | `internal/tmux/tmux.go` (tmux session management, naming, executor interface), `internal/session/checkpoint.go` (19 lifecycle checkpoints), `internal/state/session_archives.go` (archive CRUD, atomic write), `internal/state/agent_sessions.go` (TmuxSessionName field, archive triggers in Finalize/Expire), `cmd/codero/agent_launch.go` (14-step wrapper), `cmd/codero/session_end.go` (clean exit command), `internal/scheduler/expiry.go` (tmux-native heartbeat), migrations 000014–000015, `internal/state/sl_certification_test.go` (20 clause-mapped tests), `internal/tmux/tmux_test.go`, `internal/session/checkpoint_test.go`. | All 18 certification-matrix criteria pass (SL-1–SL-7, SL-9–SL-15, §1.1, §2, §4.1, §4.2). tmux binding, naming convention, 14-step wrapper, archive persistence, unclean-exit reporting, log capture all implemented and tested. Cannot advance to `done` until all dependency specs are also certified. |
| Execution Loop v1 | certified | `internal/daemon/grpc/assignments.go` (EL-12: Submit atomically creates pending review_run), `internal/state/merge_predicate.go` (EL-21: 6-predicate formal merge evaluator, superset of branch protection), `internal/state/agent_sessions.go` (EL-23: heartbeat_secret generation + validation), `internal/daemon/grpc/sessions.go` (EL-23: gRPC metadata enforcement), `internal/state/migrations/000016_el_heartbeat_secret.up.sql`, `internal/state/el_certification_test.go` (21 clause-mapped tests covering EL-1 through EL-24). | All 24 certification-matrix criteria pass (EL-1–EL-24). EL-12: submit deterministically triggers gate via atomic review_run creation. EL-21: merge predicate formally covers all 3 GitHub branch protection rules + 3 Codero-additive predicates. EL-23: heartbeat_secret enforces launcher-only semantics. Cannot advance to `done` until all dependency specs are also certified. |

## Next Branch Candidates

1. All core specs (Agent v3, Task Layer v2, Daemon v2, Repo-Context v1, Gate Config v1, Review Gate v1, Dashboard API v1, Session Lifecycle v1, Execution Loop v1) are now `certified`. Advance to `done` once dependency chain is fully certified.
2. Return to the still-partial specs from the authoritative index (LiteLLM Chat v1, Real-Time Views v1).
3. After all specs in the dependency chain reach `done`, update the spec index.
