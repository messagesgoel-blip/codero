#!/usr/bin/env bash
set -euo pipefail

# Generic CodeRabbit second-pass review for uncommitted changes.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"

if [ ! -d "$REPO_PATH" ]; then
  echo "Error: repo path does not exist: $REPO_PATH" >&2
  exit 1
fi

if ! command -v coderabbit >/dev/null 2>&1; then
  echo "Error: coderabbit CLI not found in PATH." >&2
  exit 1
fi

echo "--- CODERO SECOND PASS (CodeRabbit) ---"
(
  cd "$REPO_PATH"
  coderabbit review --type uncommitted --plain --no-color "$@" 2>&1
)
echo "--- CODERO SECOND PASS END ---"
