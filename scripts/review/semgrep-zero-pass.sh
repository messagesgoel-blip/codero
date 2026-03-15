#!/usr/bin/env bash
set -euo pipefail

# Semgrep deterministic pre-commit gate (Gate 0).
# Fails commit on findings or execution errors.
# Scans staged content (not working tree) for accuracy.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
TIMEOUT_SEC="${CODERO_SEMGREP_TIMEOUT_SEC:-180}"
SEMGREP_CONFIG="${CODERO_SEMGREP_CONFIG:-p/default}"

# Portable timeout command (macOS uses gtimeout)
TIMEOUT_CMD=""
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_CMD="timeout"
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_CMD="gtimeout"
else
  echo "Error: timeout command not found. Install coreutils (brew install coreutils on macOS)." >&2
  exit 1
fi

if ! command -v semgrep >/dev/null 2>&1; then
  echo "Error: semgrep is not installed. Install with: pip install semgrep" >&2
  exit 1
fi

if [ ! -d "$REPO_PATH" ]; then
  echo "Error: repo path does not exist: $REPO_PATH" >&2
  exit 1
fi

cd "$REPO_PATH"

mapfile -t STAGED_FILES < <(git diff --cached --name-only --diff-filter=ACMR)
if [ "${#STAGED_FILES[@]}" -eq 0 ]; then
  echo "No staged files. Semgrep gate skipped."
  exit 0
fi

# Create temp directory for staged content
TMPDIR="${TMPDIR:-/tmp}"
SCAN_DIR="$(mktemp -d "${TMPDIR}/semgrep-staged.XXXXXX")"
trap 'rm -rf "$SCAN_DIR"' EXIT

TARGETS=()
for f in "${STAGED_FILES[@]}"; do
  # Extract staged content using git show :<path>
  STAGED_CONTENT="$(git -C "$REPO_PATH" show ":$f" 2>/dev/null || true)"
  if [ -n "$STAGED_CONTENT" ]; then
    # Create same directory structure in temp
    TARGET_PATH="$SCAN_DIR/$f"
    mkdir -p "$(dirname "$TARGET_PATH")"
    printf '%s' "$STAGED_CONTENT" > "$TARGET_PATH"
    TARGETS+=("$TARGET_PATH")
  fi
done

if [ "${#TARGETS[@]}" -eq 0 ]; then
  echo "No staged regular files to scan. Semgrep gate skipped."
  exit 0
fi

EXTRA_ARGS=()
if [ -n "${CODERO_SEMGREP_EXTRA_ARGS:-}" ]; then
  # shellcheck disable=SC2206
  EXTRA_ARGS=( ${CODERO_SEMGREP_EXTRA_ARGS} )
fi

echo "--- CODERO SEMGREP PASS (Gate 0) ---"
echo "Config: $SEMGREP_CONFIG"
echo "Targets: ${#TARGETS[@]} staged file(s)"
echo "Timeout: ${TIMEOUT_SEC}s"

set +e
"$TIMEOUT_CMD" "$TIMEOUT_SEC" semgrep scan \
  --config "$SEMGREP_CONFIG" \
  --error \
  --metrics=off \
  "${EXTRA_ARGS[@]}" \
  "${TARGETS[@]}"
rc=$?
set -e

if [ "$rc" -eq 0 ]; then
  echo "Semgrep passed: no findings."
  exit 0
fi

if [ "$rc" -eq 124 ]; then
  echo "Semgrep timed out after ${TIMEOUT_SEC}s." >&2
  exit 1
fi

if [ "$rc" -eq 1 ]; then
  echo "Semgrep found issues. Commit blocked." >&2
  exit 1
fi

echo "Semgrep failed with exit code $rc. Commit blocked." >&2
exit 1