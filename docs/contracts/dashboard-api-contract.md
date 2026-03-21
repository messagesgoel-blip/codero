# Dashboard API Contract

Version: v1
Base path: `/api/v1/dashboard/`
Content-Type: `application/json` (all endpoints)

---

## GET /api/v1/dashboard/overview

Returns today's aggregate metrics and 7-day sparkline data.

### Response `200 OK`

```json
{
  "runs_today":    14,
  "pass_rate":     85.7,
  "blocked_count": 2,
  "avg_gate_sec":  42.3,
  "sparkline_7d": [
    { "date": "2026-03-10", "total": 8,  "passed": 6, "failed": 2 },
    { "date": "2026-03-11", "total": 12, "passed": 10, "failed": 2 }
  ],
  "generated_at": "2026-03-16T10:00:00Z"
}
```

**Fields:**
- `pass_rate`: 0–100 float; `-1` when no runs exist today
- `avg_gate_sec`: wall-clock seconds for completed runs; `-1` when no data
- `sparkline_7d`: one entry per day with data in the past 7 days (may be sparse)

---

## GET /api/v1/dashboard/repos

Returns the latest branch state summary per tracked repository.

### Response `200 OK`

```json
{
  "repos": [
    {
      "repo":            "acme/api",
      "branch":          "feat/rate-limit",
      "state":           "cli_reviewing",
      "head_hash":       "a3f9c2",
      "last_run_status": "completed",
      "last_run_at":     "2026-03-16T09:55:00Z",
      "updated_at":      "2026-03-16T09:55:00Z",
      "gate_summary": [
        { "name": "litellm",    "status": "pass" },
        { "name": "coderabbit", "status": "idle" }
      ]
    }
  ],
  "generated_at": "2026-03-16T10:00:00Z"
}
```

**`gate_summary[].status` values:** `pass`, `fail`, `run`, `idle`

---

## GET /api/v1/dashboard/runs

Returns the 50 most recent review runs, newest first.

### Response `200 OK`

```json
{
  "runs": [
    {
      "id":          "a3f9c2b1-...",
      "repo":        "acme/api",
      "branch":      "feat/rate-limit",
      "head_hash":   "abc123",
      "provider":    "litellm",
      "status":      "completed",
      "started_at":  "2026-03-16T09:54:00Z",
      "finished_at": "2026-03-16T09:54:42Z",
      "manual":      false,
      "created_at":  "2026-03-16T09:54:00Z"
    }
  ],
  "generated_at": "2026-03-16T10:00:00Z"
}
```

**`status` values:** `pending`, `running`, `completed`, `failed`
**`manual`:** `true` for uploads via the drag/drop upload flow

---

## GET /api/v1/dashboard/activity

Returns the 50 most recent delivery events, newest first.

### Response `200 OK`

```json
{
  "events": [
    {
      "seq":        42,
      "repo":       "acme/api",
      "branch":     "feat/rate-limit",
      "event_type": "state_transition",
      "payload":    "{\"from_state\":\"queued_cli\",\"to_state\":\"cli_reviewing\"}",
      "created_at": "2026-03-16T09:54:01Z"
    }
  ],
  "generated_at": "2026-03-16T10:00:00Z"
}
```

---

## GET /api/v1/dashboard/block-reasons

Returns ranked error sources from the findings table (all time).

### Response `200 OK`

```json
{
  "reasons": [
    { "source": "semgrep",  "count": 14 },
    { "source": "gitleaks", "count": 9  }
  ],
  "generated_at": "2026-03-16T10:00:00Z"
}
```

---

## GET /api/v1/dashboard/gate-health

Returns per-provider pass rates across all runs.

### Response `200 OK`

```json
{
  "gates": [
    { "provider": "coderabbit", "total": 20, "passed": 18, "pass_rate": 90.0 },
    { "provider": "litellm",    "total": 15, "passed": 12, "pass_rate": 80.0 }
  ],
  "generated_at": "2026-03-16T10:00:00Z"
}
```

**`pass_rate`:** 0–100 float; `-1` when `total == 0`

---

## GET /api/v1/dashboard/gate-checks

Returns the latest canonical gate-check report envelope used to render the per-step
status matrix in the UI. The embedded `report` payload follows
`docs/contracts/gate-check-schema-v1.md`.

### Response `200 OK`

```json
{
  "report_path": ".codero/gate-check/last-report.json",
  "report": {
    "summary": {
      "overall_status": "pass",
      "passed": 4,
      "failed": 0,
      "skipped": 5,
      "infra_bypassed": 0,
      "disabled": 4,
      "total": 13,
      "required_failed": 0,
      "required_disabled": 0,
      "profile": "portable",
      "schema_version": "1"
    },
    "checks": [
      {
        "id": "file-size",
        "name": "File size limit",
        "group": "format",
        "required": true,
        "enabled": true,
        "status": "skip",
        "reason_code": "not_in_scope",
        "reason": "no staged files",
        "duration_ms": 0
      }
    ],
    "run_at": "2026-03-18T14:40:34Z"
  },
  "generated_at": "2026-03-18T14:40:34Z"
}
```

