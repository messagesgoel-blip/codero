#!/usr/bin/env bash
# dry-run-patch-release.sh — Deterministic dry-run release simulation for Codero patch releases.
#
# Usage:
#   ./scripts/release/dry-run-patch-release.sh [OPTIONS]
#
# Options:
#   --target-version VERSION   Target version label (default: v1.2.1-rc.dryrun)
#   --base-version VERSION     Base version for rollback drill (default: v1.2.0)
#   --artifact-dir DIR         Output directory for artifacts (default: auto temp dir via mktemp)
#   --json                     Emit structured JSON summary to stdout; human log to stderr
#   --help                     Print this help and exit
#
# Checks performed:
#   DR-001  Build binary from source
#   DR-002  Compute SHA-256 of built binary
#   DR-003  go test -count=1 ./...
#   DR-004  go test -race ./...
#   DR-005  go test -tags=e2e ./tests/e2e/ (best-effort; SKIP if env unavailable)
#   DR-006  codero prove --fast --json
#   DR-007  verify-release.sh --version <target-version>
#   DR-008  Install drill (copy to .dryrun-test path, version check, checksum, cleanup)
#   DR-009  Rollback drill (locate base artifact, exec version; SKIP if absent)
#
# Exit codes:
#   0  All checks PASS or SKIP
#   1  One or more checks FAILED
#   2  Environment / prerequisite error
#
# Notes:
#   - Idempotent: cleans up all temp outputs on exit (unless --artifact-dir specified).
#   - Never writes to live install path (/srv/storage/shared/tools/bin/).
#   - Strict shell mode: set -euo pipefail.

set -euo pipefail

# ── defaults ──────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

TARGET_VERSION="v1.2.1-rc.dryrun"
BASE_VERSION="v1.2.0"
EMIT_JSON=false
USER_ARTIFACT_DIR=""
TS="$(date -u +%Y%m%dT%H%M%SZ)"
START_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ── parse args ────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --target-version)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --target-version requires a value" >&2; exit 2; }
            TARGET_VERSION="$2"; shift 2 ;;
        --base-version)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --base-version requires a value" >&2; exit 2; }
            BASE_VERSION="$2"; shift 2 ;;
        --artifact-dir)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --artifact-dir requires a value" >&2; exit 2; }
            USER_ARTIFACT_DIR="$2"; shift 2 ;;
        --json)     EMIT_JSON=true; shift ;;
        --help)
            sed -n '/^# dry-run-patch-release/,/^set -euo/{ /^set/d; s/^# \?//; p }' "$0"
            exit 0 ;;
        *) echo "[ERROR] Unknown option: $1" >&2; exit 2 ;;
    esac
done

# ── artifact directory setup ──────────────────────────────────────────────────
if [[ -n "${USER_ARTIFACT_DIR}" ]]; then
    ARTIFACT_DIR="${USER_ARTIFACT_DIR}"
    mkdir -p "${ARTIFACT_DIR}"
    CLEANUP_ON_EXIT=false
else
    ARTIFACT_DIR="$(mktemp -d)"
    CLEANUP_ON_EXIT=true
fi

BUILT_BIN="${ARTIFACT_DIR}/codero"
DRYRUN_INSTALL_PATH="${ARTIFACT_DIR}/.dryrun-test/codero"
CHECKSUM_FILE="${ARTIFACT_DIR}/codero.sha256"
UNIT_LOG="${ARTIFACT_DIR}/unit-tests.log"
RACE_LOG="${ARTIFACT_DIR}/race-tests.log"
E2E_LOG="${ARTIFACT_DIR}/e2e-tests.log"
PROVE_JSON="${ARTIFACT_DIR}/prove-result.json"
VERIFY_JSON="${ARTIFACT_DIR}/verify-result.json"

cleanup() {
    if [[ "${CLEANUP_ON_EXIT}" == "true" && -d "${ARTIFACT_DIR}" ]]; then
        rm -rf "${ARTIFACT_DIR}"
    fi
}
trap cleanup EXIT

# ── result tracking ───────────────────────────────────────────────────────────
declare -a RESULTS=()   # "ID|STATUS|NAME|DETAIL"
FAIL_COUNT=0
PASS_COUNT=0
SKIP_COUNT=0

