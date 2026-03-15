# Proving Scorecard Contract

**Contract ID:** PROVING-001  
**Status:** active  
**Owner:** codero  
**Version:** 1.0.0

## Overview

This contract defines the schema and semantics of the Phase 1F proving period scorecard. The scorecard provides daily evidence of operational health and phase exit gate compliance.

## Output Schema

```json
{
  "generated_at": "2026-03-15T12:00:00Z",
  "period_start": "2026-02-13T00:00:00Z",
  "period_end": "2026-03-15T12:00:00Z",
  "branches_reviewed_7_days": 15,
  "branches_reviewed_by_repo": {
    "owner/repo1": 8,
    "owner/repo2": 7
  },
  "stale_detections_30_days": 2,
  "lease_expiry_recoveries_30_days": 1,
  "precommit_reviews_7_days": 45,
  "precommit_reviews_by_repo": {
    "owner/repo1": 25,
    "owner/repo2": 20
  },
  "missed_feedback_deliveries": 0,
  "queue_stall_incidents_30_days": 0,
  "unresolved_thread_failures_30_days": 0,
  "manual_db_repairs_30_days": 0
}
```

## Field Semantics

| Field | Type | Description | Window |
|-------|------|-------------|--------|
| `generated_at` | RFC3339 timestamp | When the scorecard was computed | - |
| `period_start` | RFC3339 timestamp | Start of the measurement window | 30 days ago |
| `period_end` | RFC3339 timestamp | End of the measurement window | now |
| `branches_reviewed_7_days` | integer | Count of unique branches with completed reviews | 7 days |
| `branches_reviewed_by_repo` | object | Branch counts keyed by repo | 7 days |
| `stale_detections_30_days` | integer | Branches transitioned to `stale_branch` state | 30 days |
| `lease_expiry_recoveries_30_days` | integer | Lease expiry events with recovery (T07) | 30 days |
| `precommit_reviews_7_days` | integer | Total pre-commit review attempts (both passes) | 7 days |
| `precommit_reviews_by_repo` | object | Pre-commit counts keyed by repo | 7 days |
| `missed_feedback_deliveries` | integer | Explicitly recorded missed delivery events | 30 days |
| `queue_stall_incidents_30_days` | integer | Queue stall events requiring intervention | 30 days |
| `unresolved_thread_failures_30_days` | integer | Unresolved thread check failures | 30 days |
| `manual_db_repairs_30_days` | integer | Manual DB repair incidents | 30 days |

## Data Sources

| Metric | Source | Query |
|--------|--------|-------|
| branches_reviewed | `review_runs` table | <code>SELECT COUNT(DISTINCT repo \|\| '/' \|\| branch) FROM review_runs WHERE started_at >= ? AND status = 'completed'</code> |
| stale_detections | `state_transitions` | <code>SELECT COUNT(*) FROM state_transitions WHERE to_state = 'stale_branch' AND created_at >= ?</code> |
| lease_expiry_recoveries | `state_transitions` | <code>SELECT COUNT(*) FROM state_transitions WHERE trigger = 'lease_expired' AND created_at >= ?</code> |
| precommit_reviews | `precommit_reviews` table | <code>SELECT COUNT(*), repo FROM precommit_reviews WHERE created_at >= ? GROUP BY repo</code> |
| missed_feedback | `proving_events` table | <code>SELECT COUNT(*) FROM proving_events WHERE event_type = 'missed_delivery' AND created_at >= ?</code> |
| queue_stalls | `proving_events` table | <code>SELECT COUNT(*) FROM proving_events WHERE event_type = 'queue_stall' AND created_at >= ?</code> |
| unresolved_failures | `proving_events` table | <code>SELECT COUNT(*) FROM proving_events WHERE event_type = 'unresolved_thread_failure' AND created_at >= ?</code> |
| manual_repairs | `proving_events` table | <code>SELECT COUNT(*) FROM proving_events WHERE event_type = 'manual_db_repair' AND created_at >= ?</code> |

## Snapshot Persistence

Snapshots are stored in the `proving_snapshots` table with:
- `snapshot_date` (UNIQUE): YYYY-MM-DD format
- `scorecard_json`: Full JSON representation

Additionally, snapshots may be written to a configurable directory as JSON files for external archival.

## CLI Usage

```bash
# Human-readable scorecard
codero scorecard

# JSON output
codero scorecard --output json

# Save snapshot to database
codero scorecard --save

# Save snapshot to file
codero scorecard --save --snapshot-dir /var/lib/codero/snapshots
```

## Recording Events

```bash
# Record a queue stall
codero record-event --type queue_stall --repo owner/repo

# Record a manual DB repair
codero record-event --type manual_db_repair --repo owner/repo

# Record an unresolved thread failure
codero record-event --type unresolved_thread_failure --repo owner/repo --details '{"branch": "feature/x"}'
```

## Rollback Notes

If this contract changes:
1. Historical snapshots remain valid (they store JSON)
2. Query functions in `internal/state/proving.go` must be updated
3. CLI output may need migration for backward compatibility
4. Downgrade migration `000003_proving_period.down.sql` removes all tracking tables

## Phase 1 Exit Gate Thresholds

Per `docs/roadmaps/codero-roadmap-v5.md`:

| Metric | Threshold |
|--------|-----------|
| branches_reviewed_7_days | ≥ 3 per week |
| stale_detections_30_days | observed (any count) |
| lease_expiry_recoveries_30_days | ≥ 1 observed |
| precommit_reviews_7_days | ≥ 10 per project per week |
| manual_db_repairs_30_days | = 0 |
| missed_feedback_deliveries | = 0 |
| queue_stall_incidents_30_days | = 0 |

**Status indicators:**
- `ON TRACK`: All critical metrics at target
- `IN PROGRESS`: Metrics collected, accumulating evidence
- `NEEDS ATTENTION`: Any critical failures recorded