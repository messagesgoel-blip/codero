#!/usr/bin/env bash
# KEEP_LOCAL: required for shipped runtime — release record schema validation
#
# validate-release-record.sh — Validates that a release record YAML contains
# all required fields with non-empty values.
#
# Usage:
#   ./scripts/automation/validate-release-record.sh <record.yaml>
#
# Exit codes:
#   0  All required fields present and non-empty.
#   1  One or more required fields missing or empty.
#   2  Usage error (wrong arguments).
#
# SHA cross-check note:
#   This script validates field presence only. It does NOT cross-check
#   artifact_sha256 against the binary on disk because the artifact path
#   (/srv/storage/...) is not available in GitHub Actions runners.
#   To verify the SHA locally:
#     sha256sum /srv/storage/shared/tools/releases/codero-vX.Y.Z/codero
#   and compare against the artifact_sha256 field in the record.

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <release-record.yaml>" >&2
    exit 2
fi

RECORD="$1"

if [[ ! -f "${RECORD}" ]]; then
    echo "ERROR: release record not found: ${RECORD}" >&2
    exit 1
fi

# Required fields — each must appear in the YAML as "key: <non-empty-value>"
REQUIRED_FIELDS=(
    version
    promoted_commit
    tag
    date
    artifact_path
    artifact_sha256
    go_toolchain
    verification_status
    rollback_version
    rollback_artifact
)

failed=0
for field in "${REQUIRED_FIELDS[@]}"; do
    # Match "field: <value>" — value must not be empty after the colon/space.
    # Handles both quoted ("value") and unquoted (value) YAML scalars.
    if ! grep -qE "^${field}:[[:space:]]+\".+\"|^${field}:[[:space:]]+[^\"[:space:]]" "${RECORD}"; then
        echo "ERROR: required field missing or empty: ${field}" >&2
        failed=1
    fi
done

if [[ ${failed} -eq 1 ]]; then
    echo "FAIL: release record ${RECORD} is missing required fields" >&2
    exit 1
fi

echo "PASS: release record ${RECORD} has all required fields"
exit 0
