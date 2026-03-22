# Runbook: Proving Period Scorecard Operations

## Overview

This runbook describes how to run the daily proving period scorecard for Phase 1F sign-off evidence collection.

**Related Contract:** `docs/contracts/proving-scorecard-contract.md`  
**Related Roadmap:** `docs/roadmaps/archive/codero-roadmap-v5.md` (Section 1F)

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

**Cause:** No `commit-gate` runs have completed, or runs completed before auto-recording was available.

**Solution:** Pre-commit reviews are now recorded **automatically** by `codero commit-gate` on every terminal result. No manual action is required in normal flow.

- `commit-gate` writes one idempotent record per provider (`copilot`, `litellm`) on PASS or FAIL.
- Records use `INSERT OR IGNORE` with a deterministic ID (`pc-{run_id}-{provider}`), so polling retries or re-runs with the same run ID do not create duplicates.
- If the state DB is unavailable, a warning is printed to stderr and the gate exit behavior is unaffected.

If you need to backfill records from a run that predates auto-recording, use the manual command:
```bash
# Manual backfill only — not required in normal flow
codero record-precommit --repo owner/repo --branch feature/x --provider copilot --status passed
codero record-precommit --repo owner/repo --branch feature/x --provider litellm --status passed
```

### Snapshot save fails

**Cause:** Database migration not run, or write permissions.

**Solution:**
1. Check migration: `SELECT * FROM schema_migrations;`
2. Run migrations: `codero daemon` (applies on startup)
3. Check database permissions

---

## Preflight Checker

Run before any evidence collection to verify shared tools and hook enforcement across all managed repos:

```bash
codero preflight
```

Exit codes:
- `0` — all checks pass
- `1` — one or more checks failed

Checks performed:
- Shared tools available at `/srv/storage/shared/tools/bin`: `semgrep`, `gitleaks`, `pre-commit`, `poetry`, `ruff`
- Gate heartbeat binary present: `/srv/storage/shared/agent-toolkit/bin/gate-heartbeat`
- Pre-commit hook enforced on every repo in `docs/managed-repos.txt`

### Remediation Paths

| Failure | Cause | Fix |
|---------|-------|-----|
| Tool missing | Shared venv not installed | `cd /srv/storage/shared && poetry install` |
| Heartbeat binary missing | Toolkit not installed | `ls /srv/storage/shared/agent-toolkit/bin/` — check toolkit path |
| Hook missing/stale | `.githooks/pre-commit` absent or not executable | `bash scripts/review/install-pre-commit-all.sh` then re-run preflight |
| Hook not symlink | Local hook instead of shared toolkit | Delete `.githooks/pre-commit`, then run the install script |

---

## Automated Daily Collection

Use the new `daily-snapshot` command instead of the manual cron approach. It includes preflight gating, idempotency, timestamped logging, and retention.

```bash
# One-off manual run
codero daily-snapshot --snapshot-dir ~/.codero/snapshots

# Verify today's snapshot exists
codero daily-snapshot --verify-only --snapshot-dir ~/.codero/snapshots

# Custom retention (default: 45 days)
codero daily-snapshot --snapshot-dir ~/.codero/snapshots --retain-days 60
```

### Install with cron (recommended)

```bash
crontab -e
# Paste from scripts/automation/codero-daily.cron
```

The cron template at `scripts/automation/codero-daily.cron` configures a 02:05 daily run with per-day log rotation.

### Install with systemd timer

```bash
cp scripts/automation/codero-daily.service ~/.config/systemd/user/
cp scripts/automation/codero-daily.timer ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now codero-daily.timer
systemctl --user list-timers codero-daily.timer
```

### Verify automation is working

```bash
# Check today's snapshot in DB
codero daily-snapshot --verify-only

# Check file output
ls -lh ~/.codero/snapshots/

# Check logs
cat ~/.codero/logs/daily-snapshot-$(date +%Y%m%d).log
```

### Rollback

DB rows in `proving_snapshots` are never deleted (audit trail). To remove an erroneous record:

```sql
DELETE FROM proving_snapshots WHERE snapshot_date = '2026-01-15';
```

Snapshot files are managed by retention (default 45 days). Delete manually if needed:

```bash
rm ~/.codero/snapshots/proving-snapshot-2026-01-15.json
```

---

## Exit-Gate Tracker

Monitor Phase 1F readiness and compute gaps to close:

```bash
# Human-readable table (default)
codero exit-gate

# JSON for automation/CI
codero exit-gate --output json

# Markdown block for evidence pasting
codero exit-gate --output markdown
```

### Sample output

```
Phase 1F Exit-Gate Status
══════════════════════════════════════════════════════
 CRITERION                         REQUIRED  VALUE     STATUS
──────────────────────────────────────────────────────
 Consecutive days ≥ 30             yes       12        NOT_READY  gap: need 18 more days
 Active repos ≥ 2                  yes       3         READY
 Branches reviewed/week ≥ 3        yes       4         READY
 Stale detections ≥ 2 (30d)        yes       1         NOT_READY  gap: need 1 more detection
 Lease expiry recoveries ≥ 1       yes       2         READY
 Precommit reviews ≥ 10/repo/wk    yes       11        READY
 Manual DB repairs = 0             yes       0         READY
 Missed deliveries = 0             yes       0         READY
 Queue stall incidents = 0         no        0         READY
 Unresolved gate failures = 0      no        0         READY
──────────────────────────────────────────────────────
 Overall: NOT_READY  (2 required criteria unmet)
══════════════════════════════════════════════════════
Gaps to close:
  • consecutive_days_gte_30: need 18 more days
  • stale_detections_gte_2: need 1 more detection
```

### Weekly operator workflow

```bash
# Generate and commit weekly report
codero exit-gate --output markdown >> docs/runbooks/proving-evidence-2026-03.md
git add docs/runbooks/proving-evidence-2026-03.md
git commit -m "chore: weekly Phase 1F exit-gate snapshot $(date +%Y-%m-%d)"
```

---

## Related Documents

- `docs/roadmaps/archive/codero-roadmap-v5.md` - Phase 1F requirements
- `docs/runbooks/sprint6-hardening-matrix.md` - Recovery drills
- `docs/contracts/proving-scorecard-contract.md` - Scorecard schema
- `docs/contracts/delivery-replay-contract.md` - Delivery semantics