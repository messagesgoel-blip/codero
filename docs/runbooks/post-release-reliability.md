# Post-Release Reliability Gate — Runbook

**Workflow:** `.github/workflows/post-release-reliability.yml`
**Task:** COD-039

---

## Purpose

The post-release reliability gate provides continuous assurance that the
promoted v1.2.0 release (and future releases) remain stable after promotion.
It detects regression, release-record drift, and infrastructure rot before
users or operators encounter failures.

---

## Trigger Cadence

| Trigger | When |
|---|---|
| Scheduled | Daily at **06:00 UTC** (cron: `0 6 * * *`) |
| On-demand | `workflow_dispatch` (see Inputs below) |

The scheduled run uses default inputs (v1.2.0, non-strict). On-demand runs
accept overrides for ad-hoc verification after hotfixes or manual promotions.

---

## Inputs (workflow_dispatch)

| Input | Default | Description |
|---|---|---|
| `release_version` | `v1.2.0` | Version string; selects release record at `docs/runbooks/releases/<version>.yaml` |
| `expected_checksum` | _(empty)_ | SHA-256 override; if set, used instead of `artifact_sha256` from the release record |
| `artifact_path` | _(empty)_ | Binary path override; if set, used instead of `artifact_path` from the release record |
| `strict_mode` | `false` | If `true`, artifact-absent SKIP is converted to FAIL |

---

## Checks

| ID | Name | Description |
|---|---|---|
| R-001 | `prove-gate` | Runs `codero prove --fast` (22-check v1.2 proving gate). FAIL if `overall_status != PASS`. |
| R-002 | `verify-release` | Runs `verify-release.sh --version <version> --fast --json`. FAIL if any check reports FAIL. |
| R-003 | `release-record-valid` | Runs `validate-release-record.sh` against the release record YAML. FAIL if any required field is missing or empty. |
| R-004 | `drift-version` | Compares `version` field in release record against the expected release version. FAIL on mismatch. |
| R-005 | `drift-artifact-sha` | Cross-checks `artifact_sha256` in the release record against `sha256sum` of the artifact binary. SKIP if artifact absent on runner (expected in CI). FAIL if present but SHA mismatches. |

---

## PASS / FAIL / SKIP Semantics

| Status | Meaning |
|---|---|
| **PASS** | Check completed successfully; no issues found. |
| **FAIL** | A real regression or drift was detected. Workflow exits non-zero. |
| **SKIP** | Expected environmental gap: artifact not available on GitHub runner, or daemon not running. Not a regression. |

**Expected SKIPs in every scheduled run:**

- R-005 (`drift-artifact-sha`): The release artifact lives at `/srv/storage/…` which is not mounted on GitHub runners. SHA cross-check is performed locally during promotion drills. This SKIP is always expected and never indicates a regression.
- V-007/V-008/V-009 from `verify-release.sh`: Daemon not running in CI. These are inherited SKIPs counted under R-002's summary.

---

## strict_mode Behavior

When `strict_mode=true` is passed via `workflow_dispatch`:

- R-005 changes from SKIP to FAIL if the artifact binary is absent.
- Use this when verifying a live operator host where the artifact _should_ be present.
- Do **not** use in scheduled CI runs (it will always fail due to missing `/srv/storage/` mount).

---

## Output Artifacts

Every run uploads the following (retained 30 days):

| File | Description |
|---|---|
| `post-release-report.json` | Machine-readable consolidated report (schema below) |
| `post-release-summary.md` | Human-readable markdown table |
| `prove-result.json` | Raw JSON from `codero prove` |
| `prove-human.txt` | Human table from `codero prove` (stderr) |
| `verify-result.json` | Raw JSON from `verify-release.sh` |
| `verify-human.txt` | Human summary from `verify-release.sh` (stderr) |

Artifacts are available under **Actions → post-release-reliability-{run_id}** in the GitHub UI.

---

## Report JSON Schema

