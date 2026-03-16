# Runbook: Sprint 6 Hardening Matrix

## Overview

This document provides executable validation steps for all Sprint 6 failure and recovery scenarios. Each scenario includes preconditions, fault injection methods, expected behavior, and rollback steps.

**Related Roadmap Section**: `docs/roadmaps/codero-roadmap-v5.md` Appendix G — Failure and Recovery Contract

---

## Scenario Matrix

| ID | Scenario | Code Path | Test Command |
|---|---|---|---|
| HM-001 | Redis unavailable at daemon start | `internal/daemon/redis.go` | `go test ./internal/daemon -run TestRedisUnavailableAtStartup` |
| HM-002 | Redis interruption during runtime | `internal/scheduler/queue.go` | `go test ./tests/integration -run TestIntegration_RedisRestart` |
| HM-003 | Webhook disabled mode (polling-only) | `internal/webhook/reconciler.go` | `go test ./tests/integration -run TestIntegration_PollingOnlyMode` |
| HM-004 | Lease expiry transition (cli_reviewing -> queued_cli) | `internal/scheduler/expiry.go` | `go test ./tests/integration -run TestIntegration_LeaseExpiry` |
| HM-005 | Queue stall detection | `internal/scheduler/stall.go` | `go test ./internal/scheduler -run TestQueueStall` |
| HM-006 | Delivery replay after restart | `internal/delivery/delivery.go` | `go test ./tests/integration -run TestIntegration_DeliveryReplay` |
| HM-007 | Runner failure path | `internal/runner/runner.go` | `go test ./internal/runner -run TestRunnerFailure` |
| HM-008 | Stale branch on HEAD mismatch | `internal/webhook/reconciler.go` | `go test ./tests/integration -run TestIntegration_StaleBranch` |
| HM-009 | Abandoned transition and reactivate | `internal/scheduler/expiry.go` | `go test ./tests/integration -run TestIntegration_Abandoned` |
| HM-010 | Merge-ready recompute guardrails | `internal/state/repository.go` | `go test ./tests/integration -run TestIntegration_MergeReady` |

---

## HM-001: Redis Unavailable at Daemon Start

### Description
Daemon must fail-fast with named error if Redis is unavailable at startup.

### Preconditions
- Redis server NOT running
- codero daemon not running

### Trigger / Fault Injection
```bash
# Stop Redis
redis-cli SHUTDOWN NOSAVE 2>/dev/null || true

# Attempt to start daemon
./codero daemon --config codero.yaml
```

### Expected Behavior
- Daemon exits with non-zero status
- Log contains `REDIS_UNAVAILABLE` or equivalent named error
- No partial state corruption

### Pass Criteria
```bash
# Check exit code is non-zero
echo $?  # Should be != 0

# Check logs for named error
grep -i "redis" /var/log/codero/daemon.log || cat codero.log
```

### Rollback / Reset
```bash
# Start Redis
redis-server --daemonize yes

# Verify Redis is available
redis-cli PING
```

### Related Endpoints
- N/A (daemon fails before HTTP server starts)

### Related Code
- `internal/daemon/redis.go:CheckConnection`
- `internal/daemon/lifecycle.go`

---

## HM-002: Redis Interruption During Runtime

### Description
When Redis becomes unavailable after startup, in-flight work continues, new dispatches halt, and recovery is automatic.

### Preconditions
- Daemon running with Redis connected
- At least one branch in `queued_cli` state

### Trigger / Fault Injection
```bash
# Simulate Redis interruption
redis-cli DEBUG SLEEP 30  # Blocks for 30 seconds
# OR
redis-cli SHUTDOWN NOSAVE
```

### Expected Behavior
- In-flight reviews continue
- New lease acquisitions fail gracefully (logged, not crashed)
- `/health` endpoint reports Redis status as degraded
- On Redis recovery, queue operations resume automatically

### Pass Criteria
```bash
# Check health endpoint shows degraded
curl -s http://localhost:8080/health | jq .redis_status
# Expected: "degraded" or "unavailable"

# After Redis restart, verify recovery
redis-server --daemonize yes
sleep 5
curl -s http://localhost:8080/health | jq .redis_status
# Expected: "healthy"
```

### Rollback / Reset
```bash
# Restart Redis
redis-server --daemonize yes

# Verify queue state preserved in SQLite
sqlite3 /var/lib/codero/codero.db "SELECT COUNT(*) FROM branch_states WHERE state = 'queued_cli'"
```

### Related Endpoints
- `GET /health` — returns Redis status
- `GET /queue` — queue snapshot (may be stale)

### Related Code
- `internal/scheduler/queue.go`
- `internal/daemon/observability.go`

---

## HM-003: Webhook Disabled Mode (Polling-Only)

### Description
Daemon operates correctly without webhooks. Reconciler polls GitHub every 60 seconds.

