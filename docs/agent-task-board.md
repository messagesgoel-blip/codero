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
| SL-V1 | cert/session-lifecycle-v1 | Copilot | partial | 2026-03-25T01:50:00Z | 2026-03-25T01:50:00Z | 11 of 18 criteria pass. Implemented: session_archives table + migration, archive trigger (finalize + expire), checkpoint enum, codero session end cmd. 20 clause-mapped tests. SL-9–SL-15 (tmux integration, wrapper) remain unimplemented — cannot advance to `implemented`. |
| RTV-V1 | cert/realtime-views-v1 | Copilot | partial | 2026-03-25T02:45:00Z | 2026-03-25T02:45:00Z | 12 of 18 criteria pass. Implemented: view registry (11 views, 7 implemented), 38 TUI config vars, 29 dashboard config vars, SSE endpoint verified. 23 clause-mapped tests (15 TUI + 4 dashboard + 4 registry). Missing: session drill-down, archives, compliance TUI views; RV-1 data parity; RV-2 checkpoint visibility; RV-3 sub-second updates; §4.2 SSE schema partial. |

## Rules

- A task can have only one `in_progress` owner.
- An agent can have only one `in_progress` task.
- Update this board at task start, handoff, block, and completion.
