# Agent Task Board

Use this file as the task-board ledger for spec-driven work.

Status note (2026-03-24 UTC): legacy branch-era rows were retired after PRs `#88`-`#91`
merged and the Codero spec set under `/srv/storage/Specifications/codero` became the
authoritative baseline. Use `docs/spec-implementation-checklist.md` for the audited
implementation status on `origin/main`, then use this board to track the next slice to
branch from `main`.

## Status values

- `planned`
- `in_progress`
- `blocked`
- `review`
- `done`

## Active Tasks

| Task ID | Branch | Owner Agent | Status | Started (UTC) | Updated (UTC) | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| SPEC-BASELINE | chore/spec-baseline-rebaseline | Codex | done | 2026-03-24T01:05:03Z | 2026-03-24T01:05:03Z | Promote `/srv/storage/Specifications/codero` as the final baseline, publish the direct spec checklist, and rebaseline roadmap/task tracking against `origin/main` `2ae2557`. |
| AGENT-V3 | main | unassigned | done | 2026-03-24T01:05:03Z | 2026-03-24T01:05:03Z | Core agent-session bootstrap, assignment substatus enums, rule monitoring, and lifecycle compliance are merged on `origin/main`. See `docs/spec-implementation-checklist.md`. |
| TL-V2-CLOSEOUT | feat/tl-v2-closeout | OpenCode | done | 2026-03-24T05:00:00Z | 2026-03-24T07:30:00Z | Task Layer v2 closeout: live `codero_github_links` CRUD, `task_feedback_cache` with source_status, feedback aggregator with precedence/truncation, worktree file writer (TASK.md, FEEDBACK.md, current.json), `codero task submit`, webhook-driven link upsert + cache invalidation, import boundary enforcement, and normative lifecycle integration tests. 10 tasks, 8 commits. |
| DM-V2-CLOSEOUT | feat/DM-V2-closeout | Copilot | done | 2026-03-24T12:55:00Z | 2026-03-24T13:30:00Z | Daemon Spec v2 closeout: defined proto contract (`proto/codero/daemon/v1/daemon.proto`) with 6 gRPC services (Session, Task, Assignment, Feedback, Gate, Health), implemented all service handlers backed by state/session stores, wired gRPC+HTTP on same API port via h2c multiplexing, added readiness interceptor (codes.Unavailable until startup sweep completes), integrated into daemon lifecycle in `cmd/codero/main.go`, and added comprehensive gRPC server tests. |
| RC-V1 | next branch from `main` | unassigned | planned | 2026-03-24T01:05:03Z | 2026-03-24T01:05:03Z | Repo-context v1 is not started on the audited base. Implement the additive `.codero/context/graph.db` plus `codero context ...` command family. |
| GATE-V1 | next branch from `main` | unassigned | planned | 2026-03-24T01:05:03Z | 2026-03-24T01:05:03Z | Gate Config v1 still needs machine-global `$HOME/.codero/config.env`, dashboard parity, and the broader gate env matrix from the spec. |

## Rules

- A task can have only one `in_progress` owner.
- An agent can have only one `in_progress` task.
- Update this board at task start, handoff, block, and completion.
