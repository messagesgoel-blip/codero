#!/usr/bin/env bash
# KEEP_LOCAL: required for shipped runtime — post-release verification entrypoint
#
# verify-release.sh — Non-destructive post-release verification for Codero.
#
# Usage:
#   ./scripts/automation/verify-release.sh [OPTIONS]
#
# Options:
#   --version VERSION      Expected version string (e.g. v1.2.0). If provided,
#                          codero version output is asserted to match exactly.
#   --repo-path PATH       Path to codero repo root (default: auto-detect from
#                          script location).
#   --endpoint-url URL     If the daemon is running, base URL for smoke checks
#                          (e.g. http://localhost:8080). Optional.
#   --fast                 Pass --fast to codero prove (skip race tests).
#   --json                 Emit structured JSON summary to stdout in addition to
#                          human log (goes to stderr).
#   --help                 Print this help and exit.
#
# Exit codes:
#   0  All checks PASS (SKIP counts as pass).
#   1  One or more checks FAILED.
#   2  Environment / prerequisite error (binary not found, wrong version, etc.).
#
# Checks performed (all non-destructive, read-only):
#   V-001  codero binary reachable
#   V-002  codero version (optionally asserted)
#   V-003  codero gate-status --json (valid JSON, expected keys present)
#   V-004  codero ports (exit 0)
#   V-005  codero status (exit 0)
#   V-006  codero prove --fast (full proving gate: 22 checks)
#   V-007  endpoint smoke: /health          [SKIP if --endpoint-url absent]
#   V-008  endpoint smoke: /gate            [SKIP if --endpoint-url absent]
#   V-009  endpoint smoke: /dashboard/      [SKIP if --endpoint-url absent]
#   V-010  checksum file present            [SKIP if releases dir absent]
#
# All SKIP checks are clearly labelled. No state is modified. No daemon is
# started or stopped.

set -euo pipefail

# ── defaults ──────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_PATH="${SCRIPT_DIR}/../.."  # scripts/automation -> repo root
CODERO_BIN="${CODERO_BIN:-$(command -v codero 2>/dev/null || echo "")}"
EXPECTED_VERSION=""
ENDPOINT_URL=""
FAST_FLAG="--fast"
EMIT_JSON=false

# ── parse args ────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --version)   EXPECTED_VERSION="$2"; shift 2 ;;
        --repo-path) REPO_PATH="$2";        shift 2 ;;
        --endpoint-url) ENDPOINT_URL="$2";  shift 2 ;;
        --fast)      FAST_FLAG="--fast";    shift ;;
        --no-fast)   FAST_FLAG="";          shift ;;
        --json)      EMIT_JSON=true;        shift ;;
        --help)
            sed -n '/^# verify-release/,/^set -euo/{ /^set/d; s/^# \?//; p }' "$0"
            exit 0 ;;
        *) echo "[ERROR] Unknown option: $1" >&2; exit 2 ;;
    esac
done

REPO_PATH="$(cd "${REPO_PATH}" && pwd)"

# ── result tracking ───────────────────────────────────────────────────────────
declare -a RESULTS=()   # "ID|STATUS|NAME|DETAIL"
FAIL_COUNT=0
PASS_COUNT=0
SKIP_COUNT=0
START_TS="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

record() {
    local id="$1" status="$2" name="$3" detail="${4:-}"
    RESULTS+=("${id}|${status}|${name}|${detail}")
    case "$status" in
        PASS) PASS_COUNT=$(( PASS_COUNT + 1 )) ;;
        FAIL) FAIL_COUNT=$(( FAIL_COUNT + 1 )) ;;
        SKIP) SKIP_COUNT=$(( SKIP_COUNT + 1 )) ;;
    esac
}

# ── helpers ───────────────────────────────────────────────────────────────────
log()  { echo "[$(date -u +%H:%M:%S)] $*" >&2; }
pass() { log "✅ $*"; }
fail() { log "❌ $*"; }
skip() { log "⏭  $*"; }