**`report`** may be `null` until the first gate-check run completes.
When `report` is `null`, the response also includes a human-readable `message` and the
resolved `report_path` so the UI can explain the missing data.
**UI rule:** render the ordered `report.checks[]` list directly; do not infer per-step
state from freeform log text.

---

## GET /api/v1/dashboard/active-sessions

Returns the currently active review sessions for the GUI. A session appears here only
while its heartbeat is fresh; expired or inactive sessions MUST be excluded.
Deduplication by `session_id` is applied **before** the page-size limit so callers
always receive the first N *unique* sessions, not the first N rows.

Session registration starts with **`session_id` + `agent_id` only**. Assignment context
(`repo`, `branch`, `worktree`, `task_id`) is attached later when the agent claims or is
assigned work. The dashboard API reflects this: sessions may appear with empty repo/branch
until an assignment is attached.

Task context is resolved from the assignment `task_id` when present; otherwise the
`feat/PROJ-NNN-description` branch pattern is used (e.g. `feat/COD-056-fix-auth`).
When neither is available, the `task` field is omitted entirely (`omitempty`) — callers
must render missing task context gracefully and must not expect a JSON `null` value.

`owner_agent` is retained for backward compatibility with the current dashboard/TUI
assumptions. It mirrors `agent_id` and should not be treated as an independent identity.

### Response `200 OK`

```json
{
  "active_count": 2,
  "sessions": [
    {
      "session_id": "sess-123",
      "agent_id": "agent-007",
      "owner_agent": "agent-007",
      "mode": "cli",
      "repo": "acme/api",
      "branch": "feat/COD-055-fix-finish-loop",
      "worktree": "/worktrees/codero/wt-1",
      "pr_number": 42,
      "activity_state": "active",
      "task": {
        "id": "COD-055",
        "title": "fix finish loop",
        "phase": "review in progress"
      },
      "started_at": "2026-03-18T14:10:00Z",
      "last_heartbeat_at": "2026-03-18T14:40:30Z",
      "elapsed_sec": 1830
    },
    {
      "session_id": "sess-456",
      "agent_id": "agent-ops-3",
      "owner_agent": "agent-ops-3",
      "mode": "tui",
      "repo": "",
      "branch": "",
      "pr_number": 0,
      "activity_state": "waiting",
      "started_at": "2026-03-18T14:30:00Z",
      "last_heartbeat_at": "2026-03-18T14:40:28Z",
      "elapsed_sec": 628
    }
  ],
  "generated_at": "2026-03-18T14:40:34Z"
}
```

**`activity_state` values:** `active`, `waiting`, `blocked` (legacy)

**Session rules:**
- Only fresh sessions are returned; stale sessions are filtered out.
- Dedupe by `session_id` is applied **before** the page-size limit.
- Sessions are considered **active** while `agent_sessions.ended_at IS NULL`; ended
  sessions are removed from this feed. `end_reason` is internal state and may appear
  in future responses but is not surfaced today.
- Registration begins with `session_id` + `agent_id` only. Repo/branch/worktree/task
  may be empty until an assignment is attached.
- `task` is omitted entirely when neither `task_id` nor a matching branch pattern exists.
  Clients MUST render a missing `task` field gracefully — typically by showing `branch`
  instead — and must not depend on a JSON `null`.
- `owner_agent` mirrors `agent_id` for dashboard/TUI parity; treat it as a legacy alias.
- The response MUST NOT expose secrets, tokens, raw prompts, or file contents.

---

## GET /api/v1/dashboard/health

Returns dashboard-level health signals: database connectivity, freshness of the
active-sessions and gate-checks data feeds, the count of live agent sessions, and
a generation timestamp. Use this endpoint to populate the "System Health" bar in the
Processes tab.

The gate-checks report path uses the **same resolution logic** as
`GET /api/v1/dashboard/gate-checks`: honours `CODERO_GATE_CHECK_REPORT_PATH` and
falls back to the compiled-in default path when the variable is unset.

### Response `200 OK`

```json
{
  "database": { "status": "ok" },
  "feeds": {
    "active_sessions": {
      "status": "ok",
      "last_refresh": "2026-03-18T14:40:30Z",
      "freshness_sec": 4
    },
    "gate_checks": {
      "status": "stale",
      "last_refresh": "2026-03-18T14:10:00Z",
      "freshness_sec": 1830
    }
  },
  "active_agent_count": 3,
  "stale_session_count": 1,
  "expired_session_count": 0,
  "reconciliation_status": "stale",
  "generated_at": "2026-03-18T14:40:34Z"
}
```

