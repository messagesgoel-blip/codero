#!/usr/bin/env bash
# KEEP_LOCAL: required for shipped runtime — stabilization triage helper
#
# stabilization-triage.sh — Triage automation for stabilization watch runs.
#
# Compares a current release-status JSON against a baseline and (optionally)
# a previous watch-run JSON to classify each check and determine regression state.
#
# Usage:
#   ./scripts/automation/stabilization-triage.sh [OPTIONS]
#
# Options:
#   --current  FILE    Path to the current release-status JSON (required)
#   --baseline FILE    Path to the baseline release-status JSON (required)
#   --previous FILE    Path to previous watch-run release-status JSON (optional)
#                      Used to distinguish persistent failures from new transients.
#   --output   FORMAT  Output format: md (default) | json | both
#   --help             Print this help and exit
#
# Output:
#   Machine JSON to stdout.
#   Human-readable summary to stderr.
#
# Exit codes:
#   0  No NEW_FAILs (regression-free)
#   1  One or more NEW_FAILs detected (regression)
#   2  Usage / prerequisite error
#
# Triage categories:
#   NEW_FAIL    Check is fail in current, was pass or skip in baseline → regression
#   NEW_SKIP    Check is skip in current, was pass in baseline → env drift (warning, not failure)
#   RECOVERED   Check is pass/skip in current, was fail in baseline → improvement
#   UNCHANGED   Status unchanged or check not in baseline with non-fail status

set -euo pipefail

# ── defaults ──────────────────────────────────────────────────────────────────
CURRENT_FILE=""
BASELINE_FILE=""
PREVIOUS_FILE=""
OUTPUT_FORMAT="md"

# ── parse args ────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --current)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --current requires a value" >&2; exit 2; }
            CURRENT_FILE="$2"; shift 2 ;;
        --baseline)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --baseline requires a value" >&2; exit 2; }
            BASELINE_FILE="$2"; shift 2 ;;
        --previous)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --previous requires a value" >&2; exit 2; }
            PREVIOUS_FILE="$2"; shift 2 ;;
        --output)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --output requires a value" >&2; exit 2; }
            OUTPUT_FORMAT="$2"; shift 2 ;;
        --help)
            sed -n '/^# stabilization-triage/,/^set -euo/{ /^set/d; s/^# \?//; p }' "$0"
            exit 0 ;;
        *) echo "[ERROR] Unknown option: $1" >&2; exit 2 ;;
    esac
done

[[ -z "${CURRENT_FILE}"  ]] && { echo "[ERROR] --current is required"  >&2; exit 2; }
[[ -z "${BASELINE_FILE}" ]] && { echo "[ERROR] --baseline is required" >&2; exit 2; }
[[ ! -f "${CURRENT_FILE}"  ]] && { echo "[ERROR] --current file not found: ${CURRENT_FILE}"   >&2; exit 2; }
[[ ! -f "${BASELINE_FILE}" ]] && { echo "[ERROR] --baseline file not found: ${BASELINE_FILE}" >&2; exit 2; }
[[ -n "${PREVIOUS_FILE}" && ! -f "${PREVIOUS_FILE}" ]] && \
    { echo "[ERROR] --previous file not found: ${PREVIOUS_FILE}" >&2; exit 2; }

command -v python3 >/dev/null 2>&1 || { echo "[ERROR] python3 is required" >&2; exit 2; }
command -v jq      >/dev/null 2>&1 || { echo "[ERROR] jq is required"      >&2; exit 2; }

log() { echo "[$(date -u +%H:%M:%S)] $*" >&2; }

log "stabilization-triage: current=${CURRENT_FILE} baseline=${BASELINE_FILE}${PREVIOUS_FILE:+ previous=${PREVIOUS_FILE}}"

# ── compute triage via python3 ────────────────────────────────────────────────
TRIAGE_JSON="$(python3 - "${CURRENT_FILE}" "${BASELINE_FILE}" "${PREVIOUS_FILE:-}" <<'PYEOF'
import json, sys, datetime as dt

current_data  = json.load(open(sys.argv[1]))
baseline_data = json.load(open(sys.argv[2]))
prev_path     = sys.argv[3] if len(sys.argv) > 3 else ""
prev_data     = json.load(open(prev_path)) if prev_path else None