```json
{
  "schema_version": "1",
  "release_version": "v1.2.0",
  "timestamp_utc": "2026-03-17T06:00:00Z",
  "overall": "PASS",
  "totals": {
    "pass": 4,
    "fail": 0,
    "skip": 1,
    "total": 5
  },
  "checks": [
    {
      "id": "R-001",
      "name": "prove-gate",
      "status": "PASS",
      "details": "overall=PASS passed=18 skipped=4 failed=0"
    }
  ],
  "drift": {
    "release_record_ok": true,
    "artifact_present": false,
    "checksum_match": null,
    "notes": "Artifact path /srv/storage/... not available on GitHub runner; ..."
  }
}
```

Fields:

| Field | Type | Description |
|---|---|---|
| `schema_version` | string | Always `"1"`. Increment if schema changes. |
| `release_version` | string | The version validated (from input or default). |
| `timestamp_utc` | string | ISO-8601 UTC timestamp of the run. |
| `overall` | string | `PASS` or `FAIL`. |
| `totals.pass/fail/skip/total` | int | Check counts. |
| `checks[].id` | string | Check ID (R-001..R-005). |
| `checks[].name` | string | Check name. |
| `checks[].status` | string | `PASS`, `FAIL`, or `SKIP`. |
| `checks[].details` | string | Free-text detail line. |
| `drift.release_record_ok` | bool | True if release record passed field validation. |
| `drift.artifact_present` | bool | True if artifact binary was found at runtime. |
| `drift.checksum_match` | bool\|null | True/false if artifact present; null if absent. |
| `drift.notes` | string | Human-readable explanation for drift status. |

---

## Triage Steps for Common Failures

### R-001 `prove-gate` FAIL

```bash
# Reproduce locally
cd /srv/storage/repo/codero
git checkout main && git pull
go build -trimpath -o ./bin/codero ./cmd/codero
./bin/codero prove --fast --repo-path .
# Check which C-XXX check failed in the human output
```

Common causes: a breaking change merged to `main`, a test that became flaky.

### R-002 `verify-release` FAIL

```bash
# Reproduce locally
cd /srv/storage/repo/codero
CODERO_BIN=/srv/storage/shared/tools/releases/codero-v1.2.0/codero \
  bash scripts/automation/verify-release.sh \
    --version v1.2.0 --repo-path . --fast --json
# Check verify-human.txt for the failing V-XXX check
```

Common causes: binary version mismatch, `gate-status --json` contract broken.

### R-003 `release-record-valid` FAIL

```bash
bash scripts/automation/validate-release-record.sh \
  docs/runbooks/releases/v1.2.0.yaml
```

Common causes: a required field was removed from the YAML or left empty.

### R-004 `drift-version` FAIL

The `version` field in the release record does not match the expected release version. Edit `docs/runbooks/releases/<version>.yaml` to correct the field, or pass the correct `release_version` input.

### R-005 `drift-artifact-sha` FAIL (on an operator host)

```bash
# Cross-check manually
sha256sum /srv/storage/shared/tools/releases/codero-v1.2.0/codero
# Compare against artifact_sha256 in docs/runbooks/releases/v1.2.0.yaml
```

If the checksum changed, the binary was modified after promotion. Treat as a security incident: roll back to the known-good artifact and investigate.

---

## Manual On-Demand Dispatch

To run the gate against a specific version or with strict checking:

1. Navigate to **Actions → Post-Release Reliability Gate** in GitHub.
2. Click **Run workflow**.
3. Fill in inputs:
   - `release_version`: e.g. `v1.2.0`
   - `expected_checksum`: leave blank to use the release record value
   - `artifact_path`: leave blank to use the release record value
   - `strict_mode`: `true` if running on an operator host with the artifact present

To dispatch via CLI:

```bash
gh workflow run post-release-reliability.yml \
  --field release_version=v1.2.0 \
  --field strict_mode=false
```

---

## Cross-References

- Release record: `docs/runbooks/releases/v1.2.0.yaml`
- Promotion drill evidence: `docs/runbooks/v1.2.0-test-release-drill.md`
- Technical report: `docs/runbooks/v1.2.0-technical-report.md`
- verify-release.sh: `scripts/automation/verify-release.sh`
- validate-release-record.sh: `scripts/automation/validate-release-record.sh`
- Proving gate workflow: `.github/workflows/proving-gate.yml`
- Post-release verification workflow: `.github/workflows/verify-release.yml`
