#!/usr/bin/env bash
# KEEP_LOCAL: required for shipped runtime — unified release health report
#
# release-status.sh — Consolidated release health report for Codero.
#
# Aggregates outputs from:
#   - codero prove --fast --json            (proving gate: C-001..C-022)
#   - scripts/automation/verify-release.sh  (post-release checks: V-001..V-010)
#   - scripts/automation/validate-release-record.sh (release record schema: RR-001)
#   - scripts/release/dry-run-patch-release.sh (patch simulation: DR-001..DR-009)
#     [only when --include-dry-run is set]
#
# Usage:
#   ./scripts/automation/release-status.sh [OPTIONS]
#
# Options:
#   --version VERSION        Target release version (default: v1.2.0)
#   --json                   Emit structured JSON summary to stdout; human log to stderr
#   --strict                 Treat SKIP as FAIL for non-daemon checks (V-010, RR-001)
#   --include-dry-run        Also run patch dry-run simulation (DR-001..DR-009)
#   --repo-path PATH         Path to repo root (default: auto-detected)
#   --no-fast                Disable --fast flag on prove (enables race checks)
#   --help                   Print this help and exit
#
# Output schema (--json):
#   schema_version, generated_at, target_version, overall_status,
#   totals{pass,fail,skip,total}, sources{prove,verify,validate,dry_run},
#   checks[]{id,group,name,status,details,source}, drift{...}
#
# Exit codes:
#   0  overall_status=PASS (zero FAILs; SKIPs acceptable unless --strict)
#   1  overall_status=FAIL (one or more FAILs)
#   2  Environment / prerequisite error
#
# Notes:
#   - All sub-commands run read-only and non-destructive.
#   - Existing commands remain independently callable; this script only aggregates.
#   - Idempotent: temp files cleaned up on exit.

set -euo pipefail

# ── defaults ──────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

TARGET_VERSION="v1.2.0"
EMIT_JSON=false
STRICT=false
INCLUDE_DRY_RUN=false
FAST_FLAG="--fast"
START_TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ── parse args ────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --version)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --version requires a value" >&2; exit 2; }
            TARGET_VERSION="$2"; shift 2 ;;
        --repo-path)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --repo-path requires a value" >&2; exit 2; }
            REPO_ROOT="$2"; shift 2 ;;
        --json)           EMIT_JSON=true;        shift ;;
        --strict)         STRICT=true;           shift ;;
        --include-dry-run) INCLUDE_DRY_RUN=true; shift ;;
        --no-fast)        FAST_FLAG="";          shift ;;
        --help)
            sed -n '/^# release-status/,/^set -euo/{ /^set/d; s/^# \?//; p }' "$0"
            exit 0 ;;
        *) echo "[ERROR] Unknown option: $1" >&2; exit 2 ;;
    esac
done

REPO_ROOT="$(cd "${REPO_ROOT}" && pwd)"

# ── locate codero binary ──────────────────────────────────────────────────────
CODERO_BIN="${CODERO_BIN:-}"
if [[ -z "${CODERO_BIN}" ]]; then
    if [[ -x "${REPO_ROOT}/bin/codero" ]]; then
        CODERO_BIN="${REPO_ROOT}/bin/codero"
    elif command -v codero >/dev/null 2>&1; then
        CODERO_BIN="$(command -v codero)"
    else
        echo "[ERROR] codero binary not found. Set CODERO_BIN or ensure codero is in PATH." >&2
        exit 2
    fi
fi

# ── temp dir ──────────────────────────────────────────────────────────────────
WORK_DIR="$(mktemp -d)"
cleanup() { rm -rf "${WORK_DIR}"; }
trap cleanup EXIT

PROVE_JSON="${WORK_DIR}/prove.json"
VERIFY_JSON="${WORK_DIR}/verify.json"
DRYRUN_JSON="${WORK_DIR}/dryrun.json"

# ── result tracking ───────────────────────────────────────────────────────────
# Each entry: "ID|GROUP|NAME|STATUS|DETAILS|SOURCE"
declare -a ALL_CHECKS=()
FAIL_COUNT=0
PASS_COUNT=0
SKIP_COUNT=0