cur_ver  = current_data.get("target_version", "?")
base_ver = baseline_data.get("target_version", "?")
prev_ver = prev_data.get("target_version", None) if prev_data else None

# Build id → status lookup (lowercased)
def idx(data):
    return {c["id"]: c["status"].lower() for c in data.get("checks", [])}

cur_idx  = idx(current_data)
base_idx = idx(baseline_data)
prev_idx = idx(prev_data) if prev_data else {}

all_ids = sorted(set(list(cur_idx.keys()) + list(base_idx.keys())))

new_fails  = []
new_skips  = []
recovered  = []
unchanged  = []
changes    = []

for cid in all_ids:
    cs = cur_idx.get(cid)   # current status
    bs = base_idx.get(cid)  # baseline status
    ps = prev_idx.get(cid)  # previous run status (may be None)

    if bs is None:
        # Check absent from baseline (new check added after baseline cut).
        # Only escalate if it's actively failing.
        if cs == "fail":
            category = "NEW_FAIL"
            new_fails.append(cid)
        else:
            category = "UNCHANGED"
            unchanged.append(cid)
    elif cs == "fail" and bs in ("pass", "skip"):
        category = "NEW_FAIL"
        new_fails.append(cid)
    elif cs == "skip" and bs == "pass":
        category = "NEW_SKIP"
        new_skips.append(cid)
    elif bs == "fail" and cs in ("pass", "skip"):
        category = "RECOVERED"
        recovered.append(cid)
    else:
        # UNCHANGED: same status, or skip→skip, fail→fail, skip→pass (improvement), etc.
        category = "UNCHANGED"
        unchanged.append(cid)

    # Annotate persistent failures: NEW_FAIL also present in prior watch run
    persistent = bool(category == "NEW_FAIL" and ps == "fail")

    if category != "UNCHANGED":
        name    = next((c["name"]         for c in current_data.get("checks", [])  if c["id"] == cid),
                  next((c["name"]         for c in baseline_data.get("checks", []) if c["id"] == cid), ""))
        group   = next((c.get("group", "") for c in current_data.get("checks", [])  if c["id"] == cid),
                  next((c.get("group", "") for c in baseline_data.get("checks", []) if c["id"] == cid), ""))
        details = next((c.get("details", "") for c in current_data.get("checks", []) if c["id"] == cid), "")
        changes.append({
            "id":               cid,
            "group":            group,
            "name":             name,
            "baseline_status":  bs or "absent",
            "current_status":   cs or "absent",
            "category":         category,
            "persistent":       persistent,
            "details":          details,
        })

totals = {
    "new_fail":  len(new_fails),
    "new_skip":  len(new_skips),
    "recovered": len(recovered),
    "unchanged": len(unchanged),
}

result = {
    "schema_version":    "1",
    "triage_at":         dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "current_version":   cur_ver,
    "baseline_version":  base_ver,
    "previous_version":  prev_ver,
    "regression_free":   len(new_fails) == 0,
    "new_fails":         new_fails,
    "new_skips":         new_skips,
    "recovered":         recovered,
    "unchanged_count":   len(unchanged),
    "totals":            totals,
    "changes":           changes,
}
print(json.dumps(result, indent=2))
PYEOF
)"

# ── extract key fields ────────────────────────────────────────────────────────
CUR_VER="$(        echo "${TRIAGE_JSON}" | jq -r '.current_version')"
BASE_VER="$(       echo "${TRIAGE_JSON}" | jq -r '.baseline_version')"
PREV_VER="$(       echo "${TRIAGE_JSON}" | jq -r '.previous_version // "none"')"
REGRESSION_FREE="$(echo "${TRIAGE_JSON}" | jq -r '.regression_free')"
NEW_FAIL_COUNT="$( echo "${TRIAGE_JSON}" | jq    '.totals.new_fail')"
NEW_SKIP_COUNT="$( echo "${TRIAGE_JSON}" | jq    '.totals.new_skip')"
RECOVERED_COUNT="$(echo "${TRIAGE_JSON}" | jq    '.totals.recovered')"
UNCHANGED_COUNT="$(echo "${TRIAGE_JSON}" | jq    '.unchanged_count')"

