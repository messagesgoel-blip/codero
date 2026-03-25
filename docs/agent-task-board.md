# Agent Task Board

Use this file as the task-board ledger for spec-driven work.

Status note (2026-03-24 UTC): legacy branch-era rows were retired after PRs `#88`-`#91`
merged and the Codero spec set under `/srv/storage/Specifications/codero` became the
authoritative baseline. Under the stricter Definition of Done, a slice is not `done`
until its explicit spec acceptance criteria are satisfied, the universal DoD passes,
and spec-targeted tests proving those criteria have been run.

## Status values

- `planned`
- `in_progress`
- `certification_pending`
- `blocked`
- `review`
- `done`

## Active Tasks

| Task ID | Branch | Owner Agent | Status | Started (UTC) | Updated (UTC) | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| SPEC-BASELINE | chore/spec-baseline-rebaseline | Codex | done | 2026-03-24T01:05:03Z | 2026-03-24T01:05:03Z | Promote `/srv/storage/Specifications/codero` as the final baseline, publish the direct spec checklist, and rebaseline roadmap/task tracking against `origin/main` `2ae2557`. |
| AGENT-V3 | cert/agent-v3 | Copilot | certified | 2026-03-24T01:05:03Z | 2026-03-24T22:00:00Z | All 11 acceptance criteria pass with clause-mapped tests. `certified` (not `done`) because dependency specs remain `certification_pending`. |
| TL-V2-CLOSEOUT | cert/task-layer-v2 | Copilot | certified | 2026-03-24T05:00:00Z | 2026-03-25T00:00:00Z | All 16 TL v2 matrix criteria pass. I-41 handoff enforcement, I-43 deviation tracking, I-50 IsBot classification implemented. 11 clause-mapped certification tests added. `certified` pending Daemon v2 dependency certification for `done`. |
| DM-V2-CLOSEOUT | feat/DM-V2-closeout | Copilot | certified | 2026-03-24T12:55:00Z | 2026-03-24T12:20:00Z | All 19 §4 certification-matrix criteria pass (18 clause-mapped tests + DOC evidence). Cannot advance to `done` until dependency specs are also certified. |
| RC-V1 | feat/RC-V1-closeout | Copilot | certified | 2026-03-24T14:56:00Z | 2026-03-24T16:20:00Z | All 22 §11 certification-matrix criteria pass (13 clause-mapped tests + DOC evidence). Cannot advance to `done` until dependency specs are also certified. |
| GATE-V1 | feat/GATE-V1-closeout | Copilot | certified | 2026-03-24T14:30:00Z | 2026-03-24T12:20:00Z | All 11 §5 certification-matrix criteria pass (11 clause-mapped tests + DOC evidence). Cannot advance to `done` until dependency specs (Daemon v2, Task Layer v2, Review Gate v1) are also certified. |
| RG-V1 | cert/review-gate-v1 | Copilot | certified | 2026-03-24T21:09:00Z | 2026-03-24T21:30:00Z | All 12 §6 certification-matrix criteria pass (15 clause-mapped tests + DOC evidence). Implemented: gate-substatus.env atomic write (RG-1), findings cap at 50 (RG-7), CODERO_GATE_INVOCATION field (RG-11). Cannot advance to `done` until full dependency chain is certified. |
| DASH-V1 | cert/dashboard-api-v1 | Copilot | certified | 2026-03-25T00:00:00Z | 2026-03-25T00:30:00Z | All 16 certification-matrix criteria pass (40+ clause-mapped tests + DOC evidence). Implemented: §3–§10 endpoints, schema_version (AX-7), DA-4 force-merge RULE-001 enforcement, DA-5 always-on 403, DA-9 config.env persistence. |
| SL-V1 | impl/session-lifecycle-v1-cert | Copilot | certified | 2026-03-26T00:00:00Z | 2026-03-26T12:00:00Z | All 18 certification-matrix criteria pass (20 clause-mapped tests). Implemented: tmux session binding (SL-9), tmux-native heartbeat (SL-10), naming convention (SL-11), 14-step Go wrapper (SL-12/13), unclean-exit reporting (SL-14), log capture (SL-15), session archives table, checkpoint enum, session end command. |
| EL-V1 | impl/execution-loop-v1-cert | Copilot | certified | 2026-03-26T12:00:00Z | 2026-03-26T18:00:00Z | All 24 certification-matrix criteria pass. Implemented: submit → gate → delivery → merge composed path, merge predicate superset enforcement, launcher-only heartbeat RPC restriction. |
| RV-V1 | impl/realtime-views-v1-cert | Copilot | certified | 2026-03-27T00:00:00Z | 2026-03-27T12:00:00Z | All 18 certification-matrix criteria pass (20 clause-mapped tests). Implemented: session drill-down (§2.3), archives view (§2.8), compliance view (§2.9), SSE event schema session_id/assignment_id (§4.2), checkpoint visibility (RV-2), 1s SSE ticker (RV-3), TUI/dashboard parity via shared state DB (RV-1), graceful degradation (RV-6). |
| LC-V1 | impl/litellm-chat-v1-cert | Copilot | certified | 2026-03-25T12:00:00Z | 2026-03-25T12:00:00Z | All 14 certification-matrix criteria pass (22 clause-mapped tests). Implemented: multi-turn conversation memory with TTL (§3.3), conversation_id + context_scope on request contract (§2.1), GET /api/v1/chat/history (§2.2), DELETE /api/v1/chat/history/{id} (§2.3), quick query expansion (§4.4), 30 config variables (§6), enabled/disabled guard (LC-4). Tool use (§3.2) explicitly deferred per spec. |

## Rules

- A task can have only one `in_progress` owner.
- An agent can have only one `in_progress` task.
- Update this board at task start, handoff, block, and completion.