add_check() {
    local id="$1" group="$2" name="$3" status="$4" details="${5:-}" source="${6:-}"
    # normalize status to lowercase
    status="$(echo "${status}" | tr '[:upper:]' '[:lower:]')"
    # apply strict: convert skip → fail for selected groups
    if [[ "${STRICT}" == "true" && "${status}" == "skip" ]]; then
        case "${group}" in
            validate|drift) status="fail" ;;
        esac
    fi
    ALL_CHECKS+=("${id}|${group}|${name}|${status}|${details}|${source}")
    case "${status}" in
        pass) PASS_COUNT=$(( PASS_COUNT + 1 )) ;;
        fail) FAIL_COUNT=$(( FAIL_COUNT + 1 )) ;;
        skip) SKIP_COUNT=$(( SKIP_COUNT + 1 )) ;;
    esac
}

log()  { echo "[$(date -u +%H:%M:%S)] $*" >&2; }
pass() { log "PASS  $*"; }
fail() { log "FAIL  $*"; }
skip() { log "SKIP  $*"; }

# ── preamble ──────────────────────────────────────────────────────────────────
log "════════════════════════════════════════════════════════"
log " Codero Unified Release Status Report"
log "════════════════════════════════════════════════════════"
log " target-version  : ${TARGET_VERSION}"
log " codero-bin      : ${CODERO_BIN}"
log " strict          : ${STRICT}"
log " include-dry-run : ${INCLUDE_DRY_RUN}"
log " generated-at    : ${START_TS}"
log "════════════════════════════════════════════════════════"

# ── SOURCE: prove ─────────────────────────────────────────────────────────────
log ""
log "── Source: prove ────────────────────────────────────────"
PROVE_ARGS=(prove)
[[ -n "${FAST_FLAG}" ]] && PROVE_ARGS+=("${FAST_FLAG}")
PROVE_ARGS+=(--json --repo-path "${REPO_ROOT}")

PROVE_EXIT=0
CODERO_BIN="${CODERO_BIN}" \
    "${CODERO_BIN}" "${PROVE_ARGS[@]}" 2>/dev/null >"${PROVE_JSON}" || PROVE_EXIT=$?

PROVE_OVERALL="unknown"
PROVE_PASSED=0
PROVE_FAILED=0
PROVE_SKIPPED=0
PROVE_TOTAL=0

if [[ -s "${PROVE_JSON}" ]]; then
    PROVE_OVERALL="$(python3 -c "import json; d=json.load(open('${PROVE_JSON}')); print(d.get('overall_status','unknown'))" 2>/dev/null || echo "unknown")"
    PROVE_PASSED="$(  python3 -c "import json; d=json.load(open('${PROVE_JSON}')); print(d.get('passed',0))"  2>/dev/null || echo 0)"
    PROVE_FAILED="$(  python3 -c "import json; d=json.load(open('${PROVE_JSON}')); print(d.get('failed',0))"  2>/dev/null || echo 0)"
    PROVE_SKIPPED="$( python3 -c "import json; d=json.load(open('${PROVE_JSON}')); print(d.get('skipped',0))" 2>/dev/null || echo 0)"
    PROVE_TOTAL="$(   python3 -c "import json; d=json.load(open('${PROVE_JSON}')); print(d.get('total',0))"   2>/dev/null || echo 0)"

    # Ingest individual checks
    while IFS=$'\t' read -r cid cname cstat cdetail; do
        [[ -z "${cid}" ]] && continue
        add_check "${cid}" "prove" "${cname}" "${cstat}" "${cdetail}" "prove"
    done < <(python3 - "${PROVE_JSON}" <<'PYEOF'
import json, sys
data = json.load(open(sys.argv[1]))
for c in data.get("checks", []):
    cid    = c.get("id", "")
    cname  = c.get("name", "")
    cstat  = c.get("status", "skip").upper()
    cdetail = c.get("detail", "")
    print(f"{cid}\t{cname}\t{cstat}\t{cdetail}")
PYEOF
    )

    log "prove: overall=${PROVE_OVERALL} passed=${PROVE_PASSED} skipped=${PROVE_SKIPPED} failed=${PROVE_FAILED}"
