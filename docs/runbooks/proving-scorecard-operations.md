# Runbook: Proving Period Scorecard Operations

## Overview

This runbook describes how to run the daily proving period scorecard for Phase 1F sign-off evidence collection.

**Related Contract:** `docs/contracts/proving-scorecard-contract.md`  
**Related Roadmap:** `docs/roadmaps/codero-roadmap-v5.md` (Section 1F)

---

## Daily Operations

### Generate Daily Scorecard

Run the scorecard daily to collect and review proving period metrics:

```bash
# Human-readable output
codero scorecard

# JSON output (for scripts/automation)
codero scorecard --output json

# Save snapshot to database
codero scorecard --save
```

### Save Snapshot for Sign-off Evidence

To persist daily snapshots for the 30-day sign-off record:

```bash
# Save to database only
codero scorecard --save

# Save to database AND file
codero scorecard --save --snapshot-dir /var/lib/codero/snapshots
```

Snapshots are stored:
- In SQLite: `proving_snapshots` table (one row per day)
- Optional file: `{snapshot_dir}/YYYY-MM-DD.json`

---

## Recording Operational Events

Some events must be explicitly recorded:

### Queue Stall Event

When the dispatch queue stalls (all eligible items blocked):

```bash
codero record-event --type queue_stall --repo owner/repo
```

### Manual DB Repair

After any manual database repair:

```bash
codero record-event --type manual_db_repair --repo owner/repo --details '{"reason": "corrupted index"}'
```

### Unresolved Thread Failure

When the unresolved thread cross-check fails:

```bash
codero record-event --type unresolved_thread_failure --repo owner/repo --details '{"branch": "feature/x", "thread_count": 2}'
```

### Missed Delivery

When a feedback delivery is confirmed missed:

```bash
codero record-event --type missed_delivery --repo owner/repo --details '{"branch": "feature/y", "expected_seq": 123}'
```

---

## Interpreting Results

### Thresholds (from roadmap)

| Metric | Target | Status |
|--------|--------|--------|
| branches_reviewed_7_days | ≥ 3 | Count of unique branches with completed reviews |
| stale_detections_30_days | ≥ 2 | Must observe stale detection working |
| lease_expiry_recoveries_30_days | ≥ 1 | Must observe recovery working |
| precommit_reviews_7_days | ≥ 10/project | Pre-commit enforcement active |
| manual_db_repairs_30_days | = 0 | **CRITICAL** - must be zero |
| missed_feedback_deliveries | = 0 | **CRITICAL** - must be zero |
| queue_stall_incidents_30_days | = 0 | Should be zero (investigate if not) |

### Status Indicators

The scorecard computes a status:

- **ON TRACK**: All critical metrics at zero, accumulation in progress
- **IN PROGRESS**: Collecting evidence, not yet at thresholds
- **NEEDS ATTENTION**: Any critical failure recorded (manual DB repair or missed delivery)

---

## Sign-off Checklist Template

At the end of the 30-day proving period, complete this checklist:

```markdown
## Phase 1 Exit Gate Sign-off

**Period:** YYYY-MM-DD to YYYY-MM-DD
**Operator:** [name]
**Date:** YYYY-MM-DD

### Checklist

- [ ] 30 consecutive days of daily use completed
- [ ] At least 2 active repositories tracked
- [ ] Minimum thresholds achieved:
  - [ ] ≥ 3 branches reviewed per week (average)
  - [ ] ≥ 2 stale detections observed
  - [ ] ≥ 1 lease-expiry recovery observed
  - [ ] ≥ 10 pre-commit reviews per project per week
- [ ] Zero incidents of:
  - [ ] Manual DB repairs
  - [ ] Missed feedback deliveries
  - [ ] Silent queue stalls
  - [ ] Undetected stale branches
- [ ] Recovery drills passed:
  - [ ] Redis restart recovery
  - [ ] Daemon restart recovery
  - [ ] SIGKILL aftermath recovery
  - [ ] Duplicate webhook delivery handling
- [ ] Pre-commit loops enforced by hook (not policy alone)

### Evidence

Snapshots stored in:
- Database: proving_snapshots table
- Files: [path to snapshot files, if applicable]

### Exceptions

[List any exceptions or notes]

### Sign-off

- Operator: ________________ Date: ________
- Reviewed by: ________________ Date: ________
```

---

## Troubleshooting

### Scorecard shows empty or zero values

**Cause:** No data collected yet, or fresh database.

**Solution:** Wait for review activity, or verify `review_runs`, `state_transitions` tables have data.

### "Branches reviewed" count seems wrong

**Cause:** Count is based on `review_runs.status = 'completed'`.

**Solution:** Verify review runs are being created with correct status.

### Pre-commit reviews not tracked

**Cause:** Pre-commit tracking requires explicit recording. The `commit-gate` command should be updated to record reviews.

**Solution:** Record pre-commit reviews via code hooks or manually:
```bash
# After a pre-commit review
codero record-precommit --repo owner/repo --branch feature/x --provider litellm --status passed
```

### Snapshot save fails

**Cause:** Database migration not run, or write permissions.

**Solution:**
1. Check migration: `SELECT * FROM schema_migrations;`
2. Run migrations: `codero daemon` (applies on startup)
3. Check database permissions

---

## Automated Daily Collection

For automated daily snapshot collection, add to cron:

```bash
# /etc/cron.d/codero-scorecard
0 8 * * * codero /usr/local/bin/codero scorecard --save --output json > /var/log/codero/scorecard.log 2>&1
```

This runs at 08:00 UTC daily and saves the snapshot to the database.

---

## Related Documents

- `docs/roadmaps/codero-roadmap-v5.md` - Phase 1F requirements
- `docs/runbooks/sprint6-hardening-matrix.md` - Recovery drills
- `docs/contracts/proving-scorecard-contract.md` - Scorecard schema
- `docs/contracts/delivery-replay-contract.md` - Delivery semantics