### Preconditions
- `webhook.enabled: false` in config
- No webhook receiver configured in GitHub

### Trigger / Fault Injection
```bash
# Ensure webhook disabled in config
cat codero.yaml | grep -A2 "webhook:"
# webhook:
#   enabled: false

# Start daemon
./codero daemon --config codero.yaml
```

### Expected Behavior
- Reconciler runs every 60 seconds (polling-only interval)
- Branch state transitions occur via polling
- No webhook-related errors in logs

### Pass Criteria
```bash
# Check reconciler is running
curl -s http://localhost:8080/health | jq .reconciler_status
# Expected: "running"

# Check webhook status
curl -s http://localhost:8080/health | jq .webhook_status
# Expected: "disabled"

# Verify logs show reconciliation events
grep "reconciler" /var/log/codero/daemon.log | tail -5
```

### Rollback / Reset
```bash
# To enable webhooks, update config and restart
# webhook:
#   enabled: true
./codero daemon --config codero.yaml
```

### Related Endpoints
- `GET /health` — shows webhook status
- `GET /queue` — queue visibility

### Related Code
- `internal/webhook/reconciler.go`
- `docs/runbooks/webhook-outage-polling-fallback.md`

---

## HM-004: Lease Expiry Transition (cli_reviewing -> queued_cli)

### Description
When a review lease expires, the branch transitions back to `queued_cli` with `retry_count` incremented.

### Preconditions
- Branch in `cli_reviewing` state
- Lease TTL has expired (default 30s)

### Trigger / Fault Injection
```bash
# Insert test branch in cli_reviewing state with expired lease
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states 
SET state = 'cli_reviewing', 
    lease_id = 'test-lease', 
    lease_expires_at = datetime('now', '-1 minute')
WHERE branch = 'test-branch';"

# Trigger lease audit cycle
# (Lease audit runs every 30s automatically)
sleep 35
```

### Expected Behavior
- Branch transitions `cli_reviewing -> queued_cli`
- `retry_count` incremented by 1
- System event appended to delivery stream
- Branch re-enqueued in Redis

### Pass Criteria
```bash
# Check branch state
sqlite3 /var/lib/codero/codero.db "
SELECT state, retry_count FROM branch_states WHERE branch = 'test-branch';"
# Expected: queued_cli, retry_count = previous + 1

# Check delivery events for lease_expired event
sqlite3 /var/lib/codero/codero.db "
SELECT event_type, payload FROM delivery_events 
WHERE branch = 'test-branch' 
ORDER BY seq DESC LIMIT 1;"
# Expected: event_type = 'system', payload contains 'lease_expired'
```

### Rollback / Reset
```bash
# Reset branch to original state
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states 
SET state = 'queued_cli', 
    retry_count = 0,
    lease_id = NULL,
    lease_expires_at = NULL
WHERE branch = 'test-branch';"
```

### Related Endpoints
- `GET /queue` — should show re-queued branch

### Related Code
- `internal/scheduler/expiry.go:auditExpiredLease`
- `internal/scheduler/lease.go`

---

## HM-005: Queue Stall Detection

### Description
When all eligible queued items are exhausted or blocked, `queue_stalled` event fires.

### Preconditions
- All branches in `blocked` state (retry_count >= max_retries)
- No branches available for dispatch

### Trigger / Fault Injection
```bash
# Set all branches to blocked state
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states SET state = 'blocked' WHERE state IN ('queued_cli', 'cli_reviewing');"

# Wait for stall detection (runs during dispatch cycle)
sleep 15
```

### Expected Behavior
- Dispatch halts
- `queue_stalled` event logged
- Operator intervention required

### Pass Criteria
```bash
# Check logs for stall detection
grep "queue_stalled" /var/log/codero/daemon.log || grep "stall" /var/log/codero/daemon.log

# Check metrics endpoint
curl -s http://localhost:8080/metrics | grep queue_stalled
```

### Rollback / Reset
```bash
# Release a blocked branch (DB update + re-enqueue)
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states SET state = 'queued_cli', retry_count = 0 WHERE state = 'blocked' LIMIT 1;"

# Enqueue the released branch in Redis
redis-cli ZADD "codero:owner_repo:queue:pending" 0 "<branch-name>"
```

### Related Endpoints
- `GET /metrics` — contains queue_stalled counter
- `GET /queue` — shows queue status

### Related Code
- `internal/scheduler/stall.go`
- `internal/runner/runner.go`

---

## HM-006: Delivery Replay Correctness After Restart

### Description
After daemon restart, delivery seq counter is re-seeded from durable floor. Replay returns all events.

### Preconditions
- Several delivery events exist
- Seq floor > 0 in SQLite

