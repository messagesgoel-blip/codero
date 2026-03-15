# Observability Contract (v1)

**Contract ID:** OBS-001  
**Version:** 1.0  
**Status:** binding  
**Applies to:** Phase 1 runtime

This document defines the mandatory observability surfaces for codero runtime.

---

## Required Endpoints

| Endpoint | Method | Contract |
| --- | --- | --- |
| `/health` | GET | JSON health with uptime, durable-store status, Redis status, webhook receiver status |
| `/queue` | GET | JSON snapshot of queue positions and live scores |
| `/metrics` | GET | Prometheus metrics format |
| `/api/v1/agent-metrics` | GET | JSON effectiveness metrics per agent and project |

### `/health` Response Schema

```json
{
  "uptime_seconds": 12345,
  "status": "ok|degraded",
  "redis": {
    "status": "connected|unavailable",
    "latency_ms": 2.5,
    "reconnects": 0
  },
  "database": {
    "status": "ok|error",
    "path": "/var/lib/codero/codero.db"
  },
  "webhook": {
    "status": "enabled|disabled|polling",
    "last_event": "2026-03-15T12:00:00Z"
  }
}
```

### `/queue` Response Schema

```json
{
  "repo": "owner/repo",
  "items": [
    {
      "branch": "feature/x",
      "priority": 15,
      "wait_seconds": 300,
      "state": "queued_cli",
      "retry_count": 0
    }
  ]
}
```

### `/metrics` Format

Prometheus text format with the following required metrics:

---

## Required Metrics

### Core Metrics

| Metric name | Type | Description | Labels |
| --- | --- | --- | --- |
| `codero_branch_states_total` | gauge | Current count of branches per state | `state` |
| `codero_queue_depth` | gauge | Number of items in queue | `repo` |
| `codero_review_runs_total` | counter | Completed review runs | `repo`, `provider`, `status` |
| `codero_state_transitions_total` | counter | State transitions | `from_state`, `to_state`, `trigger` |

### Redis Metrics

| Metric name | Type | Description | Labels |
| --- | --- | --- | --- |
| `codero_redis_latency_seconds` | histogram | Redis operation latency | `operation` |
| `codero_redis_reconnects_total` | counter | Number of Redis reconnects | - |
| `codero_redis_errors_total` | counter | Redis errors | `error_type` |

### Pre-commit Metrics

| Metric name | Type | Description | Labels |
| --- | --- | --- | --- |
| `codero_precommit_wait_seconds` | histogram | Pre-commit slot wait time | `repo` |
| `codero_precommit_pass_total` | counter | Passed pre-commit reviews | `repo`, `provider` |
| `codero_precommit_fail_total` | counter | Failed pre-commit reviews | `repo`, `provider` |

### Delivery Metrics

| Metric name | Type | Description | Labels |
| --- | --- | --- | --- |
| `codero_delivery_latency_seconds` | histogram | Time from dispatch to feedback | `repo` |
| `codero_missed_deliveries_total` | counter | Missed feedback deliveries | `repo` |

### Lease Metrics

| Metric name | Type | Description | Labels |
| --- | --- | --- | --- |
| `codero_lease_expiry_total` | counter | Lease expirations | `repo` |
| `codero_lease_recovery_total` | counter | Lease recoveries | `repo` |

---

## Required Labels

All metrics must include these labels where applicable:

| Label | Description | Example |
| --- | --- | --- |
| `repo` | Repository identifier | `owner/api` |
| `branch` | Branch name | `feature/x` |
| `queue` | Queue name | `default` |
| `status` | Operation status | `completed`, `failed` |
| `provider` | Review provider | `litellm`, `coderabbit` |

---

## Success/Failure Semantics

### Health Status Determination

- **OK:** Redis connected, database accessible, no active queue stalls
- **Degraded:** Redis reconnected recently (within 60s), or webhook in polling fallback mode

### Queue Stall Detection

- A queue is **stalled** when all eligible queued items have `retry_count >= max_retries`
- Stall emits `queue_stalled` event to `proving_events` table

### Missed Delivery Detection

- A delivery is **missed** when feedback is not received within expected SLA
- Missed deliveries increment `codero_missed_deliveries_total`

---

## SLOs (Phase 1 Proving)

| SLO | Target |
| --- | --- |
| Zero missed feedback deliveries | 0 |
| Zero silent queue stalls | 0 |
| Zero undetected stale branches | 0 |
| Redis p99 latency | < 10ms |

---

## Versioning

This contract follows semantic versioning (MAJOR.MINOR).

Changes to this contract require:
1. Update version number in header
2. Document changes in changelog
3. Notify all consumers of breaking changes
4. Maintain backward compatibility within MAJOR version

---

## Related Documents

- Appendix F: Observability contract (`docs/roadmaps/codero-roadmap-v5.md`)
- Health endpoint implementation (`internal/daemon/observability.go`)
- Metrics exports (`internal/daemon/prometheus.go`)
- Sprint 6 hardening matrix (`docs/runbooks/sprint6-hardening-matrix.md`)