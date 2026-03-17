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
| COD-031 | feat/COD-031-v12-proving-gate-automation | Copilot | in_progress | 2026-03-17T00:51:00Z | 2026-03-17T00:51:00Z | v1.2 proving gate: `codero prove` command, Appendix G drill map, human+JSON output, 34 tests |
| COD-026 | feat/COD-026-tui-commands-and-web-port-hardening | Copilot | done | 2026-03-16T14:03:00Z | 2026-03-16T17:06:00Z | TUI command polish (codero tui, gate-status --json/--no-prompt, dashboard, ports), web port/routing hardening. PR #28 merged. |
| COD-025 | feat/COD-025-dashboard | Copilot | done | 2026-03-16T09:00:00Z | 2026-03-16T09:15:00Z | Web dashboard SPA embedded in binary, 9 API endpoints, SSE stream, settings, upload. PR #27 merged. |
| COD-024 | feat/COD-024-sprint6-tui-and-metrics | Codex | review | 2026-03-16T00:00:00Z | 2026-03-16T00:00:00Z | Sprint 6 TUI v2-alpha shipped (`gate-status --watch` Bubble Tea 3-pane), gate/dashboard parity preserved, auto gate-to-metric writes completed, hardening evidence documented. |
| COD-023 | feat/COD-023-shared-heartbeat-gate | gcb (Copilot-B) | review | 2026-03-16T01:44:11Z | 2026-03-16T02:10:00Z | Replace local commit-gate branch flow with shared heartbeat contract; wire progress bar in CLI/TUI + /gate dashboard endpoint. PR #24 open. |
| COD-020 | feat/COD-020-phase1f-proving-bootstrap | Codex | in_progress | 2026-03-15T00:00:00Z | 2026-03-15T00:00:00Z | Phase 1F proving-period bootstrap: daily scorecard, snapshot history, runbook updates |
| COD-010 | feat/COD-010-p1-s4-01-wfq-queue | Codex | review | 2026-03-14T11:11:00Z | 2026-03-14T15:30:00Z | Sprint 4: completed queue_stalled detection, observability endpoints (/health, /queue, /metrics), slot counter with atomic INCR/DECR via Lua |

## Rules

- A task can have only one `in_progress` owner.
- An agent can have only one `in_progress` task.
- Update this board at task start, handoff, block, and completion.