check_json_keys() {
    local json="$1"; shift
    local keys=("$@")
    for key in "${keys[@]}"; do
        if ! echo "$json" | grep -q "\"${key}\""; then
            echo "missing key: ${key}"
            return 1
        fi
    done
    return 0
}

# ── V-001: binary reachable ───────────────────────────────────────────────────
log "V-001  codero binary reachable"
if [[ -z "${CODERO_BIN}" ]]; then
    fail "codero not found in PATH and CODERO_BIN not set"
    record "V-001" "FAIL" "codero-binary-reachable" "not found in PATH"
    echo "[ERROR] codero binary not found. Install codero or set CODERO_BIN." >&2
    exit 2
fi
pass "${CODERO_BIN}"
record "V-001" "PASS" "codero-binary-reachable" "${CODERO_BIN}"

# ── V-002: version ────────────────────────────────────────────────────────────
log "V-002  codero version"
ACTUAL_VERSION="$("${CODERO_BIN}" version 2>&1 || true)"
if [[ -n "${EXPECTED_VERSION}" ]]; then
    if [[ "${ACTUAL_VERSION}" != "${EXPECTED_VERSION}" ]]; then
        fail "version mismatch: expected '${EXPECTED_VERSION}', got '${ACTUAL_VERSION}'"
        record "V-002" "FAIL" "codero-version" "expected=${EXPECTED_VERSION} actual=${ACTUAL_VERSION}"
    else
        pass "version=${ACTUAL_VERSION}"
        record "V-002" "PASS" "codero-version" "${ACTUAL_VERSION}"
    fi
else
    pass "version=${ACTUAL_VERSION} (no assertion)"
    record "V-002" "PASS" "codero-version" "${ACTUAL_VERSION}"
fi

# ── V-003: gate-status --json ─────────────────────────────────────────────────
log "V-003  codero gate-status --json"
GS_OUT="$("${CODERO_BIN}" gate-status --json 2>/dev/null || true)"
REQUIRED_KEYS=("status" "run_id" "comments" "progress_bar")
if ! echo "${GS_OUT}" | python3 -c "import sys,json; json.load(sys.stdin)" 2>/dev/null; then
    fail "gate-status --json: not valid JSON"
    record "V-003" "FAIL" "gate-status-json" "invalid JSON output"
else
    MISSING=""
    for key in "${REQUIRED_KEYS[@]}"; do
        if ! echo "${GS_OUT}" | grep -q "\"${key}\""; then
            MISSING="${MISSING}${key} "
        fi
    done
    if [[ -n "${MISSING}" ]]; then
        fail "gate-status --json: missing keys: ${MISSING}"
        record "V-003" "FAIL" "gate-status-json" "missing keys: ${MISSING}"
    else
        pass "gate-status --json: valid, all required keys present"
        record "V-003" "PASS" "gate-status-json" "keys: ${REQUIRED_KEYS[*]}"
    fi
fi

# ── V-004: codero ports ───────────────────────────────────────────────────────
log "V-004  codero ports"
if "${CODERO_BIN}" ports >/dev/null 2>&1; then
    pass "codero ports: exit 0"
    record "V-004" "PASS" "codero-ports" "exit 0"
else
    fail "codero ports: non-zero exit"
    record "V-004" "FAIL" "codero-ports" "non-zero exit"
fi

# ── V-005: codero status ──────────────────────────────────────────────────────
log "V-005  codero status"
if "${CODERO_BIN}" status >/dev/null 2>&1; then
    pass "codero status: exit 0"
    record "V-005" "PASS" "codero-status" "exit 0"
else
    fail "codero status: non-zero exit"
    record "V-005" "FAIL" "codero-status" "non-zero exit"
fi