else
    fail "prove: no JSON output (exit ${PROVE_EXIT})"
    add_check "C-000" "prove" "prove-gate-exec" "fail" "codero prove exited ${PROVE_EXIT} with no output" "prove"
    PROVE_OVERALL="FAIL"
fi

# ── SOURCE: verify ────────────────────────────────────────────────────────────
log ""
log "── Source: verify ───────────────────────────────────────"
VERIFY_SCRIPT="${REPO_ROOT}/scripts/automation/verify-release.sh"
VERIFY_EXIT=0
VERIFY_OVERALL="unknown"
VERIFY_PASSED=0
VERIFY_FAILED=0
VERIFY_SKIPPED=0
VERIFY_TOTAL=0

if [[ ! -x "${VERIFY_SCRIPT}" ]]; then
    fail "verify-release.sh not found or not executable: ${VERIFY_SCRIPT}"
    add_check "V-000" "verify" "verify-script-exists" "fail" "${VERIFY_SCRIPT} not found" "verify"
    VERIFY_OVERALL="FAIL"
else
    CODERO_BIN="${CODERO_BIN}" \
        bash "${VERIFY_SCRIPT}" \
            --version "${TARGET_VERSION}" \
            --repo-path "${REPO_ROOT}" \
            ${FAST_FLAG:+${FAST_FLAG}} \
            --json \
            2>/dev/null >"${VERIFY_JSON}" || VERIFY_EXIT=$?

    if [[ -s "${VERIFY_JSON}" ]]; then
        VERIFY_OVERALL="$(  python3 -c "import json; d=json.load(open('${VERIFY_JSON}')); print(d.get('overall_status','unknown'))" 2>/dev/null || echo "unknown")"
        VERIFY_PASSED="$(   python3 -c "import json; d=json.load(open('${VERIFY_JSON}')); print(d.get('passed',0))"   2>/dev/null || echo 0)"
        VERIFY_FAILED="$(   python3 -c "import json; d=json.load(open('${VERIFY_JSON}')); print(d.get('failed',0))"   2>/dev/null || echo 0)"
        VERIFY_SKIPPED="$(  python3 -c "import json; d=json.load(open('${VERIFY_JSON}')); print(d.get('skipped',0))"  2>/dev/null || echo 0)"
        VERIFY_TOTAL="$(    python3 -c "import json; d=json.load(open('${VERIFY_JSON}')); print(d.get('total',0))"    2>/dev/null || echo 0)"

        while IFS=$'\t' read -r cid cname cstat cdetail; do
            [[ -z "${cid}" ]] && continue
            add_check "${cid}" "verify" "${cname}" "${cstat}" "${cdetail}" "verify"
        done < <(python3 - "${VERIFY_JSON}" <<'PYEOF'
import json, sys
data = json.load(open(sys.argv[1]))
for c in data.get("checks", []):
    cid    = c.get("id", "")
    cname  = c.get("name", "")
    cstat  = c.get("status", "skip").upper()
    cdetail = c.get("detail", "")
    print(f"{cid}\t{cname}\t{cstat}\t{cdetail}")
PYEOF
        )
        log "verify: overall=${VERIFY_OVERALL} passed=${VERIFY_PASSED} skipped=${VERIFY_SKIPPED} failed=${VERIFY_FAILED}"
    else
        fail "verify-release.sh exited ${VERIFY_EXIT} with no JSON output"
        add_check "V-000" "verify" "verify-exec" "fail" "verify-release.sh exited ${VERIFY_EXIT} with no output" "verify"
        VERIFY_OVERALL="FAIL"
    fi
fi

# ── SOURCE: validate release record ──────────────────────────────────────────
log ""
log "── Source: validate ─────────────────────────────────────"
VALIDATE_SCRIPT="${REPO_ROOT}/scripts/automation/validate-release-record.sh"
RECORD_FILE="${REPO_ROOT}/docs/runbooks/releases/${TARGET_VERSION}.yaml"
VALIDATE_OVERALL="unknown"

