# Codero Release Checklist Template

> **How to use:** Copy this file to `docs/runbooks/releases/vX.Y.Z-checklist.md` for each
> release. Fill in each item, tick the checkbox, and sign off at the bottom. Keep the filled
> copy for audit purposes.

---

## Release metadata

| Field | Value |
|---|---|
| Version | <!-- e.g. v1.3.0 --> |
| Release type | <!-- major / minor / patch --> |
| Base commit | <!-- git log --oneline -1 --> |
| Go toolchain | <!-- go version --> |
| Release engineer | <!-- name or handle --> |
| Date | <!-- YYYY-MM-DD --> |
| PR / branch | <!-- feat/COD-XXX-... or release/vX.Y.Z --> |

---

## Phase 1 — Pre-release checks

Run from the repo root on the release branch.

```bash
# Confirm you are on the correct branch and commit
git --no-pager log --oneline -3

# Full test suite — must pass before build
go test -count=1 ./...

# Race test pass
go test -race ./...

# Vet
go vet ./...

# Build (smoke only — not the release artifact yet)
go build ./...
```

| # | Check | Expected | Result | Notes |
|---|---|---|---|---|
| 1.1 | `git status` clean | No unstaged changes | ☐ PASS / ☐ FAIL | |
| 1.2 | On correct branch | feat/COD-XXX or release/vX.Y.Z | ☐ PASS / ☐ FAIL | |
| 1.3 | `go test -count=1 ./...` | All packages PASS | ☐ PASS / ☐ FAIL | |
| 1.4 | `go test -race ./...` | 0 races detected | ☐ PASS / ☐ FAIL | |
| 1.5 | `go vet ./...` | 0 findings | ☐ PASS / ☐ FAIL | |
| 1.6 | `go build ./...` | Exit 0 | ☐ PASS / ☐ FAIL | |
| 1.7 | `semgrep` clean | 0 blocking findings | ☐ PASS / ☐ FAIL | |
| 1.8 | `gitleaks` scan | 0 secrets found | ☐ PASS / ☐ FAIL | |
| 1.9 | Changelog / PR description written | Linked issue, scope, rollback | ☐ DONE | |
| 1.10 | Docs updated (runbooks, user manual) | Version refs accurate | ☐ DONE | |

---

## Phase 2 — Release build

```bash
# Substitute actual version and output path
VERSION=vX.Y.Z
RELEASE_DIR=/srv/storage/shared/tools/releases/codero-${VERSION}
mkdir -p "${RELEASE_DIR}"

go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION}" \
  -o "${RELEASE_DIR}/codero" \
  ./cmd/codero

# Generate and record checksum
sha256sum "${RELEASE_DIR}/codero" | tee "${RELEASE_DIR}/codero.sha256"

# Verify the build prints the right version
"${RELEASE_DIR}/codero" version
```

| # | Check | Expected | Result | Notes |
|---|---|---|---|---|
| 2.1 | `go build` succeeds | Exit 0, binary present | ☐ PASS / ☐ FAIL | |
| 2.2 | `binary version` output | Exact version string (vX.Y.Z) | ☐ PASS / ☐ FAIL | |
| 2.3 | `sha256sum` checksum recorded | Checksum file committed / archived | ☐ DONE | |
| 2.4 | Binary size reasonable | Within ~20% of prior release | ☐ PASS / ☐ FAIL | |

---

## Phase 3 — Release installation

```bash
VERSION=vX.Y.Z
RELEASE_DIR=/srv/storage/shared/tools/releases/codero-${VERSION}
INSTALL_TARGET=/srv/storage/shared/tools/bin/codero

# Back up current binary
cp -p "${INSTALL_TARGET}" "${INSTALL_TARGET}.prev.bak"

# Atomic swap
cp "${RELEASE_DIR}/codero" "${INSTALL_TARGET}.new"
mv "${INSTALL_TARGET}.new" "${INSTALL_TARGET}"

# Confirm installed version
codero version
```

| # | Check | Expected | Result | Notes |
|---|---|---|---|---|
| 3.1 | Previous binary backed up | `.prev.bak` exists at install path | ☐ DONE | |
| 3.2 | Atomic swap completed | No partial state at install path | ☐ DONE | |
| 3.3 | `codero version` post-swap | vX.Y.Z | ☐ PASS / ☐ FAIL | |

---

## Phase 4 — Post-release verification

Run the automated verification script and the proving gate:

```bash
# Full automated verification (non-destructive)
./scripts/automation/verify-release.sh \
  --version vX.Y.Z \
  --repo-path /srv/storage/repo/codero \
  --fast \
  --json 2>&1 | tee ~/verify-release-output.txt

# Proving gate (full, with race tests — pre-deploy sign-off)
codero prove --repo-path /srv/storage/repo/codero

# Key manual checks
codero gate-status --json
codero ports
codero status
```

| # | Check | Expected | Result | Notes |
|---|---|---|---|---|
| 4.1 | `verify-release.sh` | overall=PASS, 0 FAIL | ☐ PASS / ☐ FAIL | |
| 4.2 | `codero prove` (full, with race) | overall=PASS, 0 FAIL | ☐ PASS / ☐ FAIL | |
| 4.3 | `codero gate-status --json` | Valid JSON, expected keys | ☐ PASS / ☐ FAIL | |
| 4.4 | `codero ports` | Exit 0, no errors | ☐ PASS / ☐ FAIL | |
| 4.5 | `codero status` | Exit 0, gate state reported | ☐ PASS / ☐ FAIL | |
| 4.6 | *(If daemon running)* `codero dashboard --check` | 3/3 endpoints green | ☐ PASS / ☐ SKIP | |
| 4.7 | CI proving-gate workflow | Green on merged PR | ☐ PASS / ☐ FAIL | |

---

## Phase 5 — Rollback verification

Verify the rollback path works **before** considering the release complete.

```bash
# Perform rollback (read-only test: restore backup, verify, then swap back)
INSTALL_TARGET=/srv/storage/shared/tools/bin/codero
cp "${INSTALL_TARGET}.prev.bak" "${INSTALL_TARGET}.rollback-test"

# Test the rollback binary's version
"${INSTALL_TARGET}.rollback-test" version

# (If rollback test is satisfactory, actual rollback would be:)
# cp "${INSTALL_TARGET}.prev.bak" "${INSTALL_TARGET}" && codero version

# Clean up rollback test artifact
rm "${INSTALL_TARGET}.rollback-test"
```

| # | Check | Expected | Result | Notes |
|---|---|---|---|---|
| 5.1 | Rollback binary readable | `version` exits 0 | ☐ PASS / ☐ FAIL | |
| 5.2 | Rollback binary reports previous version | vX.(Y-1).Z or prior | ☐ PASS / ☐ FAIL | |
| 5.3 | Rollback procedure documented in PR | Exact commands present | ☐ DONE | |

---

## Sign-off

| Role | Name / Handle | Date | Signature |
|---|---|---|---|
| Release engineer | | | ☐ Approved |
| Reviewer (if available) | | | ☐ Approved |

**Release decision:** ☐ GO  ☐ NO-GO

**Notes / blockers:**

---

## Quick reference: rollback commands

```bash
# Instant rollback to previous version
cp /srv/storage/shared/tools/bin/codero.prev.bak \
   /srv/storage/shared/tools/bin/codero
codero version   # confirm previous version restored

# If daemon was running, restart:
codero stop && codero start
```
