#!/usr/bin/env bash
set -euo pipefail

# CodeRabbit Second-Pass Review (Fallback 2)
# Final fallback review using CodeRabbit for pre-commit quality gate.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
TIMEOUT_SEC="${CODERO_CODERABBIT_TIMEOUT_SEC:-300}"
CODERABBIT_MODEL="${CODERO_CODERABBIT_MODEL:-codero-2.0}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    exit 1
  fi
}

main() {
  require_cmd git
  require_cmd coderabbit

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  echo "--- CODERO CODERABBIT FALLBACK ($CODERABBIT_MODEL) ---"
  echo "Timeout: ${TIMEOUT_SEC}s"

  local result
  if ! result="$(timeout "$TIMEOUT_SEC" coderabbit review --type uncommitted --plain --no-color "$@" 2>&1)"; then
    exit_code=$?
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