record() {
    local id="$1" status="$2" name="$3" detail="${4:-}"
    RESULTS+=("${id}|${status}|${name}|${detail}")
    case "$status" in
        PASS) PASS_COUNT=$(( PASS_COUNT + 1 )) ;;
        FAIL) FAIL_COUNT=$(( FAIL_COUNT + 1 )) ;;
        SKIP) SKIP_COUNT=$(( SKIP_COUNT + 1 )) ;;
    esac
}

log()  { echo "[$(date -u +%H:%M:%S)] $*" >&2; }
pass() { log "PASS  $*"; }
fail() { log "FAIL  $*"; }
skip() { log "SKIP  $*"; }
note() { log "      $*"; }

# ── preamble ──────────────────────────────────────────────────────────────────
log "════════════════════════════════════════════════════════"
log " Codero Patch Dry-Run Release Simulation"
log "════════════════════════════════════════════════════════"
log " target-version : ${TARGET_VERSION}"
log " base-version   : ${BASE_VERSION}"
log " artifact-dir   : ${ARTIFACT_DIR}"
log " repo-root      : ${REPO_ROOT}"
log " timestamp      : ${START_TS}"
log "════════════════════════════════════════════════════════"

# ── DR-001: Build binary ──────────────────────────────────────────────────────
log "DR-001  Build binary from source"
BUILD_EXIT=0
go build -trimpath \
    -ldflags "-s -w -X main.version=${TARGET_VERSION}" \
    -o "${BUILT_BIN}" \
    "${REPO_ROOT}/cmd/codero" 2>&1 || BUILD_EXIT=$?

if [[ ${BUILD_EXIT} -ne 0 ]]; then
    fail "DR-001 build failed (exit ${BUILD_EXIT})"
    record "DR-001" "FAIL" "build-binary" "go build exit=${BUILD_EXIT}"
    log "[FATAL] Cannot continue without binary." >&2
    exit 1
fi
pass "DR-001 binary built: ${BUILT_BIN}"
record "DR-001" "PASS" "build-binary" "${BUILT_BIN}"

# ── DR-002: SHA-256 ───────────────────────────────────────────────────────────
log "DR-002  Compute SHA-256"
SHA256="$(sha256sum "${BUILT_BIN}" | awk '{print $1}')"
echo "${SHA256}  ${BUILT_BIN}" > "${CHECKSUM_FILE}"
pass "DR-002 sha256=${SHA256:0:16}…"
record "DR-002" "PASS" "sha256" "${SHA256}"
note "Checksum written: ${CHECKSUM_FILE}"

# ── DR-003: go test -count=1 ──────────────────────────────────────────────────
log "DR-003  go test -count=1 ./..."
UNIT_EXIT=0
(cd "${REPO_ROOT}" && go test -count=1 ./... 2>&1) | tee "${UNIT_LOG}" >/dev/null || UNIT_EXIT=$?
if [[ ${UNIT_EXIT} -eq 0 ]]; then
    pass "DR-003 unit tests: PASS"
    record "DR-003" "PASS" "unit-tests" "exit=0"
else
    fail "DR-003 unit tests: FAIL (exit ${UNIT_EXIT})"
    record "DR-003" "FAIL" "unit-tests" "exit=${UNIT_EXIT}; see ${UNIT_LOG}"
fi

# ── DR-004: go test -race ─────────────────────────────────────────────────────
log "DR-004  go test -race ./..."
RACE_EXIT=0
(cd "${REPO_ROOT}" && go test -race ./... 2>&1) | tee "${RACE_LOG}" >/dev/null || RACE_EXIT=$?
if [[ ${RACE_EXIT} -eq 0 ]]; then
    pass "DR-004 race tests: PASS"
    record "DR-004" "PASS" "race-tests" "exit=0"
else
    fail "DR-004 race tests: FAIL (exit ${RACE_EXIT})"
    record "DR-004" "FAIL" "race-tests" "exit=${RACE_EXIT}; see ${RACE_LOG}"
fi

# ── DR-005: e2e tests (best-effort) ──────────────────────────────────────────
log "DR-005  go test -tags=e2e ./tests/e2e/ (best-effort)"
E2E_DIR="${REPO_ROOT}/tests/e2e"
if [[ ! -d "${E2E_DIR}" ]]; then
    skip "DR-005 e2e: SKIP — tests/e2e/ directory not found"
    record "DR-005" "SKIP" "e2e-tests" "tests/e2e/ directory absent"