# ── V-006: codero prove --fast ────────────────────────────────────────────────
log "V-006  codero prove ${FAST_FLAG:---fast}"
PROVE_JSON_TMP="$(mktemp)"
# shellcheck disable=SC2064
trap "rm -f '${PROVE_JSON_TMP}'" EXIT

PROVE_ARGS=(prove)
[[ -n "${FAST_FLAG}" ]] && PROVE_ARGS+=(--fast)
PROVE_ARGS+=(--json --repo-path "${REPO_PATH}")
[[ -n "${ENDPOINT_URL}" ]] && PROVE_ARGS+=(--endpoint-url "${ENDPOINT_URL}")

PROVE_EXIT=0
"${CODERO_BIN}" "${PROVE_ARGS[@]}" 2>/dev/null >"${PROVE_JSON_TMP}" || PROVE_EXIT=$?

if [[ ${PROVE_EXIT} -ne 0 ]]; then
    fail "codero prove exited ${PROVE_EXIT}"
    PROVE_OVERALL="$(python3 -c "import sys,json; d=json.load(open('${PROVE_JSON_TMP}')); print(d.get('overall_status','unknown'))" 2>/dev/null || echo "unknown")"
    record "V-006" "FAIL" "codero-prove" "exit=${PROVE_EXIT} overall=${PROVE_OVERALL}"
else
    PROVE_OVERALL="$(python3 -c "import sys,json; d=json.load(open('${PROVE_JSON_TMP}')); print(d.get('overall_status','unknown'))" 2>/dev/null || echo "unknown")"
    PROVE_PASSED="$(python3 -c "import sys,json; d=json.load(open('${PROVE_JSON_TMP}')); print(d.get('passed',0))" 2>/dev/null || echo "?")"
    PROVE_SKIPPED="$(python3 -c "import sys,json; d=json.load(open('${PROVE_JSON_TMP}')); print(d.get('skipped',0))" 2>/dev/null || echo "?")"
    PROVE_FAILED="$(python3 -c "import sys,json; d=json.load(open('${PROVE_JSON_TMP}')); print(d.get('failed',0))" 2>/dev/null || echo "?")"
    pass "prove: overall=${PROVE_OVERALL}  passed=${PROVE_PASSED}  skipped=${PROVE_SKIPPED}  failed=${PROVE_FAILED}"
    record "V-006" "PASS" "codero-prove" "overall=${PROVE_OVERALL} passed=${PROVE_PASSED} skipped=${PROVE_SKIPPED} failed=${PROVE_FAILED}"
fi

# ── V-007/008/009: endpoint smoke ─────────────────────────────────────────────
if [[ -z "${ENDPOINT_URL}" ]]; then
    skip "V-007  /health endpoint: SKIP (--endpoint-url not provided; daemon absent)"
    skip "V-008  /gate endpoint: SKIP (--endpoint-url not provided; daemon absent)"
    skip "V-009  /dashboard/ endpoint: SKIP (--endpoint-url not provided; daemon absent)"
    record "V-007" "SKIP" "endpoint-health"     "daemon absent — pass --endpoint-url to enable"
    record "V-008" "SKIP" "endpoint-gate"       "daemon absent — pass --endpoint-url to enable"
    record "V-009" "SKIP" "endpoint-dashboard"  "daemon absent — pass --endpoint-url to enable"
else
    for spec in "V-007|/health|endpoint-health" "V-008|/gate|endpoint-gate" "V-009|/dashboard/|endpoint-dashboard"; do
        IFS='|' read -r vid path name <<< "$spec"
        log "${vid}  ${ENDPOINT_URL}${path}"
        HTTP_STATUS="$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "${ENDPOINT_URL}${path}" 2>/dev/null || echo "000")"
        if [[ "${HTTP_STATUS}" == "200" ]]; then
            pass "${vid} ${path}: HTTP ${HTTP_STATUS}"
            record "${vid}" "PASS" "${name}" "HTTP 200"
        else
            fail "${vid} ${path}: HTTP ${HTTP_STATUS}"
            record "${vid}" "FAIL" "${name}" "HTTP ${HTTP_STATUS}"
        fi
    done
