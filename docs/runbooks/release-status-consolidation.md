# Release Status Consolidation Runbook

## Purpose

`scripts/automation/release-status.sh` is the single go/no-go surface for Codero release health.
It aggregates outputs from four sources into one canonical JSON report and human summary:

| Source | Script | Checks |
|---|---|---|
| `prove` | `codero prove --fast` | C-001..C-022 (22 proving-gate checks) |
| `verify` | `scripts/automation/verify-release.sh` | V-001..V-010 (post-release verification) |
| `validate` | `scripts/automation/validate-release-record.sh` | RR-001 (release record schema) |
| `drift` | (inline) | DR-V01 (version field), DR-V02 (artifact SHA-256) |
| `dry_run` | `scripts/release/dry-run-patch-release.sh` | DR-001..DR-009 (patch simulation, optional) |

All underlying commands remain independently callable; this script only aggregates.

---

## Command Examples

### Default (prove + verify + validate, human output)

```bash
cd /srv/storage/repo/codero
bash scripts/automation/release-status.sh --version v1.2.0
```

### JSON output (machine-readable)

```bash
bash scripts/automation/release-status.sh \
  --version v1.2.0 \
  --json \
  2>release-status.txt | tee release-status.json
```

### Strict mode (SKIP → FAIL for validate/drift groups)

```bash
bash scripts/automation/release-status.sh \
  --version v1.2.0 \
  --json \
  --strict
```

### Include patch dry-run simulation

```bash
bash scripts/automation/release-status.sh \
  --version v1.2.0 \
  --json \
  --include-dry-run
```

### Full options

```bash
bash scripts/automation/release-status.sh \
  --version v1.2.0 \
  --repo-path /srv/storage/repo/codero \
  --json \
  --strict \
  --include-dry-run
```

---

## JSON Schema Reference

All fields are stable across minor releases. `schema_version` is bumped on breaking changes.

```json
{
  "schema_version": "1",
  "generated_at":   "<ISO-8601 UTC>",
  "target_version": "v1.2.0",
  "overall_status": "PASS | FAIL",
  "totals": {
    "pass":  <int>,
    "fail":  <int>,
    "skip":  <int>,
    "total": <int>
  },
  "sources": {
    "prove": {
      "overall_status": "PASS | FAIL",
      "passed":  <int>,
      "failed":  <int>,
      "skipped": <int>,
      "total":   <int>
    },
    "verify": {
      "overall_status": "PASS | FAIL",
      "passed":  <int>,
      "failed":  <int>,
      "skipped": <int>,
      "total":   <int>
    },
    "validate": {
      "overall_status": "PASS | FAIL | SKIP",
      "record_path":   "<path>"
    },
    "dry_run": null
  },
  "checks": [
    {
      "id":      "C-001",
      "group":   "prove | verify | validate | drift | dry_run",
      "name":    "<check-name>",
      "status":  "pass | fail | skip",
      "details": "<detail string>",
      "source":  "prove | verify | validate | drift | dry_run"
    }
  ],
  "drift": {
    "release_record_ok": true | false,
    "artifact_present":  true | false,
    "checksum_match":    true | false | null,
    "notes":             "<explanation>"
  }
}
```

**Field notes:**

| Field | Description |
|---|---|
| `schema_version` | Always `"1"` for this release; bumped on breaking schema changes |
| `overall_status` | `PASS` if zero FAILs (SKIPs do not fail unless `--strict` applies) |
| `totals` | Aggregate across all enabled sources |
| `sources.*` | Per-source summary; `dry_run` is `null` unless `--include-dry-run` |
| `checks[].group` | Logical group matching the source; `drift` is always included |
| `checks[].status` | Lowercase `pass\|fail\|skip`; normalized from all sub-command outputs |
| `drift.checksum_match` | `null` if artifact not present (not a failure in default mode) |

---

## PASS / FAIL / SKIP Semantics

| Status | Meaning | Effect on `overall_status` |
|---|---|---|
| `pass` | Check succeeded | — |
| `fail` | Check failed | Sets `overall_status=FAIL`; workflow exits non-zero |
| `skip` | Check could not run (env gap, missing artifact) | No effect by default; see strict mode |

### Strict mode (`--strict`)

Strict mode converts `skip` → `fail` for the `validate` and `drift` groups:

| Group | Strict behavior |
|---|---|
| `validate` | SKIP (record absent) → FAIL |
| `drift` | SKIP (artifact absent) → FAIL |
| `prove` | SKIPs remain SKIP (daemon-dependent race checks; expected in CI) |
| `verify` | SKIPs remain SKIP (V-007..V-010; daemon/artifact not in CI) |

Use strict mode on the operator host where the full environment is available.
CI uses default mode (strict=false) because `/srv/storage/` artifact paths are unavailable.

---

## Triage Flow for FAIL / SKIP

### FAIL

1. Identify the failing check by `id` and `group`:
   - `prove` (`C-xxx`): run `codero prove --fast` standalone; examine human output
   - `verify` (`V-xxx`): run `verify-release.sh --version <v>` standalone
   - `validate` (`RR-001`): inspect `docs/runbooks/releases/<version>.yaml` for missing fields
   - `drift` (`DR-V01`): record version mismatch — edit release record to fix `version:` field
   - `drift` (`DR-V02`): SHA mismatch — re-build and re-record artifact SHA, or investigate binary tampering
   - `dry_run` (`DR-xxx`): run `dry-run-patch-release.sh` standalone for full detail

2. Fix root cause.
3. Re-run `release-status.sh` until `overall_status=PASS`.

### SKIP

| Check | SKIP condition | To convert to PASS |
|---|---|---|
| V-007..V-009 | Daemon not running | Pass `--endpoint-url` to `verify-release.sh` directly |
| V-010 | Checksum file absent in releases dir | Run on operator host with `/srv/storage/` mounted |
| RR-001 | Release record file missing | Create `docs/runbooks/releases/<version>.yaml` |
| DR-V02 | Artifact not at `artifact_path` | Run on operator host with full storage access |
| DR-001..DR-009 | `--include-dry-run` not set | Add `--include-dry-run` flag |

Expected CI SKIPs (always acceptable):
- V-007, V-008, V-009 (daemon not available on runners)
- V-010 (releases dir not on runner)
- DR-V02 (artifact path not on runner)

---

## CI Integration

The `.github/workflows/release-status.yml` workflow runs this report:

- **Nightly at 06:30 UTC** (after `post-release-reliability` at 06:00)
- **On demand** via `workflow_dispatch` with optional `release_version` and `strict_mode` inputs

Artifacts uploaded per run:
- `release-status.json` — canonical JSON (schema_version=1)
- `release-status.txt` — human summary (stderr)

The workflow fails if `overall_status=FAIL`. SKIPs are never a failure in CI mode.

---

## Related Documents

- `scripts/automation/verify-release.sh` — post-release verification (V-001..V-010)
- `scripts/automation/validate-release-record.sh` — release record schema validation
- `scripts/release/dry-run-patch-release.sh` — patch dry-run simulation (DR-001..DR-009)
- `docs/runbooks/v1.2.1-patch-readiness.md` — v1.2.1 patch go/no-go criteria
- `docs/runbooks/hotfix-patch-release.md` — hotfix patch release runbook
- `.github/workflows/release-status.yml` — CI workflow for this report
- `.github/workflows/post-release-reliability.yml` — scheduled reliability gate (R-001..R-005)