elif [[ "${CODERO_E2E_SKIP:-}" == "true" ]]; then
    skip "DR-005 e2e: SKIP — CODERO_E2E_SKIP=true"
    record "DR-005" "SKIP" "e2e-tests" "CODERO_E2E_SKIP=true"
else
    E2E_EXIT=0
    (cd "${REPO_ROOT}" && go test -tags=e2e -count=1 -v ./tests/e2e/ 2>&1) \
        | tee "${E2E_LOG}" >/dev/null || E2E_EXIT=$?
    # Check for "no test files" regardless of exit code — on zero exit it means
    # nothing actually ran, which is a SKIP not a PASS.
    if grep -q "no test files" "${E2E_LOG}" 2>/dev/null || \
       grep -q "\[no test files\]" "${E2E_LOG}" 2>/dev/null; then
        skip "DR-005 e2e: SKIP — no test files found under tests/e2e/"
        record "DR-005" "SKIP" "e2e-tests" "no test files in tests/e2e/"
    elif [[ ${E2E_EXIT} -eq 0 ]]; then
        pass "DR-005 e2e tests: PASS"
        record "DR-005" "PASS" "e2e-tests" "exit=0"
    else
        fail "DR-005 e2e tests: FAIL (exit ${E2E_EXIT}) — daemon or env unavailable"
        record "DR-005" "FAIL" "e2e-tests" "exit=${E2E_EXIT}; daemon or env unavailable; see ${E2E_LOG}"
    fi
fi

# ── DR-006: codero prove --fast --json ────────────────────────────────────────
log "DR-006  codero prove --fast --json"
PROVE_EXIT=0
CODERO_BIN="${BUILT_BIN}" \
    "${BUILT_BIN}" prove --fast --json --repo-path "${REPO_ROOT}" \
    2>/dev/null >"${PROVE_JSON}" || PROVE_EXIT=$?

if [[ ${PROVE_EXIT} -eq 0 && -s "${PROVE_JSON}" ]]; then
    PROVE_OVERALL="$(python3 -c "import json,sys; d=json.load(open('${PROVE_JSON}')); print(d.get('overall_status','unknown'))" 2>/dev/null || echo "unknown")"
    PROVE_PASSED="$( python3 -c "import json,sys; d=json.load(open('${PROVE_JSON}')); print(d.get('passed',0))"         2>/dev/null || echo "?")"
    PROVE_SKIPPED="$(python3 -c "import json,sys; d=json.load(open('${PROVE_JSON}')); print(d.get('skipped',0))"        2>/dev/null || echo "?")"
    PROVE_FAILED="$( python3 -c "import json,sys; d=json.load(open('${PROVE_JSON}')); print(d.get('failed',0))"         2>/dev/null || echo "?")"
    if [[ "${PROVE_OVERALL}" == "PASS" ]]; then
        pass "DR-006 prove: overall=${PROVE_OVERALL} passed=${PROVE_PASSED} skipped=${PROVE_SKIPPED} failed=${PROVE_FAILED}"
        record "DR-006" "PASS" "prove-gate" "overall=${PROVE_OVERALL} passed=${PROVE_PASSED} skipped=${PROVE_SKIPPED} failed=${PROVE_FAILED}"
    else
        fail "DR-006 prove: overall=${PROVE_OVERALL} passed=${PROVE_PASSED} skipped=${PROVE_SKIPPED} failed=${PROVE_FAILED}"
        record "DR-006" "FAIL" "prove-gate" "overall=${PROVE_OVERALL} passed=${PROVE_PASSED} skipped=${PROVE_SKIPPED} failed=${PROVE_FAILED}"
    fi
else
    fail "DR-006 prove exited ${PROVE_EXIT} or produced no output"
    record "DR-006" "FAIL" "prove-gate" "exit=${PROVE_EXIT}; no JSON output"
fi

# ── DR-007: verify-release.sh ────────────────────────────────────────────────
log "DR-007  verify-release.sh --version ${TARGET_VERSION}"
VERIFY_SCRIPT="${REPO_ROOT}/scripts/automation/verify-release.sh"
VERIFY_EXIT=0
if [[ ! -x "${VERIFY_SCRIPT}" ]]; then
    fail "DR-007 verify-release.sh not found or not executable: ${VERIFY_SCRIPT}"
    record "DR-007" "FAIL" "verify-release" "script not found: ${VERIFY_SCRIPT}"