**`database.status` values:** `ok`, `down`

**`feeds.*.status` values:**
- `ok` — last refresh within 5 minutes
- `stale` — last refresh older than 5 minutes
- `unavailable` — no data found (empty DB or missing report file)

**Rules:**
- Database health is determined by a lightweight `PING` against the configured store.
- `feeds.active_sessions.freshness_sec` is derived from the most recent
  `agent_sessions.last_seen_at` heartbeat (legacy fallback: `branch_states.owner_session_last_seen`).
- `feeds.gate_checks.freshness_sec` is derived from the mod-time of the last
  gate-check report file, resolved via `CODERO_GATE_CHECK_REPORT_PATH` (same as
  the gate-checks endpoint).
- `stale_session_count` counts sessions with last heartbeat older than 5 minutes
  but not yet expired.
- `expired_session_count` counts sessions with last heartbeat older than
  `SessionHeartbeatTTL`.
- `reconciliation_status` values: `ok`, `stale`, `attention`, `unavailable`.
- The response MUST NOT expose secrets, tokens, raw prompts, or file contents.
- This endpoint is read-only and idempotent.

---

## GET /api/v1/dashboard/settings

Returns current integration connection status and gate pipeline configuration.

### Response `200 OK`

```json
{
  "integrations": [
    {
      "id":        "coderabbit",
      "name":      "CodeRabbit",
      "desc":      "AI code review on PRs",
      "connected": true
    }
  ],
  "gate_pipeline": [
    {
      "name":          "gitleaks",
      "enabled":       true,
      "blocks_commit": true,
      "timeout_sec":   30,
      "provider":      "built-in"
    }
  ],
  "generated_at": "2026-03-16T10:00:00Z"
}
```

---

## PUT /api/v1/dashboard/settings

Updates integration and gate pipeline settings. Changes are validated and
persisted atomically. The updated settings are returned in the response.

### Request body

```json
{
  "integrations": [
    { "id": "coderabbit", "name": "CodeRabbit", "desc": "...", "connected": true }
  ],
  "gate_pipeline": [
    { "name": "gitleaks", "enabled": true, "blocks_commit": true, "timeout_sec": 30, "provider": "built-in" }
  ]
}
```

Fields are optional — omit either array to leave it unchanged.

### Response `200 OK` — updated settings (same schema as GET)

### Response `400 Bad Request` — malformed JSON

```json
{ "error": "invalid JSON: ...", "code": "parse_error" }
```

### Response `422 Unprocessable Entity` — validation failure

```json
{ "error": "gate pipeline: timeout_sec for \"gitleaks\" must be 0–3600", "code": "validation_error" }
```

**Validation rules:**
- Gate `name` must not be empty
- Gate `timeout_sec` must be 0–3600
- Integration `id` must not be empty

---

## POST /api/v1/dashboard/manual-review-upload

Accepts a file for manual review. Creates a `review_runs` row with
`provider=manual` and `status=pending`.

### Request

`Content-Type: multipart/form-data`

| Field | Required | Description |
|-------|----------|-------------|
| `file` | yes | File to review |
| `repo` | no | Target repo slug (defaults to `manual`) |

**Allowed extensions:** `.py .ts .go .js .diff .patch .rb .java`
**Max size:** 10 MiB

### Response `202 Accepted`

```json
{
  "run_id":  "a3f9c2b1-...",
  "repo":    "acme/api",
  "branch":  "manual/fix-auth.go",
  "status":  "pending",
  "message": "manual review queued for fix-auth.go"
}
```

### Response `422 Unprocessable Entity` — invalid file

```json
{ "error": "unsupported file type \".exe\"; allowed: .py .ts .go ...", "code": "invalid_file" }
```

---

## GET /api/v1/dashboard/events  (SSE)

Server-Sent Events stream of live delivery events. Emits `activity` events
as new rows appear in `delivery_events`.

### Response — `text/event-stream`

```
: connected seq=42

event: activity
data: {"seq":43,"repo":"acme/api","branch":"main","event_type":"state_transition","payload":"{...}","created_at":"2026-03-16T10:00:01Z"}
```

The stream starts from the current tip (no historical events are replayed).
Clients should fall back to polling the `/activity` endpoint if the SSE
connection fails.

---

## Error Envelope

All error responses use a consistent envelope:

```json
{
  "error": "human-readable message",
  "code":  "machine_readable_code"
}
```

**Error codes:**

| Code | Meaning |
|------|---------|
| `db_error` | Database query failed |
| `settings_error` | Settings load/save failed |
| `parse_error` | Request body could not be parsed |
| `validation_error` | Request body failed validation |
| `missing_file` | Upload missing `file` field |
| `invalid_file` | File type or size not allowed |
| `read_error` | Failed to read request body or upload |
| `sse_unsupported` | Server does not support SSE (no Flusher) |