### Trigger / Fault Injection
```bash
# Note current seq
sqlite3 /var/lib/codero/codero.db "SELECT MAX(seq) FROM delivery_events;"

# Stop daemon
kill $(cat /var/run/codero/codero.pid)

# Flush Redis (simulate restart)
redis-cli FLUSHALL

# Restart daemon
./codero daemon --config codero.yaml
```

### Expected Behavior
- Daemon reads durable seq floor on startup
- Redis counter re-initialized to durable floor
- New events get seq > durable floor (no regression)

### Pass Criteria
```bash
# Verify seq counter >= durable floor
DURABLE_FLOOR=$(sqlite3 /var/lib/codero/codero.db "SELECT MAX(seq) FROM delivery_events;")
REDIS_SEQ=$(redis-cli GET "codero:owner_repo:seq:test-branch" || echo "0")

echo "Durable floor: $DURABLE_FLOOR, Redis seq: $REDIS_SEQ"
# Expected: REDIS_SEQ >= DURABLE_FLOOR

# Trigger new event and verify seq > floor
# (via any state transition or review)
```

### Rollback / Reset
```bash
# No rollback needed - this is recovery behavior
```

### Related Endpoints
- `GET /api/v1/events?since=N` — replay endpoint

### Related Code
- `internal/delivery/delivery.go:InitSeqFloor`
- `docs/contracts/delivery-replay-contract.md`

---

## HM-007: Runner Failure Path

### Description
When a review fails, the branch transitions appropriately based on retry_count.

### Preconditions
- Branch in `cli_reviewing` state
- Provider configured to fail

### Trigger / Fault Injection
```bash
# Use stub provider with artificial delay (0 = immediate)
# In test: runner.NewStubProvider(0) returns canned findings

# To simulate failure, use errorProvider in tests:
# type errorProvider struct{ err error }
# func (e *errorProvider) Review(...) (*ReviewResponse, error) { return nil, e.err }

# Or misconfigure a real provider
./codero daemon --config codero-bad-provider.yaml
```

### Expected Behavior
- `retry_count` incremented
- If `retry_count < max_retries`: transition to `queued_cli` (T07)
- If `retry_count >= max_retries`: transition to `blocked` (T16)
- System event appended

### Pass Criteria
```bash
# Check branch state
sqlite3 /var/lib/codero/codero.db "
SELECT state, retry_count FROM branch_states WHERE branch = 'test-branch';"

# Check review_runs for failure record
sqlite3 /var/lib/codero/codero.db "
SELECT status, error FROM review_runs WHERE branch = 'test-branch' ORDER BY created_at DESC LIMIT 1;"
# Expected: status = 'failed'
```

### Rollback / Reset
```bash
# Clear error and reset
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states SET state = 'queued_cli', retry_count = 0 WHERE branch = 'test-branch';"
```

### Related Endpoints
- `GET /api/v1/agent-metrics` — shows failure counts

### Related Code
- `internal/runner/runner.go:handleReviewFailure`

---

## HM-008: Stale Branch Handling on HEAD Mismatch

### Description
When HEAD hash changes (force-push), branch transitions to `stale_branch`.

### Preconditions
- Branch registered with known HEAD
- HEAD hash changed in GitHub

### Trigger / Fault Injection
```bash
# Update HEAD in GitHub (force-push)
# Then trigger reconciliation

# Or manually simulate:
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states SET head_hash = 'old-hash-12345' WHERE branch = 'test-branch';"

# Reconciler will detect mismatch and transition to stale_branch
sleep 65  # Wait for reconciler cycle
```

### Expected Behavior
- Branch transitions to `stale_branch` (T12)
- Event logged with reason

### Pass Criteria
```bash
# Check branch state
sqlite3 /var/lib/codero/codero.db "
SELECT state FROM branch_states WHERE branch = 'test-branch';"
# Expected: stale_branch

# Check transition audit
sqlite3 /var/lib/codero/codero.db "
SELECT from_state, to_state, trigger FROM state_transitions 
WHERE branch_state_id = (SELECT id FROM branch_states WHERE branch = 'test-branch')
ORDER BY created_at DESC LIMIT 1;"
# Expected: trigger = 'head_mismatch' or 'stale_detected'
```

### Rollback / Reset
```bash
# Agent re-submits with new HEAD (T13)
./codero submit --repo owner/repo --branch test-branch --head new-hash-67890
```

### Related Endpoints
- `GET /api/v1/branches` — shows stale branches

### Related Code
- `internal/webhook/reconciler.go`

---

## HM-009: Abandoned Transition on Heartbeat TTL and Reactivate

### Description
Branch transitions to `abandoned` when session heartbeat expires (1800s). Can be reactivated.

### Preconditions
- Branch in active state
- `owner_session_last_seen` > 1800 seconds ago