else
    CODERO_BIN="${BUILT_BIN}" \
        bash "${VERIFY_SCRIPT}" \
            --version "${TARGET_VERSION}" \
            --repo-path "${REPO_ROOT}" \
            --fast \
            --json \
            2>/dev/null >"${VERIFY_JSON}" || VERIFY_EXIT=$?

    if [[ ${VERIFY_EXIT} -eq 0 && -s "${VERIFY_JSON}" ]]; then
        VER_OVERALL="$(python3 -c "import json,sys; d=json.load(open('${VERIFY_JSON}')); print(d.get('overall_status','unknown'))" 2>/dev/null || echo "unknown")"
        VER_PASSED="$( python3 -c "import json,sys; d=json.load(open('${VERIFY_JSON}')); print(d.get('passed',0))"  2>/dev/null || echo "?")"
        VER_SKIPPED="$(python3 -c "import json,sys; d=json.load(open('${VERIFY_JSON}')); print(d.get('skipped',0))" 2>/dev/null || echo "?")"
        VER_FAILED="$( python3 -c "import json,sys; d=json.load(open('${VERIFY_JSON}')); print(d.get('failed',0))"  2>/dev/null || echo "?")"
        if [[ "${VER_OVERALL}" == "PASS" ]]; then
            pass "DR-007 verify-release: overall=${VER_OVERALL} passed=${VER_PASSED} skipped=${VER_SKIPPED} failed=${VER_FAILED}"
            record "DR-007" "PASS" "verify-release" "overall=${VER_OVERALL} passed=${VER_PASSED} skipped=${VER_SKIPPED} failed=${VER_FAILED}"
        else
            fail "DR-007 verify-release: overall=${VER_OVERALL} passed=${VER_PASSED} skipped=${VER_SKIPPED} failed=${VER_FAILED}"
            record "DR-007" "FAIL" "verify-release" "overall=${VER_OVERALL} passed=${VER_PASSED} skipped=${VER_SKIPPED} failed=${VER_FAILED}"
        fi
    else
        # verify-release exits 1 on FAIL with valid results; re-read JSON if present
        if [[ -s "${VERIFY_JSON}" ]]; then
            VER_OVERALL="$(python3 -c "import json,sys; d=json.load(open('${VERIFY_JSON}')); print(d.get('overall_status','unknown'))" 2>/dev/null || echo "unknown")"
            VER_PASSED="$( python3 -c "import json,sys; d=json.load(open('${VERIFY_JSON}')); print(d.get('passed',0))"  2>/dev/null || echo "?")"
            VER_SKIPPED="$(python3 -c "import json,sys; d=json.load(open('${VERIFY_JSON}')); print(d.get('skipped',0))" 2>/dev/null || echo "?")"
            VER_FAILED="$( python3 -c "import json,sys; d=json.load(open('${VERIFY_JSON}')); print(d.get('failed',0))"  2>/dev/null || echo "?")"
            fail "DR-007 verify-release: overall=${VER_OVERALL} passed=${VER_PASSED} skipped=${VER_SKIPPED} failed=${VER_FAILED}"
            record "DR-007" "FAIL" "verify-release" "exit=${VERIFY_EXIT} overall=${VER_OVERALL} passed=${VER_PASSED} skipped=${VER_SKIPPED} failed=${VER_FAILED}"
        else
            fail "DR-007 verify-release.sh exited ${VERIFY_EXIT} with no JSON output"
            record "DR-007" "FAIL" "verify-release" "exit=${VERIFY_EXIT}; no JSON output"
        fi
    fi
fi

# ── DR-008: Install drill ─────────────────────────────────────────────────────
log "DR-008  Install drill (non-destructive)"
INSTALL_DRILL_OK=true
INSTALL_DETAIL=""
DRYRUN_INSTALL_DIR="$(dirname "${DRYRUN_INSTALL_PATH}")"
mkdir -p "${DRYRUN_INSTALL_DIR}"

