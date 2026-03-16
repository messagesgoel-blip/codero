# Codero TUI v2 Architecture

## Overview

The Codero TUI v2 is a Bubble Tea-based terminal UI providing operators with a real-time 3-pane view of gate status, branch queue, and delivery events.

## 3-Pane Layout

```
┌──────────────┬───────────────────────────┬──────────────┐
│  GATES       │ [output][events][queue]... │  GATE BARS   │
│  copilot     │                           │  ✓ copilot   │
│  litellm     │  Gate Summary / Events /  │  ● litellm   │
├──────────────┤  Queue / Findings         │              │
│  BRANCH      │                           │  ── pipeline │
│  feat/COD-.. │                           │  ○ gitleaks  │
│  state: ...  │                           │  ○ semgrep   │
└──────────────┴───────────────────────────┴──────────────┘
[watching · interval 5s]  tab panes  ] tabs  r retry  : palette  q quit
```

## Component Hierarchy

- `Model` (app.go) — root Bubble Tea model, owns layout and message routing
  - `GatePane` (views_gate.go) — authoritative gate timeline + pipeline rows
  - `BranchPane` (views_branch.go) — current branch context
  - `QueuePane` (views_queue.go) — scrollable branch queue
  - `EventsPane` (views_events.go) — scrollable delivery event log
  - `viewport.Model` — output tab scrollable content
- `Theme` (theme.go) — centralised lipgloss style tokens
- `KeyMap` (keymap.go) — all operator keyboard shortcuts
- `Layout` (layout.go) — terminal-size adaptive pane dimensions

## Data Flow (Adapters Layer)

```
progress.env / gate.Result  →  adapters.FromGateResult()  →  GateViewModel  →  GatePane
state.BranchRecord[]        →  adapters.FromBranchRecords()  →  QueueItem[]   →  QueuePane
state.DeliveryEvent[]       →  eventsRefreshMsg              →  EventsPane
```

The `adapters` package is the sole translation boundary between domain types and TUI view models. No raw domain types are used outside adapters.

## Authoritative vs Pipeline Gate Labels

The gate pane separates:
1. **Authoritative** (from heartbeat contract): `copilot`, `litellm` — these drive real gate pass/fail
2. **Pipeline (local)**: `gitleaks`, `semgrep` — display-only, clearly labelled "local · non-authoritative"

This separation is contractually required and must be preserved in all future gate rows.

## Extension: Adding New Gate Rows

1. Add a new `PipelineRow` entry in `adapters/gate.go:staticPipelineRows()` for local-only checks.
2. For authoritative gates: add a field to `gate.Result` and update `adapters.FromGateResult()`.
3. Update `GatePane.View()` to render the new authoritative gate between `authGates` entries.

## Operator Quickstart

| Key | Action |
|-----|--------|
| `tab` | Cycle pane focus |
| `]` / `[` | Next/prev tab |
| `1-4` | Jump to tab (output/events/queue/findings) |
| `r` | Retry gate (only when gate is in terminal state) |
| `L` | Show gate log directory |
| `C-r` | Refresh from progress.env |
| `:` or `C-p` | Open command palette |
| `/` | Search mode |
| `q` | Quit |

## Usage

```bash
# One-shot display (existing text box)
codero gate-status

# Live Bubble Tea TUI (v2)
codero gate-status --watch

# Custom poll interval
codero gate-status --watch --interval 10
```