if [[ ! -x "${VALIDATE_SCRIPT}" ]]; then
    fail "validate-release-record.sh not found: ${VALIDATE_SCRIPT}"
    add_check "RR-001" "validate" "release-record-schema" "fail" "validate script not found" "validate"
    VALIDATE_OVERALL="FAIL"
elif [[ ! -f "${RECORD_FILE}" ]]; then
    skip "RR-001 release record absent: ${RECORD_FILE}"
    add_check "RR-001" "validate" "release-record-exists" "skip" "record not found: ${RECORD_FILE}" "validate"
    VALIDATE_OVERALL="SKIP"
else
    VALIDATE_OUT=""
    VALIDATE_EXIT=0
    VALIDATE_OUT="$(bash "${VALIDATE_SCRIPT}" "${RECORD_FILE}" 2>&1)" || VALIDATE_EXIT=$?
    if [[ ${VALIDATE_EXIT} -eq 0 ]]; then
        pass "validate: ${VALIDATE_OUT}"
        add_check "RR-001" "validate" "release-record-schema" "pass" "${RECORD_FILE}" "validate"
        VALIDATE_OVERALL="PASS"
    else
        fail "validate: ${VALIDATE_OUT}"
        add_check "RR-001" "validate" "release-record-schema" "fail" "${VALIDATE_OUT}" "validate"
        VALIDATE_OVERALL="FAIL"
    fi
fi

# ── drift detection ───────────────────────────────────────────────────────────
log ""
log "── Source: drift ────────────────────────────────────────"
DRIFT_RECORD_OK=false
DRIFT_ARTIFACT_PRESENT=false
DRIFT_CHECKSUM_MATCH="null"
DRIFT_NOTES=""

if [[ -f "${RECORD_FILE}" ]]; then
    RECORD_VERSION="$(  grep -E "^version:"         "${RECORD_FILE}" | sed 's/version:[[:space:]]*//' | tr -d '"' | xargs)"
    RECORD_SHA256="$(   grep -E "^artifact_sha256:"  "${RECORD_FILE}" | sed 's/artifact_sha256:[[:space:]]*//' | tr -d '"' | xargs)"
    RECORD_ARTIFACT="$( grep -E "^artifact_path:"    "${RECORD_FILE}" | sed 's/artifact_path:[[:space:]]*//' | tr -d '"' | xargs)"

    # DR-V01: version field consistency
    if [[ "${RECORD_VERSION}" == "${TARGET_VERSION}" ]]; then
        pass "drift: record version=${RECORD_VERSION} matches target"
        add_check "DR-V01" "drift" "drift-version" "pass" "record=${RECORD_VERSION} target=${TARGET_VERSION}" "drift"
        DRIFT_RECORD_OK=true
    else
        fail "drift: record version=${RECORD_VERSION} != target=${TARGET_VERSION}"
        add_check "DR-V01" "drift" "drift-version" "fail" "record=${RECORD_VERSION} target=${TARGET_VERSION}" "drift"
    fi

    # DR-V02: artifact SHA-256 cross-check
    if [[ -f "${RECORD_ARTIFACT}" ]]; then
        DRIFT_ARTIFACT_PRESENT=true
        ACTUAL_SHA="$(sha256sum "${RECORD_ARTIFACT}" | awk '{print $1}')"
        if [[ "${ACTUAL_SHA}" == "${RECORD_SHA256}" ]]; then
            DRIFT_CHECKSUM_MATCH="true"
            pass "drift: artifact SHA match (${ACTUAL_SHA:0:16}…)"
            add_check "DR-V02" "drift" "drift-artifact-sha" "pass" "sha256 match: ${ACTUAL_SHA:0:16}…" "drift"
        else
            DRIFT_CHECKSUM_MATCH="false"
            fail "drift: SHA mismatch: expected=${RECORD_SHA256:0:16}… got=${ACTUAL_SHA:0:16}…"
            add_check "DR-V02" "drift" "drift-artifact-sha" "fail" "expected=${RECORD_SHA256:0:16}… got=${ACTUAL_SHA:0:16}…" "drift"
        fi
        DRIFT_NOTES="Artifact found at ${RECORD_ARTIFACT}"
    else
        DRIFT_ARTIFACT_PRESENT=false
        skip "drift: artifact not found at ${RECORD_ARTIFACT}"
        add_check "DR-V02" "drift" "drift-artifact-sha" "skip" \
            "artifact not available: ${RECORD_ARTIFACT}; SHA cross-check requires local storage mount" "drift"
        DRIFT_NOTES="Artifact path ${RECORD_ARTIFACT} not available; SHA cross-check is local-only"
    fi
