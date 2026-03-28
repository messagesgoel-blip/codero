# Codero TUI Architecture

> **Operator quickstart and reference.** For architectural decisions and the
> design rationale, see [ADR-0006: TUI Shell Architecture](adr/0006-tui-shell-architecture.md).
> This file serves as the day-to-day operator reference for layout, shortcuts,
> and component structure.
>
> *History:* this file previously documented a 3-pane layout with `BranchPane`,
> `QueuePane`, and center tabs `output/events/queue/findings`. That model was
> replaced by the current 4-pane shell starting with UI-001.

## Current Layout

```text
┌────────────────┬──────────────────────────┬────────────┬──────────────────┐
│  LEFT          │  CENTER                  │  PIPELINE  │  RIGHT           │
│  Agents &      │  Logs / Overview /       │  Pipeline  │  Findings &      │
│  Relay         │  Events / Queue /        │  Cards     │  Routing         │
│  Orchestration │  Chat / Session / ...    │            │  Dashboard       │
└────────────────┴──────────────────────────┴────────────┴──────────────────┘
  COMMAND TERMINAL — CODERO                              [merge status] HH:MM
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `tab` / `S-tab` | Cycle pane focus |
| `]` / `[` | Next / prev center tab |
| `1-4` | Jump to logs / overview / events / queue |
| `o` | Overview (mission control) |
| `s` | Session drill-down |
| `a` | Archives |
| `i` | Config |
| `c` | Chat / review assistant |
| `p` | Focus pipeline pane |
| `r` | Retry gate |
| `L` | Open gate logs |
| `C-r` | Force refresh |
| `q` / `C-c` | Quit |

## Entry Points

```bash
codero tui                          # default: logs & architecture view
codero tui --view overview          # mission control
codero tui --view events            # delivery event stream
codero tui --view queue             # branch queue
codero tui --view chat              # review assistant
codero tui --no-alt-screen          # tmux / CI-adjacent terminals
```

## Component Hierarchy

- `Model` (app.go) — root Bubble Tea model
  - `GatePane` — agents and relay orchestration (left pane)
  - `LogsArchPane` — logs & architecture (center default)
  - `EventsPane` — delivery event log (center)
  - `QueuePane` — branch queue (center)
  - `SessionDrillPane` — session detail (center)
  - `ArchivesPane` — session archives (center)
  - `CompliancePane` — compliance checks (center)
  - `ConfigPane` — settings (center)
  - Chat tab — review assistant with slash commands (center)
  - `PipelinePane` — pipeline progress cards (pipeline pane)
  - `ChecksPane` — findings & routing dashboard (right pane)
- `Theme` (theme.go) — lipgloss style tokens
- `KeyMap` (keymap.go) — operator keyboard shortcuts
- `Layout` (layout.go) — terminal-size adaptive pane dimensions

## Data Flow

```text
progress.env / gate.Result  →  adapters.FromGateResult()     →  GateViewModel      →  GatePane
gate-check report           →  adapters.FromCheckReport()     →  CheckReportViewModel → ChecksPane, PipelinePane
state.BranchRecord[]        →  adapters.FromBranchRecords()   →  QueueItem[]        →  QueuePane
state.DeliveryEvent[]       →  eventsRefreshMsg               →  EventsPane
dashboard.ActiveSession[]   →  activeSessionsRefreshMsg       →  Overview, Pipeline
```

## Authoritative vs Pipeline Gate Labels

The gate pane separates:
1. **Authoritative** (from heartbeat contract): `copilot`, `litellm` — drive real gate pass/fail
2. **Pipeline (local)**: `gitleaks`, `semgrep` — display-only, labelled "local · non-authoritative"

## Further Reading

- [ADR-0006: TUI Shell Architecture](adr/0006-tui-shell-architecture.md) — design decisions and reference borrow matrix
- [v1.2.4 Backlog: UI-001](roadmaps/v1.2.4-backlog.md) — implementation history
- [v1.3.0 Backlog: UI-004](roadmaps/v1.3.0-backlog.md) — shortcut cleanup (completed)