# Copy artifact to drill path
if ! cp "${BUILT_BIN}" "${DRYRUN_INSTALL_PATH}"; then
    fail "DR-008 install drill: copy to ${DRYRUN_INSTALL_PATH} failed"
    record "DR-008" "FAIL" "install-drill" "copy failed"
    INSTALL_DRILL_OK=false
fi

if [[ "${INSTALL_DRILL_OK}" == "true" ]]; then
    # Run version check
    DRILL_VERSION_OUT=""
    DRILL_VERSION_EXIT=0
    DRILL_VERSION_OUT="$("${DRYRUN_INSTALL_PATH}" version 2>&1)" || DRILL_VERSION_EXIT=$?
    if [[ ${DRILL_VERSION_EXIT} -ne 0 ]]; then
        fail "DR-008 install drill: version check failed (exit ${DRILL_VERSION_EXIT})"
        record "DR-008" "FAIL" "install-drill" "version check exit=${DRILL_VERSION_EXIT}"
        INSTALL_DRILL_OK=false
    else
        note "  drill binary version: ${DRILL_VERSION_OUT}"

        # Assert version string matches TARGET_VERSION
        DRILL_VERSION_TRIMMED="$(echo "${DRILL_VERSION_OUT}" | tr -d '[:space:]')"
        TARGET_VERSION_TRIMMED="$(echo "${TARGET_VERSION}" | tr -d '[:space:]')"
        if [[ "${DRILL_VERSION_TRIMMED}" != "${TARGET_VERSION_TRIMMED}" ]]; then
            fail "DR-008 install drill: version mismatch expected=${TARGET_VERSION} got=${DRILL_VERSION_OUT}"
            record "DR-008" "FAIL" "install-drill" "version mismatch: expected=${TARGET_VERSION} got=${DRILL_VERSION_OUT}"
            INSTALL_DRILL_OK=false
        fi

        # Validate checksum
        if [[ "${INSTALL_DRILL_OK}" == "true" ]]; then
            DRILL_SHA256="$(sha256sum "${DRYRUN_INSTALL_PATH}" | awk '{print $1}')"
            if [[ "${DRILL_SHA256}" != "${SHA256}" ]]; then
                fail "DR-008 install drill: checksum mismatch after copy"
                record "DR-008" "FAIL" "install-drill" "checksum mismatch: expected=${SHA256:0:16}… got=${DRILL_SHA256:0:16}…"
                INSTALL_DRILL_OK=false
            else
                pass "DR-008 install drill: copy+version+checksum OK (version=${DRILL_VERSION_OUT})"
                record "DR-008" "PASS" "install-drill" "version=${DRILL_VERSION_OUT} sha256=${SHA256:0:16}…"
            fi
        fi
    fi
fi

# Cleanup drill path
rm -rf "${DRYRUN_INSTALL_DIR}"

# ── DR-009: Rollback drill ────────────────────────────────────────────────────
log "DR-009  Rollback drill (base-version=${BASE_VERSION})"
RELEASES_DIR="/srv/storage/shared/tools/releases"
BASE_ARTIFACT="${RELEASES_DIR}/codero-${BASE_VERSION}/codero"
PREV_BAK="/srv/storage/shared/tools/bin/codero.prev.bak"

if [[ -x "${BASE_ARTIFACT}" ]]; then
    ROLLBACK_VERSION_OUT=""
    ROLLBACK_EXIT=0
    ROLLBACK_VERSION_OUT="$("${BASE_ARTIFACT}" version 2>&1)" || ROLLBACK_EXIT=$?
    if [[ ${ROLLBACK_EXIT} -eq 0 ]]; then
        pass "DR-009 rollback drill: ${BASE_ARTIFACT} version=${ROLLBACK_VERSION_OUT}"
        record "DR-009" "PASS" "rollback-drill" "base=${BASE_VERSION} version=${ROLLBACK_VERSION_OUT}"
    else
        fail "DR-009 rollback drill: ${BASE_ARTIFACT} exec failed (exit ${ROLLBACK_EXIT})"
        record "DR-009" "FAIL" "rollback-drill" "exit=${ROLLBACK_EXIT}"
    fi
