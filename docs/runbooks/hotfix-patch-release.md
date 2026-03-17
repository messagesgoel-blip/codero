# Codero Hotfix Patch Release Runbook (v1.2.x)

## Overview

A **hotfix patch release** (e.g. v1.2.1, v1.2.2) addresses critical bugs or security issues found
after a minor version ships. This runbook covers:

- When to open a patch release vs wait for the next minor
- Branch and tag naming conventions
- Required validation matrix
- Fast-path install and rollback

---

## When to Issue a Patch Release

| Trigger | Patch now? | Rationale |
|---|---|---|
| Security vulnerability (CVSS ≥ 7.0) | ✅ Yes | Ship immediately after fix+test |
| Panic / crash in production path | ✅ Yes | Operator impact is high |
| Data loss or corruption risk | ✅ Yes | Non-negotiable |
| Observability endpoint returns 5xx unexpectedly | ✅ Yes | Blocks proving-gate CI |
| `codero prove` FAIL on a merged main check | ✅ Yes | Breaks CI gate for all PRs |
| Minor UX regression with workaround | ❌ No | Queue for next minor |
| Test-only flakiness (not user-facing) | ❌ No | Fix in minor; note in KNOWN_ISSUES |
| Documentation error | ❌ No | Doc-only PR against main |

**When in doubt:** open a patch if the issue affects operators in production today.

---

## Branch and Tag Naming

| Object | Convention | Example |
|---|---|---|
| Fix branch | `fix/COD-XXX-<short-desc>` | `fix/COD-041-gate-panic-on-nil-run` |
| Release branch | `release/v1.2.x` (created once per patch series) | `release/v1.2.1` |
| Git tag | `vMAJOR.MINOR.PATCH` | `v1.2.1` |
| Binary artifact | `codero-vMAJOR.MINOR.PATCH/codero` | `codero-v1.2.1/codero` |

**Branching model:**

```
main
  └── fix/COD-XXX-short-desc   ← implement the fix here
         └── PR → main          ← merge fix into main
                └── tag v1.2.1  ← tag on main after merge
```

For a series of related patch fixes, a shared `release/v1.2.x` branch may be used — but a
single-fix patch can go directly from `fix/...` → `main` → tag.

---

## Step-by-Step Patch Release Flow

### Step 1 — Open fix branch

```bash
cd /srv/storage/repo/codero
git checkout main && git pull
git checkout -b fix/COD-XXX-<short-description>
```

### Step 2 — Implement and test the fix

Keep the diff minimal. Only change what is needed to address the issue.

```bash
# After changes:
go test -count=1 ./...
go test -race ./...
go vet ./...
```

### Step 3 — Validate against patch validation matrix (see below)

Run the matrix before committing to the PR.

### Step 4 — Open PR

```bash
git push origin fix/COD-XXX-<short-description>
gh pr create \
  --title "fix(patch): COD-XXX — <short description>" \
  --body "..." \
  --base main
```

PR body must include:
- Linked issue (COD-XXX)
- Root cause summary (1–2 sentences)
- Test evidence (`go test -count=1 ./...` pass + proving gate output)
- Deploy impact (is a binary swap required?)
- Rollback commands
- `@coderabbitai review` + `@coderabbitai summary`

### Step 5 — Merge and tag

```bash
# After PR is merged and CI is green:
git checkout main && git pull
git tag v1.2.X -m "v1.2.X — <one-line summary>"
git push origin v1.2.X
```

### Step 6 — Build and install

```bash
VERSION=v1.2.X
RELEASE_DIR=/srv/storage/shared/tools/releases/codero-${VERSION}
mkdir -p "${RELEASE_DIR}"

go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${RELEASE_DIR}/codero" \
  ./cmd/codero

sha256sum "${RELEASE_DIR}/codero" | tee "${RELEASE_DIR}/codero.sha256"

# Back up current binary
cp -p /srv/storage/shared/tools/bin/codero \
      /srv/storage/shared/tools/bin/codero.prev.bak

# Atomic install
cp "${RELEASE_DIR}/codero" /srv/storage/shared/tools/bin/codero.new
mv /srv/storage/shared/tools/bin/codero.new /srv/storage/shared/tools/bin/codero

codero version   # must print v1.2.X
```

