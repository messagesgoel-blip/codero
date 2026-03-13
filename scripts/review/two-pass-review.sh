#!/usr/bin/env bash
set -euo pipefail

# Two-pass review gate:
# 1) LiteLLM local first pass
# 2) CodeRabbit local second pass (fallback to PR-Agent if CodeRabbit fails)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
LOG_DIR="${CODERO_REVIEW_LOG_DIR:-$REPO_PATH/.codero/review-logs}"
TS="$(date +%Y%m%d-%H%M%S)"
mkdir -p "$LOG_DIR"

FIRST_LOG="$LOG_DIR/first-pass-$TS.log"
SECOND_LOG="$LOG_DIR/second-pass-$TS.log"

echo "Running two-pass review for repo: $REPO_PATH"

CODERO_REPO_PATH="$REPO_PATH" "$SCRIPT_DIR/local-first-pass.sh" | tee "$FIRST_LOG"
set +e
CODERO_REPO_PATH="$REPO_PATH" "$SCRIPT_DIR/coderabbit-second-pass.sh" "$@" | tee "$SECOND_LOG"
coderabbit_status=$?
set -e

if [ "$coderabbit_status" -ne 0 ]; then
  echo "CodeRabbit second pass failed (exit $coderabbit_status). Trying PR-Agent fallback..." | tee -a "$SECOND_LOG"
  CODERO_REPO_PATH="$REPO_PATH" "$SCRIPT_DIR/pr-agent-second-pass.sh" "$@" | tee -a "$SECOND_LOG"
fi

echo "Two-pass review completed."
echo "Logs:"
echo "  $FIRST_LOG"
echo "  $SECOND_LOG"
