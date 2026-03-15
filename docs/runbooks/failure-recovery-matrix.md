# Failure & Recovery Matrix

**Purpose:** Define expected failure behaviors, detection signals, and recovery procedures for codero runtime components.

**Scope:** Phase 1 runtime components: Redis, SQLite, daemon, lease manager, webhook receiver, dispatch queue.

**Related runbooks:**
- `docs/runbooks/sprint6-hardening-matrix.md` - End-to-end hardening tests
- `docs/runbooks/webhook-outage-polling-fallback.md` - Polling fallback procedures
- `docs/contracts/observability-v1.md` - Detection signals and metrics

---

## Failure Scenarios

| ID | Failure scenario | Impacted component | Detection signal/metric | Expected recovery behavior | Operator action | Test/simulation hook | Status |
| --- | --- | --- | --- | --- | --- | --- | --- |
| FR-001 | Redis restart mid-dispatch | Queue, lease manager | `redis_reconnects` counter, queue empty signal | Rebuild queue and slot state from durable SQLite store; resume dispatch automatically | Monitor `/health` for Redis status; verify queue repopulated | `scripts/test-redis-restart.sh` | defined |
| FR-002 | Redis unavailable at startup | Daemon | Startup exit code, `REDIS_UNAVAILABLE` error | Daemon does not start; exits with named error | Fix Redis availability; restart daemon | `scripts/test-redis-startup-failure.sh` | defined |
| FR-003 | Daemon crash mid-review (SIGKILL) | Lease manager, branch state | Orphaned `cli_reviewing` branches, stale lease keys | On restart, audit durable `cli_reviewing` state against Redis lease keys; repair inconsistencies | None required; automatic repair | `scripts/test-sigkill-recovery.sh` | defined |
| FR-004 | Lease expiry without heartbeat | Lease manager, runner | `lease_expiry` transitions in `state_transitions` | Terminate hung review path; increment retry count; requeue branch; append system bundle | Check `/queue` for requeued branches; verify retry count | `scripts/test-lease-expiry.sh` | defined |
| FR-005 | Webhook outage / delivery failure | Webhook receiver, reconciler | `webhook_delivery_failures` counter, polling fallback active | Automatic fallback to polling mode (60s interval); continue operation | Monitor `/health` webhook status; check polling logs | `scripts/test-webhook-outage.sh` | defined |
| FR-006 | Queue stall / starvation | Dispatch queue | `queue_stalled` event, all items at `max_retries` | Dispatch halts; emit `queue_stalled` event for operator intervention | Investigate blocked items; `codero record-event --type queue_stall`; manual requeue or resolution | `scripts/test-queue-stall.sh` | defined |
| FR-007 | Duplicate webhook delivery | Webhook receiver | `webhook_duplicates_dropped` counter | Drop via Redis NX fast path; secondary durable idempotency check in `webhook_deliveries` table | None required; automatic deduplication | `scripts/test-webhook-duplicate.sh` | defined |
| FR-008 | SIGTERM graceful shutdown | Daemon | Graceful shutdown log, drained count | Stop accepting new submissions; drain in-flight work up to grace period; exit cleanly | None required; monitor shutdown logs | `kill -TERM $PID` | defined |

---

## Detection Signals

| Signal | Source | Metric/Log |
| --- | --- | --- |
| Redis unavailable | `/health` endpoint | `redis_status: unavailable` |
| Redis reconnect | Internal counter | `redis_reconnects_total` |
| Lease expiry | `state_transitions` table | `trigger = 'lease_expired'` |
| Queue stall | `proving_events` table | `event_type = 'queue_stall'` |
| Webhook dedup | `webhook_deliveries` table | `processed = 1` |
| Daemon crash | `branch_states` audit | Orphaned `cli_reviewing` state |

---

## Recovery Procedures

### FR-001: Redis Restart Mid-Dispatch

1. Monitor `/health` endpoint for Redis reconnection
2. Verify queue state via `/queue` endpoint
3. Check logs for queue rebuild confirmation
4. No manual intervention required

### FR-002: Redis Unavailable at Startup

1. Check daemon logs for `REDIS_UNAVAILABLE` error
2. Verify Redis process status
3. Fix Redis availability (start service, fix config)
4. Restart codero daemon

### FR-003: SIGKILL Recovery

1. Restart daemon
2. Monitor logs for lease audit and repair
3. Verify no orphaned `cli_reviewing` branches remain
4. Check `/queue` for requeued items

### FR-004: Lease Expiry

1. Automatic recovery; no immediate action
2. Review `state_transitions` for lease expiry pattern
3. Investigate root cause if recurring
4. Record via `codero record-event` if systemic

### FR-005: Webhook Outage

1. Automatic fallback to polling mode
2. Monitor logs for polling activity
3. Restore webhook receiver when possible
4. Verify resumption of webhook delivery

### FR-006: Queue Stall

1. Inspect blocked items via `codero queue`
2. Investigate root cause (external dependency, config)
3. Resolve or requeue items manually
4. Record event via `codero record-event --type queue_stall`

---

## Drill Schedule

Phase 1 sign-off requires explicit drills for all scenarios:

| Drill | Frequency | Owner | Sign-off |
| --- | --- | --- | --- |
| Redis restart | Weekly during proving period | operator | ___ |
| SIGKILL recovery | Weekly during proving period | operator | ___ |
| Lease expiry | Monthly during proving period | operator | ___ |
| Webhook outage | Monthly during proving period | operator | ___ |
| Queue stall | Ad-hoc, simulate as needed | operator | ___ |

---

## References

- Appendix G: Failure and recovery contract (`docs/roadmaps/codero-roadmap-v5.md`)
- Sprint 6 hardening matrix (`docs/runbooks/sprint6-hardening-matrix.md`)
- Observability contract (`docs/contracts/observability-v1.md`)