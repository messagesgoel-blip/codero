# Dashboard Architecture

## Overview

The Codero web dashboard (`/dashboard`) provides a live, browser-based view of the
review orchestration control plane. It surfaces real data from the same SQLite state
store and Redis coordination layer used by the CLI, TUI, and daemon.

There is no synthetic simulation in the production path. Every metric, run, and
activity event is sourced from actual daemon state.

---

## Component Map

```
┌────────────────────────────────────────────────────────────┐
│  Browser  /dashboard/                                       │
│  ─────────────────────────────────────────────────────     │
│  vanilla JS + Canvas  (no build step required)              │
│  • polling every 5 s (configurable via data-poll attr)      │
│  • SSE stream at /api/v1/dashboard/events (primary)         │
│  • fallback to polling on SSE failure                       │
└──────────────────────┬─────────────────────────────────────┘
                       │  HTTP + SSE
┌──────────────────────▼─────────────────────────────────────┐
│  Observability Server  (:8080)                              │
│  ─────────────────────────────────────────────────────     │
│  existing: /health  /queue  /metrics  /ready  /gate        │
│  new:       /dashboard/  (static files, embedded)           │
│             /api/v1/dashboard/*  (dashboard API)            │
└──────────────────────┬─────────────────────────────────────┘
                       │  SQL
┌──────────────────────▼─────────────────────────────────────┐
│  SQLite State Store  (WAL mode)                             │
│  • branch_states      review_runs     findings              │
│  • delivery_events    precommit_reviews                     │
└────────────────────────────────────────────────────────────┘
```

---

## Package Layout

```
internal/
  dashboard/
    doc.go            package-level documentation
    models.go         JSON request/response types
    queries.go        SQL query helpers (no HTTP concerns)
    handlers.go       HTTP handlers for all dashboard API routes
    settings_store.go JSON-file-backed settings persistence
    static_embed.go   //go:embed static
    static/
      index.html      single-file SPA (vanilla JS, no build required)
    dashboard_test.go full test coverage (27 tests)

  daemon/
    observability.go  mounts dashboard routes + static file serving
```

---

## API Endpoints

