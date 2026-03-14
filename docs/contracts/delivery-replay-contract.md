# Contract: Delivery Replay (DR-001)

Status: implemented
Sprint: 5
Package: `internal/delivery`

## Purpose

Define the semantics of the append-only delivery stream and the replay API.
The stream is the primary mechanism for delivering review findings and system
events to agents and operators.

## Core Invariants

1. **Append-only**: events are never modified or deleted after insertion.
2. **Monotonic seq**: every event has a `seq` number that is strictly greater
   than all previously assigned seqs for the same `repo+branch`. Seq numbers
   may have gaps (crash between Redis INCR and SQLite INSERT).
3. **Durable source of truth**: SQLite is the authoritative event log; Redis
   seq counter is coordination only and is re-seeded on daemon start.
4. **Idempotent replay**: calling `Replay(repo, branch, sinceSeq)` repeatedly
   with the same arguments returns the same events. Replay does not re-run
   reviews or re-deliver to agents.
5. **Repo-qualified identity**: every event is scoped to `repo + branch`.

## Seq Counter Coordination

| Layer  | Role |
|---|---|
| Redis `codero:<repo>:seq:<branch>` | `INCR`-based fast-path assignment |
| SQLite `delivery_events.seq` | Durable floor; source after Redis loss |

### Startup Recovery

On daemon start (or after Redis flush), `Stream.InitSeqFloor(ctx, repo, branch)`
reads `MAX(seq)` from `delivery_events` and uses a Lua `SET-if-lower` script
to ensure the Redis counter is at or above the durable floor. This prevents
seq regression after Redis restart.

### Gap Tolerance

Consumers of the delivery stream **must** tolerate seq gaps. A gap does not
indicate a missing event — it indicates a crash between INCR and INSERT.
The correct consumer pattern is:

```go
events, err := stream.Replay(ctx, repo, branch, lastSeenSeq)
// process events in order; update lastSeenSeq = events[len-1].Seq
```

## Event Types

| EventType          | Payload struct              | When emitted |
|---|---|---|
| `finding_bundle`   | `FindingBundlePayload`      | Review completes with findings |
| `system`           | `SystemPayload`             | Lease expiry, retry, session expiry |
| `state_transition` | `StateTransitionPayload`    | Key state changes |

### FindingBundlePayload

```json
{
  "run_id":   "uuid",
  "provider": "stub",
  "findings": [
    {
      "severity":  "warning",
      "category":  "security",
      "file":      "main.go",
      "line":      42,
      "message":   "SQL injection risk",
      "source":    "coderabbit",
      "timestamp": "2026-01-15T12:00:00Z",
      "rule_id":   "S001"
    }
  ]
}
```

### SystemPayload

```json
{
  "reason":  "lease_expired",
  "details": "retry_count=2 error=context deadline exceeded"
}
```

## Replay API

### Go

```go
events, err := stream.Replay(ctx, repo, branch, sinceSeq)
// Returns events with seq > sinceSeq, ordered by seq ASC.
// sinceSeq=0 returns all events.
```

### CLI (planned Sprint 6)

```
codero replay --repo owner/repo --branch feat/x --since 42
```

Returns all delivery events with seq > 42, in seq order.

## Compaction (future)

When compaction is implemented, the durable seq floor must be written to
SQLite first and to Redis second. Recovery uses `MAX(durable_floor, redis_counter)`.
Compaction must never delete events below the highest seq that any active
consumer has acknowledged.

## Security

- Raw source code is not included in delivery payloads.
- Findings contain only normalized metadata (file, line, message, severity).
- The delivery stream does not contain secrets, tokens, or credentials.