fi

# ── V-010: checksum file ──────────────────────────────────────────────────────
log "V-010  checksum file"
RELEASES_DIR="/srv/storage/shared/tools/releases"
VERSION_TAG="${EXPECTED_VERSION:-$(${CODERO_BIN} version 2>/dev/null || echo "")}"
CHECKSUM_FILE="${RELEASES_DIR}/codero-${VERSION_TAG}/codero.sha256"
if [[ -f "${CHECKSUM_FILE}" ]]; then
    CHECKSUM_VAL="$(cat "${CHECKSUM_FILE}")"
    pass "checksum file present: ${CHECKSUM_FILE}"
    record "V-010" "PASS" "checksum-file" "${CHECKSUM_VAL:0:16}…"
else
    skip "V-010  checksum file: SKIP (${CHECKSUM_FILE} not found — expected in releases dir)"
    record "V-010" "SKIP" "checksum-file" "${CHECKSUM_FILE} absent"
fi

# ── summary table ─────────────────────────────────────────────────────────────
TOTAL=$(( PASS_COUNT + FAIL_COUNT + SKIP_COUNT ))
OVERALL="PASS"
[[ ${FAIL_COUNT} -gt 0 ]] && OVERALL="FAIL"

log ""
log "═══════════════════════════════════════════════════════"
log " Post-Release Verification Summary"
log "═══════════════════════════════════════════════════════"
printf "%-8s  %-30s  %-6s  %s\n" "ID" "NAME" "STATUS" "DETAIL" >&2
printf "%-8s  %-30s  %-6s  %s\n" "──────" "──────────────────────────────" "──────" "──────" >&2
for row in "${RESULTS[@]}"; do
    IFS='|' read -r id status name detail <<< "$row"
    icon="✅"
    [[ "$status" == "FAIL" ]] && icon="❌"
    [[ "$status" == "SKIP" ]] && icon="⏭ "
    printf "%s %-6s  %-30s  %-6s  %s\n" "$icon" "$id" "$name" "$status" "$detail" >&2
done
log "───────────────────────────────────────────────────────"
log " overall=${OVERALL}  passed=${PASS_COUNT}  failed=${FAIL_COUNT}  skipped=${SKIP_COUNT}  total=${TOTAL}"
log "═══════════════════════════════════════════════════════"

# ── JSON output ───────────────────────────────────────────────────────────────
if [[ "${EMIT_JSON}" == "true" ]]; then
    CHECKS_JSON=""
    for row in "${RESULTS[@]}"; do
        IFS='|' read -r id status name detail <<< "$row"
        # lowercase status for JSON consistency with codero prove
        STATUS_LC="$(echo "${status}" | tr '[:upper:]' '[:lower:]')"
        DETAIL_ESCAPED="${detail//\"/\\\"}"
        CHECKS_JSON="${CHECKS_JSON}{\"id\":\"${id}\",\"status\":\"${STATUS_LC}\",\"name\":\"${name}\",\"detail\":\"${DETAIL_ESCAPED}\"},"
    done
    CHECKS_JSON="[${CHECKS_JSON%,}]"
    printf '{"schema_version":"1","timestamp":"%s","version":"%s","overall_status":"%s","passed":%d,"failed":%d,"skipped":%d,"total":%d,"checks":%s}\n' \
        "${START_TS}" "${ACTUAL_VERSION}" "${OVERALL}" "${PASS_COUNT}" "${FAIL_COUNT}" "${SKIP_COUNT}" "${TOTAL}" "${CHECKS_JSON}"
fi

# ── exit ──────────────────────────────────────────────────────────────────────
[[ ${FAIL_COUNT} -gt 0 ]] && exit 1
exit 0
