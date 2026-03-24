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
| Daemon Spec v2 | certification_pending | `cmd/codero/main.go`, `internal/daemon/grpc/server.go`, `internal/daemon/observability.go`, `internal/webhook/reconciler.go`, `internal/scheduler/expiry.go`, `internal/config/config.go` | Daemon closeout is merged on `origin/main`, but the stricter DoD requires clause-to-test certification before the slice can be called `done`. |
| Repo-Context v1 | certified | `internal/context/` (store, indexer, queries, types), `cmd/codero/context_cmd.go` (8 CLI commands), `internal/context/repo_context_v1_certification_test.go` (13 clause-mapped tests), `docs/evidence/repo-context-v1-architecture.md`. | All 22 §11 certification-matrix criteria pass. Cannot advance to `done` until dependency specs are also certified. |
| Gate Config v1 | certified | `internal/gate/config.go` (registry, parse, resolve, save, drift), `internal/gate/heartbeat.go` (effective config wiring), `internal/dashboard/handlers.go` (GET/PUT gate-config endpoints), `internal/gate/gate_config_v1_certification_test.go` (11 clause-mapped tests), `docs/evidence/gate-config-v1-dashboard-quorum.md`. | All 11 §5 certification-matrix criteria pass. Cannot advance to `done` until Daemon Spec v2, Task Layer v2, and Review Gate v1 are also certified. |

## Next Branch Candidates

1. Re-certify `Daemon Spec v2` and `Review Gate v1` against the stricter DoD.
2. After certification of remaining dependency chain, advance certified specs to `done`.
3. After certification of dependency chain, advance `Agent Spec v3` from `certified` to `done`.
4. Return to the still-partial specs from the authoritative index (Execution Loop v1, Review Gate v1, Dashboard API v1, Session Lifecycle v1, LiteLLM Chat v1, Real-Time Views v1).