else
    skip "drift: no release record to check"
    add_check "DR-V01" "drift" "drift-version"      "skip" "release record absent: ${RECORD_FILE}" "drift"
    add_check "DR-V02" "drift" "drift-artifact-sha" "skip" "release record absent: ${RECORD_FILE}" "drift"
    DRIFT_NOTES="Release record absent at ${RECORD_FILE}"
fi

# ── SOURCE: dry-run (optional) ────────────────────────────────────────────────
DRYRUN_OVERALL="null"
DRYRUN_PASSED=0
DRYRUN_FAILED=0
DRYRUN_SKIPPED=0
DRYRUN_TOTAL=0
DRYRUN_SHA256=""

if [[ "${INCLUDE_DRY_RUN}" == "true" ]]; then
    log ""
    log "── Source: dry-run ──────────────────────────────────────"
    DRYRUN_SCRIPT="${REPO_ROOT}/scripts/release/dry-run-patch-release.sh"
    DRYRUN_EXIT=0

    if [[ ! -x "${DRYRUN_SCRIPT}" ]]; then
        fail "dry-run-patch-release.sh not found: ${DRYRUN_SCRIPT}"
        add_check "DR-000" "dry_run" "dry-run-script-exists" "fail" "${DRYRUN_SCRIPT} not found" "dry_run"
        DRYRUN_OVERALL="FAIL"
    else
        # Derive a sensible target version for dry-run (next patch)
        DRYRUN_TARGET="$(echo "${TARGET_VERSION}" | sed 's/\.[0-9]*$/.1-rc.dryrun/')"
        bash "${DRYRUN_SCRIPT}" \
            --target-version "${DRYRUN_TARGET}" \
            --base-version   "${TARGET_VERSION}" \
            --json \
            2>/dev/null >"${DRYRUN_JSON}" || DRYRUN_EXIT=$?

        if [[ -s "${DRYRUN_JSON}" ]]; then
            DRYRUN_OVERALL="$(  python3 -c "import json; d=json.load(open('${DRYRUN_JSON}')); print(d.get('overall_status','unknown'))" 2>/dev/null || echo "unknown")"
            DRYRUN_PASSED="$(   python3 -c "import json; d=json.load(open('${DRYRUN_JSON}')); print(d.get('passed',0))"   2>/dev/null || echo 0)"
            DRYRUN_FAILED="$(   python3 -c "import json; d=json.load(open('${DRYRUN_JSON}')); print(d.get('failed',0))"   2>/dev/null || echo 0)"
            DRYRUN_SKIPPED="$(  python3 -c "import json; d=json.load(open('${DRYRUN_JSON}')); print(d.get('skipped',0))"  2>/dev/null || echo 0)"
            DRYRUN_TOTAL="$(    python3 -c "import json; d=json.load(open('${DRYRUN_JSON}')); print(d.get('total',0))"    2>/dev/null || echo 0)"
            DRYRUN_SHA256="$(   python3 -c "import json; d=json.load(open('${DRYRUN_JSON}')); print(d.get('sha256',''))"  2>/dev/null || echo "")"

            while IFS=$'\t' read -r cid cname cstat cdetail; do
                [[ -z "${cid}" ]] && continue
                add_check "${cid}" "dry_run" "${cname}" "${cstat}" "${cdetail}" "dry_run"
            done < <(python3 - "${DRYRUN_JSON}" <<'PYEOF'
import json, sys
data = json.load(open(sys.argv[1]))
for c in data.get("checks", []):
    cid    = c.get("id", "")
    cname  = c.get("name", "")
    cstat  = c.get("status", "skip").upper()
    cdetail = c.get("detail", "")
    print(f"{cid}\t{cname}\t{cstat}\t{cdetail}")
PYEOF
            )
            log "dry-run: overall=${DRYRUN_OVERALL} passed=${DRYRUN_PASSED} skipped=${DRYRUN_SKIPPED} failed=${DRYRUN_FAILED}"
        else
            fail "dry-run-patch-release.sh exited ${DRYRUN_EXIT} with no JSON output"
            add_check "DR-000" "dry_run" "dry-run-exec" "fail" "exit=${DRYRUN_EXIT}; no JSON output" "dry_run"
            DRYRUN_OVERALL="FAIL"
        fi
    fi
