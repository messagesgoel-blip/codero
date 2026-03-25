# Agent Task Board

Use this file as the task-board ledger for spec-driven work.

Status note (2026-03-24 UTC): legacy branch-era rows were retired after PRs `#88`-`#91`
merged and the Codero spec set under `/srv/storage/Specifications/codero` became the
authoritative baseline. Under the Certification Baseline v1 Definition of Done, a slice
is not `done` until its explicit spec acceptance criteria are satisfied, the universal
DoD passes, spec-targeted tests proving those criteria have been run, and the operator
has signed off.

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
| AGENT-V3 | cert/agent-v3 | Copilot | done | 2026-03-24T01:05:03Z | 2026-03-25T16:00:00Z | All 11 criteria pass. Signed off 2026-03-25. |
| TL-V2-CLOSEOUT | cert/task-layer-v2 | Copilot | done | 2026-03-24T05:00:00Z | 2026-03-25T16:00:00Z | All 16 criteria pass. Signed off 2026-03-25. |
| DM-V2-CLOSEOUT | feat/DM-V2-closeout | Copilot | done | 2026-03-24T12:55:00Z | 2026-03-25T16:00:00Z | All 19 criteria pass. Signed off 2026-03-25. |
| RC-V1 | feat/RC-V1-closeout | Copilot | done | 2026-03-24T14:56:00Z | 2026-03-25T16:00:00Z | All 22 criteria pass. Signed off 2026-03-25. |
| GATE-V1 | feat/GATE-V1-closeout | Copilot | done | 2026-03-24T14:30:00Z | 2026-03-25T16:00:00Z | All 11 criteria pass. Signed off 2026-03-25. |
| RG-V1 | cert/review-gate-v1 | Copilot | done | 2026-03-24T21:09:00Z | 2026-03-25T16:00:00Z | All 12 criteria pass. Signed off 2026-03-25. |
| DASH-V1 | cert/dashboard-api-v1 | Copilot | done | 2026-03-25T00:00:00Z | 2026-03-25T16:00:00Z | All 16 criteria pass. Signed off 2026-03-25. |
| SL-V1 | impl/session-lifecycle-v1-cert | Copilot | done | 2026-03-25T11:06:48Z | 2026-03-25T16:00:00Z | All 18 criteria pass. Signed off 2026-03-25. |
| EL-V1 | impl/execution-loop-v1-cert | Copilot | done | 2026-03-25T10:38:32Z | 2026-03-25T16:00:00Z | All 24 criteria pass. Signed off 2026-03-25. |
| RV-V1 | impl/realtime-views-v1-cert | Copilot | done | 2026-03-25T11:47:02Z | 2026-03-25T16:00:00Z | All 18 criteria pass. Signed off 2026-03-25. |
| LC-V1 | impl/litellm-chat-v1-cert | Copilot | done | 2026-03-25T14:05:36Z | 2026-03-25T16:00:00Z | All 14 criteria pass. Signed off 2026-03-25. |

## Rules

- A task can have only one `in_progress` owner.
- An agent can have only one `in_progress` task.
- Update this board at task start, handoff, block, and completion.
