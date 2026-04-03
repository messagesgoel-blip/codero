# OpenClaw State Query Contract (OCL-010)

**Endpoint:** `GET /api/v1/openclaw/state`  
**Method:** GET  
**Auth:** None (internal network only)  
**Content-Type:** `application/json`

## Purpose

Returns a structured JSON snapshot of all Codero system state, designed for
OpenClaw consumption. Replaces the markdown-based `assembleChatContextMarkdown`
with a machine-parseable format.

## Response Schema

```json
{
  "sessions": [
    {
      "session_id": "string",
      "agent_id": "string",
      "repo": "string",
      "branch": "string",
      "pr_number": 0,
      "owner_agent": "string",
      "activity_state": "string",
      "task": { "id": "string", "title": "string", "phase": "string" },
      "started_at": "2026-01-01T00:00:00Z",
      "last_heartbeat_at": "2026-01-01T00:00:00Z",
      "elapsed_sec": 0
    }
  ],
  "pipeline": [
    {
      "session_id": "string",
      "assignment_id": "string",
      "task_id": "string",
      "agent_id": "string",
      "repo": "string",
      "branch": "string",
      "pr_number": 0,
      "state": "string",
      "substatus": "string",
      "checkpoint": "string",
      "version": 0,
      "started_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z",
      "stage_sec": 0
    }
  ],
  "activity": [
    {
      "seq": 0,
      "repo": "string",
      "branch": "string",
      "event_type": "string",
      "payload": "string",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ],
  "gate_health": {
    "providers": [
      {
        "provider": "string",
        "total": 0,
        "passed": 0,
        "pass_rate": 0.0
      }
    ],
    "summary": "string"
  },
  "scorecard": {
    "gate_pass_rate": "string",
    "avg_cycle_time": "string",
    "merge_rate": "string",
    "compliance_score": "string",
    "summary": "string"
  },
  "generated_at": "2026-01-01T00:00:00Z"
}
```

## Section Details

### `sessions` (array)
Active sessions with repo/branch context (WIRE-001 data). Empty array if no
active sessions. Includes inferred_status, context_pressure, and IO metrics
when available.

### `pipeline` (array)
Current pipeline cards showing session → assignment → branch flow. Includes
`pr_number` from branch_states (WIRE-003 data). Empty array if no active
pipeline entries.

### `activity` (array)
Last 20 delivery events ordered by recency. Includes event type and payload
for each event.

### `gate_health` (object)
Per-provider pass rates computed from review_runs (WIRE-002 data). The
`summary` field provides a human-readable aggregate.

### `scorecard` (object)
Proving period metrics: gate pass rate, merge rate, compliance score, and a
narrative summary. Based on precommit_reviews and branch_states data.

## Guarantees

- All array fields are always present (never null; empty `[]` if no data).
- All object fields are always present with default values.
- Response is generated fresh on each request (no caching).
- Query errors for individual sections are logged but do not fail the endpoint;
  the affected section returns empty/default values.

## Error Responses

| Status | Meaning |
|--------|---------|
| 405    | Method not allowed (only GET accepted) |
| 500    | Internal error (all sections failed; unlikely) |
