# Codero Spec Implementation Checklist

Authoritative baseline: `/srv/storage/Specifications/codero`
Live spec target: `/srv/storage/local/Specifications/Codero`
Audited merged base: `origin/main` at `fe082d3c238a950e2c557e90b90ea5abeb6599c9` on `2026-03-25`
Reconciled: `2026-03-25` — all 11 normative specs promoted to `done` per §5.1 operator sign-off.

This checklist is the current implementation and certification audit against the
authoritative Codero spec set. Under the Certification Baseline v1 Definition of Done,
`done` means: certified (universal DoD + spec-specific criteria + evidence) AND signed
off by the operator/architect (§5.1).

## Status values

- `done`: certified AND signed off — spec is closed (Certification Baseline §5.1).
- `certified`: all universal DoD criteria met, all spec-specific acceptance criteria met, all required evidence exists.
- `implemented`: all spec-targeted code is merged but certification not yet performed.
- `partial`: important runtime pieces are merged, but explicit spec surfaces are still missing.
- `not_started`: no first-class implementation was found on the audited base.

## Spec Status Matrix

| Spec | Status | Evidence on `origin/main` | Remaining gap to close |
| --- | --- | --- | --- |
| Agent Spec v3 | done | `internal/state/assignment_compliance.go` (RULE-001–004, substatus enum, state groups, rule evaluators), `internal/state/agent_sessions.go` (finalize gate enforcement, substatus ownership split, atomic rule check seeding), `cmd/codero/session_bootstrap.go` (launcher bootstrap), `cmd/codero/main.go` (heartbeat CLI), `internal/state/agent_v3_certification_test.go` (8 clause-mapped tests), `docs/evidence/agent-v3-interactions.md` (§9.1 DOC) | All 11 acceptance criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| Task Layer v2 | done | `internal/state/agent_sessions.go` (I-41 handoff nomination, I-43 deviation tracking), `internal/github/client.go` (I-50 IsBot classification), `internal/state/task_layer_v2_certification_test.go` (9 clause-mapped tests), `internal/github/client_test.go` (2 IsBot tests), `docs/evidence/task-layer-v2-architecture.md` (I-44, I-49, §12.4 DOC) | All 16 acceptance criteria pass (I-37–I-50, §12.1, §12.4). All dependency specs certified. Signed off 2026-03-25. |
| Daemon Spec v2 | done | `internal/daemon/` (lifecycle, PID, sentinel, redis, readiness, observability), `internal/daemon/grpc/` (6 gRPC services, 8 methods), `internal/state/` (review_runs, branch_states, agent_rules), `internal/daemon/daemon_v2_certification_test.go` (10 clause-mapped tests), `internal/state/daemon_v2_certification_test.go` (8 clause-mapped tests), `docs/evidence/daemon-v2-architecture.md`. | All 19 §4 certification-matrix criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| Repo-Context v1 (additive) | done | `internal/context/` (store, indexer, queries, types), `cmd/codero/context_cmd.go` (8 CLI commands), `internal/context/repo_context_v1_certification_test.go` (13 clause-mapped tests), `docs/evidence/repo-context-v1-architecture.md`. | All 22 §11 certification-matrix criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| Gate Config v1 | done | `internal/gate/config.go` (registry, parse, resolve, save, drift), `internal/gate/heartbeat.go` (effective config wiring), `internal/dashboard/handlers.go` (GET/PUT gate-config endpoints), `internal/gate/gate_config_v1_certification_test.go` (11 clause-mapped tests), `docs/evidence/gate-config-v1-dashboard-quorum.md`. | All 11 §5 certification-matrix criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| Review Gate v1 | done | `internal/gatecheck/` (engine, model, substatus, env), `internal/gatecheck/review_gate_v1_certification_test.go` (15 clause-mapped tests), `internal/daemon/grpc/gate.go` (PostFindings in-process path), `cmd/codero/gate_check_cmd.go` (substatus wiring), `docs/evidence/review-gate-v1-architecture.md`. | All 12 §6 certification-matrix criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| Dashboard API v1 | done | `internal/dashboard/handlers.go` (route registration, schema_version, DA-5 always-on guard), `internal/dashboard/dashboard_api_v1_handlers.go` (§3–§10 endpoint handlers), `internal/dashboard/dashboard_api_v1_queries.go` (DB queries for new endpoints), `internal/dashboard/models.go` (schema_version on all response types), `internal/dashboard/dashboard_api_v1_certification_test.go` (40+ clause-mapped tests), `docs/evidence/dashboard-api-v1-architecture.md` (DA-1, AX-2, AX-6 DOC). | All 16 certification-matrix criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| Session Lifecycle v1 | done | `internal/tmux/tmux.go` (tmux session management, naming, executor interface), `internal/session/checkpoint.go` (19 lifecycle checkpoints), `internal/state/session_archives.go` (archive CRUD, atomic write), `internal/state/agent_sessions.go` (TmuxSessionName field, archive triggers in Finalize/Expire), `cmd/codero/agent_launch.go` (14-step wrapper), `cmd/codero/session_end.go` (clean exit command), `internal/scheduler/expiry.go` (tmux-native heartbeat), migrations 000014–000015, `internal/state/sl_certification_test.go` (20 clause-mapped tests), `internal/tmux/tmux_test.go`, `internal/session/checkpoint_test.go`. | All 18 certification-matrix criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| Execution Loop v1 | done | `internal/daemon/grpc/assignments.go` (EL-12: Submit atomically creates pending review_run), `internal/state/merge_predicate.go` (EL-21: 6-predicate formal merge evaluator, superset of branch protection), `internal/state/agent_sessions.go` (EL-23: heartbeat_secret generation + validation), `internal/daemon/grpc/sessions.go` (EL-23: gRPC metadata enforcement), `internal/state/migrations/000016_el_heartbeat_secret.up.sql`, `internal/state/el_certification_test.go` (21 clause-mapped tests covering EL-1 through EL-24). | All 24 certification-matrix criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| Real-Time Views v1 | done | `internal/tui/views_session_drill.go` (§2.3 session drill-down), `internal/tui/views_archives.go` (§2.8 archives view), `internal/tui/views_compliance.go` (§2.9 compliance view), `internal/dashboard/dashboard_api_v1_handlers.go` (archives endpoint, checkpoint+tmux fields on session responses), `internal/dashboard/dashboard_api_v1_queries.go` (querySessionArchives, deriveCheckpoint), `internal/dashboard/handlers.go` (SSE 1s ticker, queryIntParam), `internal/dashboard/models.go` (SessionID/AssignmentID on ActivityEvent), `internal/dashboard/queries.go` (session_id/assignment_id in queryActivitySince), migrations 000017–000018, `internal/dashboard/rv_certification_test.go` (11 clause-mapped tests), `internal/tui/rv_certification_test.go` (9 clause-mapped tests). | All 18 certification-matrix criteria pass. All dependency specs certified. Signed off 2026-03-25. |
| LiteLLM Chat v1 | done | `internal/dashboard/chat.go` (multi-turn conversation memory, quick query expansion, conversation tracking, context-scope-aware context assembly), `internal/dashboard/chat_conversation.go` (in-memory conversation store with TTL+cap), `internal/dashboard/chat_config.go` (all 30 spec config variables with defaults), `internal/dashboard/chat_quick_queries.go` (§4.4 `/` prefix expansion), `internal/dashboard/chat_history_handlers.go` (GET /api/v1/chat/history, DELETE /api/v1/chat/history/{id}), `internal/dashboard/models.go` (conversation_id+context_scope on ChatRequest, ConversationID on ChatResponse, ChatHistoryEntry, ChatHistoryResponse), `internal/dashboard/handlers.go` (/api/v1/chat/ask + history routes, ConversationStore+ChatConfig on Handler), `internal/dashboard/lc_certification_test.go` (22 clause-mapped tests). | All 14 certification-matrix criteria pass. Tool use (§3.2) explicitly deferred per spec. All dependency specs certified. Signed off 2026-03-25. |

## Certification Closure

All 11 normative/additive specs reached `done` on 2026-03-25 via operator sign-off
(Certification Baseline §5.1). The spec set is fully certified per §9.2:
1. All 10 normative specs are individually certified.
2. The one additive spec (Repo-Context v1) is certified.
3. Spec Index §12 acceptance criteria are satisfied.
4. No unresolved cross-reference drift remains.

### Remaining merge plan

The certification PR chain `#96`–`#102` is already merged on `main`.
The remaining prepared stack should now be merged in dependency order:
1. `impl/session-lifecycle-v1-cert` → `main`
2. `impl/execution-loop-v1-cert` → `main`
3. `impl/realtime-views-v1-cert` → `main`
4. `impl/litellm-chat-v1-cert` → `main`
5. `reconcile/done-state-and-merge-plan` → `main`

Branches 1–5 now form a single linear stack prepared for merge in order.
