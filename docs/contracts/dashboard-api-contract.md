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