### Trigger / Fault Injection
```bash
# Set last seen to past
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states 
SET owner_session_last_seen = datetime('now', '-1900 seconds')
WHERE branch = 'test-branch';"

# Trigger session expiry check
sleep 65  # Wait for session expiry cycle (60s interval)
```

### Expected Behavior
- Branch transitions to `abandoned` (T14)
- System event appended
- Queue slot freed

### Pass Criteria
```bash
# Check branch state
sqlite3 /var/lib/codero/codero.db "
SELECT state FROM branch_states WHERE branch = 'test-branch';"
# Expected: abandoned
```

### Reactivate Path
```bash
# Operator reactivates branch (T15)
./codero reactivate --repo owner/repo --branch test-branch

# Verify state
sqlite3 /var/lib/codero/codero.db "
SELECT state, retry_count FROM branch_states WHERE branch = 'test-branch';"
# Expected: queued_cli, retry_count = 0
```

### Rollback / Reset
```bash
# Already covered by reactivate path
```

### Related Endpoints
- `POST /api/v1/branches/{id}/reactivate`

### Related Code
- `internal/scheduler/expiry.go:expireSession`
- `internal/scheduler/expiry.go:SessionHeartbeatTTL`

---

## HM-010: Merge-Ready Recompute Guardrails

### Description
`merge_ready` is computed only when ALL conditions are met:
- `approved = true`
- `ci_green = true`
- `pending_events = 0`
- `unresolved_threads = 0`

### Preconditions
- Branch in `reviewed` state

### Trigger / Fault Injection
```bash
# Set all conditions for merge_ready
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states 
SET approved = 1, 
    ci_green = 1, 
    pending_events = 0, 
    unresolved_threads = 0
WHERE branch = 'test-branch';"

# Trigger recompute (via reconciler or watch tick)
sleep 65
```

### Expected Behavior
- Branch transitions to `merge_ready` (T10)
- If any condition changes, reverts to `reviewed` or `coding` (T11)

### Pass Criteria
```bash
# Check branch state
sqlite3 /var/lib/codero/codero.db "
SELECT state, approved, ci_green, pending_events, unresolved_threads 
FROM branch_states WHERE branch = 'test-branch';"
# Expected: state = merge_ready, all conditions met

# Remove approval, verify reversion
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states SET approved = 0 WHERE branch = 'test-branch';"
sleep 65
sqlite3 /var/lib/codero/codero.db "SELECT state FROM branch_states WHERE branch = 'test-branch';"
# Expected: reviewed or coding (not merge_ready)
```

### Rollback / Reset
```bash
# Reset conditions
sqlite3 /var/lib/codero/codero.db "
UPDATE branch_states 
SET approved = 0, ci_green = 0, pending_events = 0, unresolved_threads = 0, state = 'reviewed'
WHERE branch = 'test-branch';"
```

### Related Endpoints
- `GET /api/v1/branches` — shows merge_ready status
- `GET /queue` — merge_ready branches visible

### Related Code
- `internal/state/repository.go:UpdateMergeReadiness`
- `docs/roadmaps/codero-roadmap-v5.md` Appendix A, T10

---

## Quick Reference: TUI Commands

| Command | Purpose |
|---|---|
| `codero queue` | Show queue snapshot |
| `codero branch <name>` | Show branch detail |
| `codero events --since N` | Replay events from seq N |
| `codero why` | Show score breakdown |
| `codero reactivate --repo R --branch B` | Reactivate abandoned |
| `codero release --repo R --branch B` | Release blocked |
| `codero gate-status` | Show gate run status (TUI view) |
| `codero gate-status --watch` | Live-polling gate TUI (redraws until PASS/FAIL) |
| `codero gate-status --logs` | Show gate log path and last entries |
| `codero commit-gate` | Run gate and auto-record outcomes to scorecard |

---

## Quick Reference: HTTP Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /health` | Health status (Redis, DB, webhook) |
| `GET /queue` | Queue snapshot with scores |
| `GET /metrics` | Prometheus metrics |
| `GET /api/v1/agent-metrics` | Effectiveness metrics |
| `GET /ready` | Readiness probe |
| `GET /gate` | Current gate progress (same state as TUI — dashboard parity) |

---

## Appendix: State Transition Reference

| Transition | From | To | Trigger |
|---|---|---|---|
| T06 | queued_cli | cli_reviewing | Lease issued |
| T07 | cli_reviewing | queued_cli | Lease expired (retry) |
| T08 | cli_reviewing | reviewed | Review complete |
| T10 | reviewed | merge_ready | All conditions met |
| T11 | merge_ready | reviewed/coding | Condition lost |
| T12 | any active | stale_branch | HEAD mismatch |
| T14 | any active | abandoned | Heartbeat TTL |
| T15 | abandoned | queued_cli | Reactivate |
| T16 | any active | blocked | Max retries |

Full state machine: `docs/roadmaps/codero-roadmap-v5.md` Appendix A