fi

# ── compute overall ───────────────────────────────────────────────────────────
TOTAL=$(( PASS_COUNT + FAIL_COUNT + SKIP_COUNT ))
OVERALL="PASS"
[[ ${FAIL_COUNT} -gt 0 ]] && OVERALL="FAIL"

# ── human summary ─────────────────────────────────────────────────────────────
log ""
log "════════════════════════════════════════════════════════"
log " Unified Release Status — ${TARGET_VERSION}"
log "════════════════════════════════════════════════════════"
printf "%-8s  %-10s  %-30s  %-6s  %s\n" "ID" "GROUP" "NAME" "STATUS" "DETAILS" >&2
printf "%-8s  %-10s  %-30s  %-6s  %s\n" "──────" "──────────" "──────────────────────────────" "──────" "──────" >&2

for row in "${ALL_CHECKS[@]}"; do
    IFS='|' read -r cid cgroup cname cstatus cdetails csource <<< "$row"
    icon="PASS"
    [[ "$cstatus" == "fail" ]] && icon="FAIL"
    [[ "$cstatus" == "skip" ]] && icon="SKIP"
    printf "[%-4s] %-6s  %-10s  %-30s  %s\n" "$icon" "$cid" "$cgroup" "$cname" "$cdetails" >&2
done

log "────────────────────────────────────────────────────────"
log " Sources:"
log "   prove    : overall=${PROVE_OVERALL} passed=${PROVE_PASSED} skipped=${PROVE_SKIPPED} failed=${PROVE_FAILED}"
log "   verify   : overall=${VERIFY_OVERALL} passed=${VERIFY_PASSED} skipped=${VERIFY_SKIPPED} failed=${VERIFY_FAILED}"
log "   validate : overall=${VALIDATE_OVERALL}"
if [[ "${INCLUDE_DRY_RUN}" == "true" ]]; then
    log "   dry_run  : overall=${DRYRUN_OVERALL} passed=${DRYRUN_PASSED} skipped=${DRYRUN_SKIPPED} failed=${DRYRUN_FAILED}"
fi
log "────────────────────────────────────────────────────────"
log " overall=${OVERALL}  passed=${PASS_COUNT}  failed=${FAIL_COUNT}  skipped=${SKIP_COUNT}  total=${TOTAL}"
[[ "${STRICT}" == "true" ]] && log " [strict mode: SKIP→FAIL for validate/drift groups]"
log "════════════════════════════════════════════════════════"