### Step 7 — Post-install verification

```bash
./scripts/automation/verify-release.sh \
  --version v1.2.X \
  --repo-path /srv/storage/repo/codero \
  --fast
```

Expected output:
```
overall=PASS  passed=≥8  failed=0  skipped=≤3
```

### Step 8 — Update docs

For a patch release, only update the following (no new user manual needed for minor fixes):

- `docs/runbooks/v1.2.0-technical-report.md`: add a **Patch history** row.
- `CHANGELOG.md` (if present): add entry.
- This runbook: record the patch under **Patch history** below.

---

## Patch Validation Matrix

All items must be ✅ PASS before merging the patch PR.

| # | Check | Command | Pass Criteria |
|---|---|---|---|
| P-001 | Unit tests | `go test -count=1 ./...` | All packages PASS |
| P-002 | Race tests | `go test -race ./...` | 0 races detected |
| P-003 | Vet | `go vet ./...` | 0 findings |
| P-004 | Proving gate (fast) | `codero prove --fast --repo-path .` | overall=PASS, 0 FAIL |
| P-005 | gate-status JSON contract | `codero gate-status --json` | Valid JSON, required keys present |
| P-006 | Release verification script | `./scripts/automation/verify-release.sh --fast` | overall=PASS, 0 FAIL |
| P-007 | Version string | `codero version` | Exact patch version |
| P-008 | Checksum recorded | `cat releases/codero-vX.Y.Z/codero.sha256` | Non-empty SHA-256 |
| P-009 | Rollback binary readable | `codero.prev.bak version` | Previous version string |
| P-010 | CI proving-gate workflow | GitHub Actions on merged commit | Workflow green |

**For security patches only — additional checks:**

| # | Check | Command | Pass Criteria |
|---|---|---|---|
| PS-001 | `gitleaks` scan | `gitleaks detect --no-banner` | 0 secrets |
| PS-002 | `semgrep` scan | `semgrep --config auto .` | 0 blocking findings |

---

## Rollback Procedure

```bash
# Immediate rollback (takes effect instantly; no restart if daemon is off)
cp /srv/storage/shared/tools/bin/codero.prev.bak \
   /srv/storage/shared/tools/bin/codero

codero version   # confirm previous version

# If daemon was running:
codero stop && codero start
```

If the daemon was restarted after the patch install, rolling back the binary is sufficient — no
database or state changes occur during a patch install.

---

## SKIP Behavior in Verification

When running verification checks without a live daemon, the following checks will SKIP rather
than FAIL. This is expected and not a release blocker:

| Check | SKIP condition | To convert to PASS |
|---|---|---|
| V-007 `/health` | `--endpoint-url` not provided | Start daemon, pass `--endpoint-url` |
| V-008 `/gate` | `--endpoint-url` not provided | Start daemon, pass `--endpoint-url` |
| V-009 `/dashboard/` | `--endpoint-url` not provided | Start daemon, pass `--endpoint-url` |
| C-004 race-tests | `codero prove --fast` flag | Run `codero prove` (no `--fast`) |
| C-007..C-009 | Proving gate daemon checks | Same as above |

A SKIP is **not** a failure. CI (`proving-gate.yml`) treats SKIP as acceptable by design.

---

## Patch History

| Version | Date | Issue | Summary |
|---|---|---|---|
| v1.2.0 | 2026-03-17 | COD-031..034 | Initial v1.2.0 minor release (not a patch) |
| *(next patch)* | | | |

---

## Related Documents

- `docs/runbooks/release-checklist-template.md` — step-by-step checklist to fill in per release
- `docs/runbooks/v1.2.0-technical-report.md` — v1.2.0 baseline evidence
- `docs/runbooks/v1.2.0-user-manual.md` — operator upgrade guide
- `docs/runbooks/v1.2-proving-gate.md` — proving gate reference
- `scripts/automation/verify-release.sh` — automated post-release verification
- `.github/workflows/proving-gate.yml` — CI enforcement workflow
