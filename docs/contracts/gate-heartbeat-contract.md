# Gate Heartbeat Contract

**Status:** active  
**Owner:** codero  
**Implemented in:** `/srv/storage/shared/agent-toolkit/bin/gate-heartbeat`  
**Go client:** `internal/gate`

---

## Overview

The gate-heartbeat contract defines the pre-commit local review gate used before
any agent may commit. The gate enforces the Sprint 6 policy:

1. Copilot reviews the diff first.
2. LiteLLM reviews the diff second.
3. Each gate has its own independent timeout.
4. Infra/auth failures are non-blocking; explicit findings block.
5. An agent must poll until `STATUS: PASS` before committing.

---

## Polling Contract

The gate-heartbeat binary implements a stateful polling protocol:

| Call | Pre-condition | Output |
|------|--------------|--------|
| First call | No `state.env` present | Starts run background process; returns `STATUS: PENDING` |
| Subsequent calls | Process running | Returns `STATUS: PENDING` with current progress |
| Subsequent calls | Process finished (rc=0) | Returns `STATUS: PASS` |
| Subsequent calls | Process finished (rc!=0) | Returns `STATUS: FAIL` with `COMMENTS:` block |

Callers must not start a new gate run until the previous run reaches a terminal
state (`PASS` or `FAIL`).

---

## Output Format

Gate-heartbeat prints `KEY: VALUE` pairs to stdout, one per line:

```
STATUS: PENDING | PASS | FAIL
RUN_ID: <timestamp-random>
ELAPSED_SEC: <n>
POLL_AFTER_SEC: <n>          (present in PENDING only)
PROGRESS_BAR: [<icon> copilot:<state>] [<icon> litellm:<state>]
CURRENT_GATE: copilot | litellm | none
COPILOT_STATUS: <gate-state>
LITELLM_STATUS: <gate-state>
COMMENTS:                    (FAIL only; "none" when no blockers)
<comment line 1>
<comment line 2>
...
```

---

## Gate States

| State | Icon | Meaning |
|-------|------|---------|
| `pending` | `○` | Gate has not started |
| `running` | `●` | Gate is actively running |
| `pass` | `✓` | Gate completed with no blocking findings |
| `blocked` | `✗` | Gate found blocking findings |
| `timeout` | `⏱` | Gate exceeded its time budget |
| `infra_fail` | `!` | Infrastructure/auth failure (non-blocking) |

---

## Overall Status

| STATUS | Meaning |
|--------|---------|
| `PENDING` | Run in progress; poll again after `POLL_AFTER_SEC` |
| `PASS` | Both gates passed; commit is allowed |
| `FAIL` | At least one gate has blocking findings; `COMMENTS:` contains blockers |

An `infra_fail` on a gate does **not** produce `STATUS: FAIL`. The gate policy
requires at least one AI gate to pass. A gate that fails due to infrastructure
issues counts as infra-bypassed (not as a blocker).

---

## Environment Variables

All timeouts are independent. Setting one does not affect any other.

| Variable | Default | Description |
|----------|---------|-------------|
| `CODERO_COPILOT_TIMEOUT_SEC` | `15` | Copilot gate per-run timeout |
| `CODERO_LITELLM_TIMEOUT_SEC` | `45` | LiteLLM gate per-run timeout |
| `CODERO_GATE_TOTAL_TIMEOUT_SEC` | `180` | Overall gate wall-clock budget |
| `CODERO_GATE_POLL_INTERVAL_SEC` | `180` | Suggested poll interval (returned in `POLL_AFTER_SEC`) |
| `CODERO_GATE_HEARTBEAT_BIN` | `/srv/storage/shared/agent-toolkit/bin/gate-heartbeat` | Override binary path |
| `CODERO_REPO_PATH` | `$(git rev-parse --show-toplevel)` | Repo root for state file location |

---

## Progress Fields

The following fields are written to `.codero/gate-heartbeat/progress.env` and
are also available from the `/gate` observability endpoint:

| Field | Description |
|-------|-------------|
| `PROGRESS_BAR` | Single-line bar: `[icon copilot:state] [icon litellm:state]` |
| `CURRENT_GATE` | Name of the gate currently running (`copilot`, `litellm`, or `none`) |
| `COPILOT_STATUS` | Current Copilot gate state |
| `LITELLM_STATUS` | Current LiteLLM gate state |

These fields are rendered identically in the CLI (`commit-gate` command) and the
dashboard UI (`/gate` endpoint on the observability server).

---

## Timeout Independence Rule

Each gate runs with its own `timeout <n>` invocation in the shell script.
The Copilot timeout expiry does **not** reduce the LiteLLM budget. Agents must
not apply a Go `context.WithTimeout` wrapping the entire gate run; the shell
handles the timeout budget. The Go context is used only for cancellation signals.

---

## State Machine Integration

| Transition | Trigger | Gate requirement |
|-----------|---------|-----------------|
| `coding -> local_review` | Agent signals ready | None (gate starts after) |
| `local_review -> queued_cli` | Commit gate PASS | `STATUS: PASS` required |
| `local_review -> coding` | Commit gate FAIL | Agent must fix and re-submit |

---

## Dashboard UI Parity

The shared `internal/gate` Go package provides:

- `gate.ParseOutput(string) Result` — parses heartbeat stdout
- `gate.RenderBar(copilot, litellm, current string) string` — renders the progress bar
- `gate.FormatProgressLine(Result) string` — compact CLI line (for `\r` overwrite)
- `gate.FormatSummary(Result) string` — multi-line summary after completion

Both the CLI (`commit-gate` command) and the dashboard UI (`/gate` endpoint)
use `gate.RenderBar()` for identical visual output.
