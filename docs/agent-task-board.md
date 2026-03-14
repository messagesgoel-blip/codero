# Agent Task Board

Use this file as the single source of truth for active agent work.

## Status values

- `planned`
- `in_progress`
- `blocked`
- `review`
- `done`

## Active Tasks

| Task ID | Branch | Owner Agent | Status | Started (UTC) | Updated (UTC) | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| COD-010 | feat/COD-010-p1-s4-01-wfq-queue | Codex | review | 2026-03-14T11:11:00Z | 2026-03-14T15:30:00Z | Sprint 4: completed queue_stalled detection, observability endpoints (/health, /queue, /metrics), slot counter with atomic INCR/DECR via Lua |

## Rules

- A task can have only one `in_progress` owner.
- An agent can have only one `in_progress` task.
- Update this board at task start, handoff, block, and completion.