# ── JSON output ───────────────────────────────────────────────────────────────
if [[ "${EMIT_JSON}" == "true" ]]; then
    # Build checks JSON array
    CHECKS_JSON="[]"
    for row in "${ALL_CHECKS[@]}"; do
        IFS='|' read -r cid cgroup cname cstatus cdetails csource <<< "$row"
        CHECKS_JSON="$(jq -c \
            --arg id      "${cid}" \
            --arg group   "${cgroup}" \
            --arg name    "${cname}" \
            --arg status  "${cstatus}" \
            --arg details "${cdetails}" \
            --arg source  "${csource}" \
            '. + [{id:$id,group:$group,name:$name,status:$status,details:$details,source:$source}]' \
            <<<"${CHECKS_JSON}")"
    done

    # Build sources block
    PROVE_SRC_JSON="$(jq -nc \
        --arg overall "${PROVE_OVERALL}" \
        --argjson passed  "${PROVE_PASSED}" \
        --argjson failed  "${PROVE_FAILED}" \
        --argjson skipped "${PROVE_SKIPPED}" \
        --argjson total   "${PROVE_TOTAL}" \
        '{overall_status:$overall,passed:$passed,failed:$failed,skipped:$skipped,total:$total}')"

    VERIFY_SRC_JSON="$(jq -nc \
        --arg overall "${VERIFY_OVERALL}" \
        --argjson passed  "${VERIFY_PASSED}" \
        --argjson failed  "${VERIFY_FAILED}" \
        --argjson skipped "${VERIFY_SKIPPED}" \
        --argjson total   "${VERIFY_TOTAL}" \
        '{overall_status:$overall,passed:$passed,failed:$failed,skipped:$skipped,total:$total}')"

    VALIDATE_SRC_JSON="$(jq -nc \
        --arg overall  "${VALIDATE_OVERALL}" \
        --arg record   "${RECORD_FILE}" \
        '{overall_status:$overall,record_path:$record}')"

    if [[ "${INCLUDE_DRY_RUN}" == "true" ]]; then
        DRYRUN_SRC_JSON="$(jq -nc \
            --arg overall "${DRYRUN_OVERALL}" \
            --argjson passed  "${DRYRUN_PASSED}" \
            --argjson failed  "${DRYRUN_FAILED}" \
            --argjson skipped "${DRYRUN_SKIPPED}" \
            --argjson total   "${DRYRUN_TOTAL}" \
            --arg sha256  "${DRYRUN_SHA256}" \
            '{overall_status:$overall,passed:$passed,failed:$failed,skipped:$skipped,total:$total,sha256:$sha256}')"
    else
        DRYRUN_SRC_JSON="null"
    fi

    # Build drift block
    DRIFT_JSON="$(jq -nc \
        --argjson record_ok  "${DRIFT_RECORD_OK}" \
        --argjson art_present "${DRIFT_ARTIFACT_PRESENT}" \
        --argjson chk_match  "${DRIFT_CHECKSUM_MATCH}" \
        --arg notes          "${DRIFT_NOTES}" \
        '{release_record_ok:$record_ok,artifact_present:$art_present,checksum_match:$chk_match,notes:$notes}')"

    jq -nc \
        --arg schema_version   "1" \
        --arg generated_at     "${START_TS}" \
        --arg target_version   "${TARGET_VERSION}" \
        --arg overall_status   "${OVERALL}" \
        --argjson passed       "${PASS_COUNT}" \
        --argjson failed       "${FAIL_COUNT}" \
        --argjson skipped      "${SKIP_COUNT}" \
        --argjson total        "${TOTAL}" \
        --argjson prove_src    "${PROVE_SRC_JSON}" \
        --argjson verify_src   "${VERIFY_SRC_JSON}" \
        --argjson validate_src "${VALIDATE_SRC_JSON}" \
        --argjson dry_run_src  "${DRYRUN_SRC_JSON}" \
        --argjson checks       "${CHECKS_JSON}" \
        --argjson drift        "${DRIFT_JSON}" \
        '{
          schema_version:  $schema_version,
          generated_at:    $generated_at,
          target_version:  $target_version,
          overall_status:  $overall_status,
          totals: {
            pass:  $passed,
            fail:  $failed,
            skip:  $skipped,
            total: $total
          },
          sources: {
            prove:    $prove_src,
            verify:   $verify_src,
            validate: $validate_src,
            dry_run:  $dry_run_src
          },
          checks: $checks,
          drift:  $drift
        }'
fi

# ── exit ──────────────────────────────────────────────────────────────────────
[[ ${FAIL_COUNT} -gt 0 ]] && exit 1
exit 0
