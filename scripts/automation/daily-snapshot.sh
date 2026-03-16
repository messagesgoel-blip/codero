#!/usr/bin/env bash
# KEEP_LOCAL: required for shipped runtime — daily proving snapshot automation wrapper
# Usage:
#   ./daily-snapshot.sh [--snapshot-dir /path] [--retain-days 45] [--skip-preflight]
#
# Wraps `codero daily-snapshot` with timestamped logging and explicit failure exit codes.
# Intended to be called from a systemd timer or cron job (see sibling templates).
set -euo pipefail

CODERO_BIN="${CODERO_BIN:-$(command -v codero 2>/dev/null || echo "")}"
SNAPSHOT_DIR="${SNAPSHOT_DIR:-${HOME}/.codero/snapshots}"
RETAIN_DAYS="${RETAIN_DAYS:-45}"
LOG_DIR="${LOG_DIR:-${HOME}/.codero/logs}"
EXTRA_ARGS=()

# Forward all script arguments to the codero command
for arg in "$@"; do
    EXTRA_ARGS+=("$arg")
done

if [[ -z "${CODERO_BIN}" ]]; then
    echo "[ERROR] codero binary not found. Set CODERO_BIN or add codero to PATH." >&2
    exit 3
fi

mkdir -p "${LOG_DIR}"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
LOG_FILE="${LOG_DIR}/daily-snapshot-$(date -u +%Y%m%d).log"

log() {
    echo "[${TIMESTAMP}] $*" | tee -a "${LOG_FILE}"
}

log "Starting daily proving snapshot"
log "Binary: ${CODERO_BIN}"
log "Snapshot dir: ${SNAPSHOT_DIR}"
log "Retain days: ${RETAIN_DAYS}"

if ! "${CODERO_BIN}" daily-snapshot \
        --snapshot-dir "${SNAPSHOT_DIR}" \
        --retain-days "${RETAIN_DAYS}" \
        "${EXTRA_ARGS[@]}"; then
    EXIT_CODE=$?
    log "FAIL: daily-snapshot exited with code ${EXIT_CODE}"
    exit "${EXIT_CODE}"
fi

log "OK: daily-snapshot completed successfully"

# Verify today's snapshot is present
if ! "${CODERO_BIN}" daily-snapshot \
        --snapshot-dir "${SNAPSHOT_DIR}" \
        --verify-only; then
    log "FAIL: post-run verify-only check failed"
    exit 4
fi

log "OK: verification passed"