log "current=${CUR_VER}  baseline=${BASE_VER}  previous=${PREV_VER}"
log "regression_free=${REGRESSION_FREE}  new_fail=${NEW_FAIL_COUNT}  new_skip=${NEW_SKIP_COUNT}  recovered=${RECOVERED_COUNT}  unchanged=${UNCHANGED_COUNT}"

# ── markdown output ───────────────────────────────────────────────────────────
emit_md() {
    echo "# Stabilization Triage: ${CUR_VER} vs baseline ${BASE_VER}"
    echo ""
    echo "**Triage:** $(echo "${TRIAGE_JSON}" | jq -r '.triage_at')  |  **Previous run:** ${PREV_VER}"
    echo ""
    echo "| Category     | Count |"
    echo "|---|---:|"
    echo "| NEW_FAIL     | ${NEW_FAIL_COUNT} |"
    echo "| NEW_SKIP     | ${NEW_SKIP_COUNT} |"
    echo "| RECOVERED    | ${RECOVERED_COUNT} |"
    echo "| UNCHANGED    | ${UNCHANGED_COUNT} |"
    echo ""

    NCHANGES="$(echo "${TRIAGE_JSON}" | jq '.changes | length')"
    if [[ "${NCHANGES}" -eq 0 ]]; then
        echo "**All checks UNCHANGED.** No regressions, no new skips, nothing recovered."
    else
        echo "## Check Changes"
        echo ""
        echo "| ID | Group | Name | Baseline | Current | Category | Persistent |"
        echo "|---|---|---|---|---|---|---|"
        echo "${TRIAGE_JSON}" | python3 -c "
import json, sys
d = json.load(sys.stdin)
for c in d['changes']:
    p = 'yes' if c['persistent'] else 'no'
    print(f\"| {c['id']} | {c['group']} | {c['name']} | {c['baseline_status']} | {c['current_status']} | {c['category']} | {p} |\")
"
    fi
    echo ""

    if [[ "${NEW_FAIL_COUNT}" -gt 0 ]]; then
        echo "## Regressions (NEW_FAIL)"
        echo ""
        echo "The following checks are FAIL in ${CUR_VER} but were not FAIL in baseline ${BASE_VER}:"
        echo ""
        echo "${TRIAGE_JSON}" | jq -r '.changes[] | select(.category == "NEW_FAIL") | "- \(.id) (\(.group)/\(.name))\(if .persistent then " [PERSISTENT — also failed in previous run]" else "" end): \(.details)"'
        echo ""
        echo "> **Backout criteria may be triggered.** See \`docs/runbooks/v1.2.1-stabilization-watch.md\`."
    fi

    if [[ "${NEW_SKIP_COUNT}" -gt 0 ]]; then
        echo ""
        echo "## Warnings (NEW_SKIP)"
        echo ""
        echo "The following checks are SKIP in ${CUR_VER} but were PASS in baseline (env drift — not a failure):"
        echo ""
        echo "${TRIAGE_JSON}" | jq -r '.changes[] | select(.category == "NEW_SKIP") | "- \(.id) (\(.group)/\(.name)): \(.details)"'
    fi

    if [[ "${RECOVERED_COUNT}" -gt 0 ]]; then
        echo ""
        echo "## Improvements (RECOVERED)"
        echo ""
        echo "${TRIAGE_JSON}" | jq -r '.changes[] | select(.category == "RECOVERED") | "- \(.id) (\(.group)/\(.name)): was \(.baseline_status) → now \(.current_status)"'
    fi
}

# ── emit output ───────────────────────────────────────────────────────────────
case "${OUTPUT_FORMAT}" in
    md)   emit_md ;;
    json) echo "${TRIAGE_JSON}" ;;
    both)
        emit_md
        echo ""
        echo "## Raw Triage JSON"
        echo ""
        echo '```json'
        echo "${TRIAGE_JSON}"
        echo '```'
        ;;
    *) echo "[ERROR] Unknown --output format: ${OUTPUT_FORMAT} (use md|json|both)" >&2; exit 2 ;;
esac

# ── exit code: non-zero only on NEW_FAILs ─────────────────────────────────────
[[ "${REGRESSION_FREE}" == "true" ]] && exit 0
exit 1