All dashboard-specific endpoints live under `/api/v1/dashboard/`.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/dashboard/overview` | Today's run count, pass rate, blocked count, avg gate time, 7d sparkline |
| GET | `/api/v1/dashboard/repos` | Repo list with latest branch, state, last-run info, gate pills |
| GET | `/api/v1/dashboard/node-repos` | Discovered git repos on the node with connected/orphan status |
| GET | `/api/v1/dashboard/runs` | 50 most recent review runs |
| GET | `/api/v1/dashboard/activity` | 50 most recent delivery events |
| GET | `/api/v1/dashboard/block-reasons` | Ranked error sources from findings |
| GET | `/api/v1/dashboard/gate-health` | Per-provider pass rates |
| GET | `/api/v1/dashboard/active-sessions` | Currently active agent sessions with heartbeat/context data |
| GET | `/api/v1/dashboard/assignments` | Agent assignment list with state, PR, and timing info |
| GET | `/api/v1/dashboard/assignments/{id}` | Single assignment detail |
| GET | `/api/v1/dashboard/agent-events` | Agent session event stream |
| GET | `/api/v1/dashboard/agents` | Per-agent stats aggregated over last 30 days |
| GET | `/api/v1/dashboard/agents/{id}/sessions` | Recent sessions for a specific agent |
| GET | `/api/v1/dashboard/sessions` | Session list with filtering/pagination |
| GET | `/api/v1/dashboard/sessions/{id}` | Single session detail |
| GET | `/api/v1/dashboard/sessions/{id}/tail` | Live tail of session output |
| GET | `/api/v1/dashboard/sessions/metrics/{id}` | Session token metrics (context pressure/observability) |
| GET | `/api/v1/dashboard/tasks` | Task list for the Tasks tab |
| GET | `/api/v1/dashboard/pipeline` | Pipeline view with active assignment cards |
| GET | `/api/v1/dashboard/scorecard` | Phase 1F scorecard with gate pass rate, cycle time, compliance |
| GET | `/api/v1/dashboard/archives` | Session archives for TUI/dashboard parity |
| GET | `/api/v1/dashboard/tracking-config` | Agent tracking config (disabled agents, env vars) |
| PUT | `/api/v1/dashboard/tracking-config` | Toggle agent tracking or update env vars |
| GET | `/api/v1/dashboard/compliance` | Compliance rules and per-assignment checks |
| GET | `/api/v1/dashboard/compliance/rules` | Compliance rule definitions |
| GET | `/api/v1/dashboard/compliance/checks/{id}` | Per-assignment compliance checks |
| GET | `/api/v1/dashboard/compliance/violations` | Compliance violation list |
| GET | `/api/v1/dashboard/gate-checks` | Latest gate-check report with per-check results |
| GET | `/api/v1/dashboard/gate/{action}` | Gate actions (trigger, status, cancel) |
| GET | `/api/v1/dashboard/merge/{action}` | Merge actions routed by sub-path |
| GET | `/api/v1/dashboard/queue` | Current review queue |
| GET | `/api/v1/dashboard/queue/stats` | Queue statistics |
| GET | `/api/v1/dashboard/health` | DB connectivity, feed freshness, security score, ETA |
| GET | `/api/v1/dashboard/settings` | Integrations + gate pipeline config |
| PUT | `/api/v1/dashboard/settings` | Validated settings update (persisted, audited) |
| GET | `/api/v1/dashboard/settings/gate-config` | Gate config key-value pairs |
| GET | `/api/v1/dashboard/settings/gate-config/{key}` | Individual gate config value |
| GET | `/api/v1/dashboard/settings/repo-config/{repo}` | Per-repo configuration |
| GET | `/api/v1/dashboard/feedback/history` | Feedback history for sessions |
| POST | `/api/v1/dashboard/feedback/{id}` | Submit feedback for a session |
| POST | `/api/v1/dashboard/chat` | LiteLLM-backed review-process assistant for the live dashboard |
| POST | `/api/v1/chat/ask` | Alias for `/api/v1/dashboard/chat` |
| GET | `/api/v1/chat/history` | Chat conversation list |
| GET | `/api/v1/chat/history/{id}` | Single conversation history |
| POST | `/api/v1/dashboard/manual-review-upload` | Drag/drop file upload for manual review |
| GET | `/api/v1/dashboard/events` | SSE stream of live delivery events |

Existing observability endpoints (`/health`, `/queue`, `/metrics`, `/gate`) are
**unchanged**.

---

## Data Sources

| Dashboard widget | SQL table(s) |
|------------------|--------------|
| Runs today / pass rate | `review_runs` |
| Blocked count | `branch_states WHERE state='blocked'` |
| Avg gate time | `review_runs.started_at / finished_at` |
| 7d sparkline | `review_runs` GROUP BY day |
| Repos sidebar | `branch_states` (latest per repo) |
| Runs table | `review_runs` |
| Activity feed | `delivery_events` |
| Block reasons | `findings WHERE severity='error'` |
| Gate health | `review_runs` GROUP BY provider |

---

## Parity with TUI and `/gate` Endpoint

| Concern | TUI | `/gate` endpoint | Dashboard |
|---------|-----|------------------|-----------|
| Gate progress (live run) | live progress bar | `PROGRESS_BAR` field | reads same progress.env via `/gate` endpoint |
| Branch state machine | displays state | n/a | shows `state` from `branch_states` |
| Queue ordering | WFQ priority | `/queue` endpoint | n/a (separate concern) |
| Findings/blockers | n/a | n/a | `findings` table |

The dashboard does **not** bypass state-machine transitions. No dashboard action
writes to `branch_states` directly; all writes go through the existing
`state.TransitionBranch()` contract.

---

## Settings Persistence

Dashboard settings are stored in `<data_dir>/dashboard-settings.json` alongside
the SQLite state database.

Writes are atomic: the file is written to `dashboard-settings.json.tmp` then
renamed over the target. Partial writes cannot corrupt the stored state.

Settings changes are logged at INFO level with `event_type=dashboard_settings_updated`.

**What is persisted:**
- Integration `connected` status (per integration ID)
- Gate pipeline `enabled` and `blocks_commit` toggles (per gate name)

**What is NOT persisted (read from daemon config only):**
- Gate `timeout_sec` and `provider` (authoritative in `codero.yaml`)

---

## Manual Upload Flow

1. User drops a file onto the runs table area in the browser.
2. Client validates extension (`.py .ts .go .js .diff .patch .rb .java`) and rejects immediately if invalid.
3. `POST /api/v1/dashboard/manual-review-upload` is called with `multipart/form-data`.
4. Server re-validates extension and size (max 10 MiB).
5. A `review_runs` row is inserted with `provider='manual'` and `status='pending'`.
6. The returned `run_id` appears in the runs table on the next poll/refresh.

There is no arbitrary file execution. The uploaded file is read and discarded;
only the metadata (name, repo, branch derivation) is persisted.

---

## Realtime Architecture

- **Primary:** SSE stream at `/api/v1/dashboard/events` polls `delivery_events` every 2 s.
- **Fallback:** If SSE connection drops, client falls back to full-page polling at 5 s.
- **Live badge** shows `live` (green), `degraded` (amber on SSE fallback), or `offline` (red on API failure).

---

## Security Properties

- No secrets, tokens, or credentials are served through the dashboard API.
- The chat assistant is advisory only. It may explain and recommend, but it does not execute actions or mutate state.
- The assistant is scoped to the review process only: queue, gate checks, findings, activity, and merge readiness.
- Upload endpoint validates file type and size; file content is not executed or persisted to disk.
- Settings writes are validated before persistence; invalid updates return 422 with a descriptive error.
- CORS headers allow `*` for local development; in production the dashboard is served from the same origin as the API.
