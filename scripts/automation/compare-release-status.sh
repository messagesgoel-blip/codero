#!/usr/bin/env bash
# KEEP_LOCAL: required for shipped runtime — release-status comparison helper
#
# compare-release-status.sh — Compare two release-status JSON reports and
# emit a delta summary (human markdown + machine JSON).
#
# Usage:
#   ./scripts/automation/compare-release-status.sh [OPTIONS]
#
# Options:
#   --new      FILE    Path to the newer release-status JSON (required)
#   --baseline FILE    Path to the baseline release-status JSON (required)
#   --output   FORMAT  Output format: md (default) | json | both
#   --help             Print this help and exit
#
# Output:
#   Human-readable markdown table to stdout (or JSON if --output json/both).
#   Human log to stderr.
#
# Exit codes:
#   0  No new FAILs in --new relative to --baseline (regression-free)
#   1  One or more checks that PASS/SKIP in baseline are FAIL in new (regression)
#   2  Usage / prerequisite error
#
# Delta semantics:
#   NEW_FAIL    Check is fail in new, was pass or skip in baseline → regression
#   RECOVERED   Check is pass/skip in new, was fail in baseline → improvement
#   DEGRADED    Check is skip in new, was pass in baseline → env drift (not a FAIL)
#   IMPROVED    Check is pass in new, was skip in baseline → env improvement
#   STABLE      Check status is unchanged
#   NEW_CHECK   Check present in new but absent in baseline

set -euo pipefail

# ── defaults ──────────────────────────────────────────────────────────────────
NEW_FILE=""
BASELINE_FILE=""
OUTPUT_FORMAT="md"

# ── parse args ────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case "$1" in
        --new)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --new requires a value" >&2; exit 2; }
            NEW_FILE="$2"; shift 2 ;;
        --baseline)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --baseline requires a value" >&2; exit 2; }
            BASELINE_FILE="$2"; shift 2 ;;
        --output)
            [[ $# -lt 2 || "${2:-}" == --* ]] && { echo "[ERROR] --output requires a value" >&2; exit 2; }
            OUTPUT_FORMAT="$2"; shift 2 ;;
        --help)
            sed -n '/^# compare-release-status/,/^set -euo/{ /^set/d; s/^# \?//; p }' "$0"
            exit 0 ;;
        *) echo "[ERROR] Unknown option: $1" >&2; exit 2 ;;
    esac
done

[[ -z "${NEW_FILE}" ]]      && { echo "[ERROR] --new is required" >&2; exit 2; }
[[ -z "${BASELINE_FILE}" ]] && { echo "[ERROR] --baseline is required" >&2; exit 2; }
[[ ! -f "${NEW_FILE}" ]]      && { echo "[ERROR] --new file not found: ${NEW_FILE}" >&2; exit 2; }
[[ ! -f "${BASELINE_FILE}" ]] && { echo "[ERROR] --baseline file not found: ${BASELINE_FILE}" >&2; exit 2; }

command -v python3 >/dev/null 2>&1 || { echo "[ERROR] python3 is required" >&2; exit 2; }
command -v jq      >/dev/null 2>&1 || { echo "[ERROR] jq is required" >&2; exit 2; }

log() { echo "[$(date -u +%H:%M:%S)] $*" >&2; }

log "compare-release-status: new=${NEW_FILE} baseline=${BASELINE_FILE}"

# ── compute delta via python3 ─────────────────────────────────────────────────
DELTA_JSON="$(python3 - "${NEW_FILE}" "${BASELINE_FILE}" <<'PYEOF'
import json, sys

new_data      = json.load(open(sys.argv[1]))
baseline_data = json.load(open(sys.argv[2]))

new_ver  = new_data.get("target_version", "?")
base_ver = baseline_data.get("target_version", "?")

# Build lookup: id -> status (lowercased)
def idx(data):
    return {c["id"]: c["status"].lower() for c in data.get("checks", [])}

new_idx  = idx(new_data)
base_idx = idx(baseline_data)

all_ids = sorted(set(list(new_idx.keys()) + list(base_idx.keys())))

changes    = []
new_fails  = []
recovered  = []
degraded   = []
improved   = []
stable     = []
new_checks = []

for cid in all_ids:
    ns = new_idx.get(cid)
    bs = base_idx.get(cid)

    if bs is None:
        delta = "NEW_CHECK"
        new_checks.append(cid)
    elif ns == bs:
        delta = "STABLE"
        stable.append(cid)
    elif ns == "fail" and bs in ("pass", "skip"):
        delta = "NEW_FAIL"
        new_fails.append(cid)
    elif bs == "fail" and ns in ("pass", "skip"):
        delta = "RECOVERED"
        recovered.append(cid)
    elif bs == "pass" and ns == "skip":
        delta = "DEGRADED"
        degraded.append(cid)
    elif bs == "skip" and ns == "pass":
        delta = "IMPROVED"
        improved.append(cid)
    else:
        delta = "CHANGED"

    if delta != "STABLE":
        # look up name from whichever report has it
        name   = next((c["name"]   for c in new_data.get("checks",[])      if c["id"]==cid), \
                 next((c["name"]   for c in baseline_data.get("checks",[]) if c["id"]==cid), ""))
        group  = next((c.get("group","") for c in new_data.get("checks",[])      if c["id"]==cid), \
                 next((c.get("group","") for c in baseline_data.get("checks",[]) if c["id"]==cid), ""))
        details = next((c.get("details","") for c in new_data.get("checks",[]) if c["id"]==cid), "")
        changes.append({
            "id": cid, "group": group, "name": name,
            "baseline_status": bs or "absent",
            "new_status": ns or "absent",
            "delta": delta,
            "details": details,
        })

