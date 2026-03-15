#!/usr/bin/env bash
set -euo pipefail

# CodeRabbit Second-Pass Review (Fallback 2)
# Final fallback review using CodeRabbit for pre-commit quality gate.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
TIMEOUT_SEC="${CODERO_CODERABBIT_TIMEOUT_SEC:-300}"
CODERABBIT_MODEL="${CODERO_CODERABBIT_MODEL:-codero-2.0}"

# Portable timeout command (macOS uses gtimeout)
TIMEOUT_CMD=""
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_CMD="timeout"
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_CMD="gtimeout"
else
  echo "Error: timeout utility not found (install coreutils)" >&2
  exit 1
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    return 1
  fi
  return 0
}

main() {
  if ! require_cmd git; then
    exit 1
  fi
  if ! require_cmd coderabbit; then
    exit 1
  fi

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  echo "--- CODERO CODERABBIT FALLBACK ($CODERABBIT_MODEL) ---"
  echo "Timeout: ${TIMEOUT_SEC}s"

  local result exit_code=0
  result=$("$TIMEOUT_CMD" "$TIMEOUT_SEC" coderabbit review --type uncommitted --plain --no-color "$@" 2>&1) || exit_code=$?
  if [ $exit_code -ne 0 ]; then
    if [ $exit_code -eq 124 ]; then
      echo "Error: CodeRabbit review timed out after ${TIMEOUT_SEC}s"
      exit 1
    fi
    echo "$result" >&2
    exit 1
  fi

  echo "$result"
  echo "--- CODERO CODERABBIT FALLBACK END ---"
}

main "$@"
