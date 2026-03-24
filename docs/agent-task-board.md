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
| TL-V2-CLOSEOUT | feat/tl-v2-closeout | OpenCode | certification_pending | 2026-03-24T05:00:00Z | 2026-03-24T12:20:00Z | Task Layer v2 closeout is merged, but strict DoD recertification is still pending before the slice can be restored to `done`. |
| DM-V2-CLOSEOUT | feat/DM-V2-closeout | Copilot | certification_pending | 2026-03-24T12:55:00Z | 2026-03-24T12:20:00Z | Daemon Spec v2 closeout is merged, but strict DoD recertification is still pending before the slice can be restored to `done`. |
| RC-V1 | feat/RC-V1-closeout | Copilot | certification_pending | 2026-03-24T14:56:00Z | 2026-03-24T16:20:00Z | Repo-Context v1 now includes the missing MI-006 intake artifacts, spec-aligned CLI JSON/error contract, dedicated store/query/CLI contract tests, and a green strict-DoD validation pass on the branch. Merge to `main` is still pending. |
| GATE-V1 | feat/GATE-V1-closeout | Copilot | certification_pending | 2026-03-24T14:30:00Z | 2026-03-24T12:20:00Z | Gate Config v1 is merged, but strict DoD recertification is still pending before the slice can be restored to `done`. |

## Rules

- A task can have only one `in_progress` owner.
- An agent can have only one `in_progress` task.
- Update this board at task start, handoff, block, and completion.