elif [[ -x "${PREV_BAK}" ]]; then
    ROLLBACK_VERSION_OUT=""
    ROLLBACK_EXIT=0
    ROLLBACK_VERSION_OUT="$("${PREV_BAK}" version 2>&1)" || ROLLBACK_EXIT=$?
    if [[ ${ROLLBACK_EXIT} -eq 0 ]]; then
        pass "DR-009 rollback drill: ${PREV_BAK} version=${ROLLBACK_VERSION_OUT}"
        record "DR-009" "PASS" "rollback-drill" "source=codero.prev.bak version=${ROLLBACK_VERSION_OUT}"
    else
        fail "DR-009 rollback drill: ${PREV_BAK} exec failed (exit ${ROLLBACK_EXIT})"
        record "DR-009" "FAIL" "rollback-drill" "exit=${ROLLBACK_EXIT}"
    fi
else
    skip "DR-009 rollback drill: SKIP — base artifact not found"
    skip "  Checked: ${BASE_ARTIFACT}"
    skip "  Checked: ${PREV_BAK}"
    record "DR-009" "SKIP" "rollback-drill" \
        "base artifact absent: ${BASE_ARTIFACT}; no codero.prev.bak; run a real install first"
fi

# ── Summary table ─────────────────────────────────────────────────────────────
TOTAL=$(( PASS_COUNT + FAIL_COUNT + SKIP_COUNT ))
OVERALL="PASS"
[[ ${FAIL_COUNT} -gt 0 ]] && OVERALL="FAIL"

log ""
log "════════════════════════════════════════════════════════"
log " Patch Dry-Run Release Simulation — Summary"
log "════════════════════════════════════════════════════════"
printf "%-8s  %-28s  %-6s  %s\n" "ID" "NAME" "STATUS" "DETAIL" >&2
printf "%-8s  %-28s  %-6s  %s\n" "──────" "────────────────────────────" "──────" "──────" >&2
for row in "${RESULTS[@]}"; do
    IFS='|' read -r id status name detail <<< "$row"
    icon="PASS"
    [[ "$status" == "FAIL" ]] && icon="FAIL"
    [[ "$status" == "SKIP" ]] && icon="SKIP"
    printf "[%-4s] %-6s  %-28s  %s\n" "$icon" "$id" "$name" "$detail" >&2
done
log "────────────────────────────────────────────────────────"
log " overall=${OVERALL}  passed=${PASS_COUNT}  failed=${FAIL_COUNT}  skipped=${SKIP_COUNT}  total=${TOTAL}"
log "════════════════════════════════════════════════════════"
if [[ -n "${USER_ARTIFACT_DIR}" ]]; then
    log " Artifacts retained: ${ARTIFACT_DIR}"
fi

# ── JSON output ───────────────────────────────────────────────────────────────
if [[ "${EMIT_JSON}" == "true" ]]; then
    CHECKS_JSON="[]"
    for row in "${RESULTS[@]}"; do
        IFS='|' read -r id status name detail <<< "$row"
        STATUS_LC="$(echo "${status}" | tr '[:upper:]' '[:lower:]')"
        CHECKS_JSON="$(jq -c \
            --arg id     "${id}" \
            --arg status "${STATUS_LC}" \
            --arg name   "${name}" \
            --arg detail "${detail}" \
            '. + [{id:$id,status:$status,name:$name,detail:$detail}]' \
            <<<"${CHECKS_JSON}")"
    done
    jq -nc \
        --arg schema_version "1" \
        --arg timestamp      "${START_TS}" \
        --arg target_version "${TARGET_VERSION}" \
        --arg base_version   "${BASE_VERSION}" \
        --arg overall        "${OVERALL}" \
        --argjson passed     "${PASS_COUNT}" \
        --argjson failed     "${FAIL_COUNT}" \
        --argjson skipped    "${SKIP_COUNT}" \
        --argjson total      "${TOTAL}" \
        --arg sha256         "${SHA256:-}" \
        --arg artifact_dir   "${ARTIFACT_DIR}" \
        --argjson checks     "${CHECKS_JSON}" \
        '{
          schema_version: $schema_version,
          timestamp:      $timestamp,
          target_version: $target_version,
          base_version:   $base_version,
          overall_status: $overall,
          passed:         $passed,
          failed:         $failed,
          skipped:        $skipped,
          total:          $total,
          sha256:         $sha256,
          artifact_dir:   $artifact_dir,
          checks:         $checks
        }'
fi

# ── exit ──────────────────────────────────────────────────────────────────────
[[ ${FAIL_COUNT} -gt 0 ]] && exit 1
exit 0