nt = new_data.get("totals", {})
bt = baseline_data.get("totals", {})

result = {
    "schema_version": "1",
    "new_version":      new_ver,
    "baseline_version": base_ver,
    "new_overall":      new_data.get("overall_status","?"),
    "baseline_overall": baseline_data.get("overall_status","?"),
    "regression_free":  len(new_fails) == 0,
    "new_fails":        new_fails,
    "recovered":        recovered,
    "degraded":         degraded,
    "improved":         improved,
    "new_checks":       new_checks,
    "stable_count":     len(stable),
    "changed_count":    len(changes),
    "totals_delta": {
        "pass":  nt.get("pass",0)  - bt.get("pass",0),
        "fail":  nt.get("fail",0)  - bt.get("fail",0),
        "skip":  nt.get("skip",0)  - bt.get("skip",0),
        "total": nt.get("total",0) - bt.get("total",0),
    },
    "new_totals":      nt,
    "baseline_totals": bt,
    "changes": changes,
}
print(json.dumps(result, indent=2))
PYEOF
)"

# ── extract key fields ────────────────────────────────────────────────────────
NEW_VER="$(         echo "${DELTA_JSON}" | jq -r '.new_version')"
BASE_VER="$(        echo "${DELTA_JSON}" | jq -r '.baseline_version')"
NEW_OVERALL="$(     echo "${DELTA_JSON}" | jq -r '.new_overall')"
BASE_OVERALL="$(    echo "${DELTA_JSON}" | jq -r '.baseline_overall')"
REGRESSION_FREE="$( echo "${DELTA_JSON}" | jq -r '.regression_free')"
NEW_FAIL_COUNT="$(  echo "${DELTA_JSON}" | jq '.new_fails | length')"
CHANGED_COUNT="$(   echo "${DELTA_JSON}" | jq '.changed_count')"

log "new=${NEW_VER} overall=${NEW_OVERALL}  baseline=${BASE_VER} overall=${BASE_OVERALL}"
log "regression_free=${REGRESSION_FREE}  new_fails=${NEW_FAIL_COUNT}  changed=${CHANGED_COUNT}"

# ── markdown output ───────────────────────────────────────────────────────────
emit_md() {
    echo "# Release Status Comparison: ${NEW_VER} vs ${BASE_VER}"
    echo ""
    echo "**Generated:** $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo ""
    echo "| Field | ${BASE_VER} (baseline) | ${NEW_VER} (new) | Delta |"
    echo "|---|---|---|---|"
    echo "${DELTA_JSON}" | python3 -c "
import json,sys
d=json.load(sys.stdin)
bt=d['baseline_totals']; nt=d['new_totals']; td=d['totals_delta']
def fmt(v): return ('+' if v>0 else '') + str(v) if v!=0 else '—'
print(f\"| overall | {d['baseline_overall']} | {d['new_overall']} | — |\")
print(f\"| pass | {bt.get('pass',0)} | {nt.get('pass',0)} | {fmt(td['pass'])} |\")
print(f\"| fail | {bt.get('fail',0)} | {nt.get('fail',0)} | {fmt(td['fail'])} |\")
print(f\"| skip | {bt.get('skip',0)} | {nt.get('skip',0)} | {fmt(td['skip'])} |\")
print(f\"| total | {bt.get('total',0)} | {nt.get('total',0)} | {fmt(td['total'])} |\")
"
    echo ""

    NCHANGES="$(echo "${DELTA_JSON}" | jq '.changes | length')"
    if [[ "${NCHANGES}" -eq 0 ]]; then
        echo "**No status changes detected.** All checks stable."
    else
        echo "## Status Changes"
        echo ""
        echo "| ID | Group | Name | Baseline | New | Delta |"
        echo "|---|---|---|---|---|---|"
        echo "${DELTA_JSON}" | python3 -c "
import json,sys
d=json.load(sys.stdin)
for c in d['changes']:
    print(f\"| {c['id']} | {c['group']} | {c['name']} | {c['baseline_status']} | {c['new_status']} | {c['delta']} |\")
"
    fi
    echo ""

    if [[ "${NEW_FAIL_COUNT}" -gt 0 ]]; then
        echo "## ⚠ Regressions Detected"
        echo ""
        echo "The following checks are **FAIL** in ${NEW_VER} but were not FAIL in ${BASE_VER}:"
        echo ""
        echo "${DELTA_JSON}" | jq -r '.new_fails[] | "- `\(.)`"'
        echo ""
        echo "> These failures indicate a regression. Backout criteria may be triggered."
        echo "> See \`docs/runbooks/v1.2.1-stabilization-watch.md\`."
    else
        echo "## Regression Status"
        echo ""
        echo "**REGRESSION-FREE** — no new FAILs in ${NEW_VER} relative to ${BASE_VER}."
    fi
}

# ── emit output ───────────────────────────────────────────────────────────────
case "${OUTPUT_FORMAT}" in
    md)   emit_md ;;
    json) echo "${DELTA_JSON}" ;;
    both)
        emit_md
        echo ""
        echo "## Raw Delta JSON"
        echo ""
        echo '```json'
        echo "${DELTA_JSON}"
        echo '```'
        ;;
    *) echo "[ERROR] Unknown --output format: ${OUTPUT_FORMAT} (use md|json|both)" >&2; exit 2 ;;
esac

# ── exit code: non-zero only on new FAILs ─────────────────────────────────────
[[ "${REGRESSION_FREE}" == "true" ]] && exit 0
exit 1
