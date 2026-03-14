#!/usr/bin/env bash
set -euo pipefail

# Mandatory-two review gate:
# 1) Aider first pass (always)
# 2) Gemini second pass (always)
# Two successful checks are required to pass.
# Fallback chain only applies when a primary check is rate-limited:
# 3) PR-Agent third pass
# 4) CodeRabbit fourth pass (only if PR-Agent fails)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
LOG_DIR="${CODERO_REVIEW_LOG_DIR:-$REPO_PATH/.codero/review-logs}"
TS="$(date +%Y%m%d-%H%M%S)"
mkdir -p "$LOG_DIR"

FIRST_LOG="$LOG_DIR/first-pass-$TS.log"
SECOND_LOG="$LOG_DIR/second-pass-$TS.log"
THIRD_LOG="$LOG_DIR/third-pass-$TS.log"
FOURTH_LOG="$LOG_DIR/fourth-pass-$TS.log"
PASS_TIMEOUT_SEC="${CODERO_REVIEW_PASS_TIMEOUT_SEC:-300}"
REQUIRED_PASSES=2

echo "Running mandatory-two review gate for repo: $REPO_PATH"

run_pass() {
  local name="$1"
  local log="$2"
  shift 2
  set +e
  timeout "$PASS_TIMEOUT_SEC" env CODERO_REPO_PATH="$REPO_PATH" "$@" | tee "$log"
  local status="${PIPESTATUS[0]}"
  set -e
  if [ "$status" -eq 124 ]; then
    echo "FAIL: $name (timeout ${PASS_TIMEOUT_SEC}s)"
    return 124
  fi
  if [ "$status" -eq 0 ]; then
    echo "PASS: $name"
  else
    echo "FAIL: $name (exit $status)"
  fi
  return "$status"
}

is_rate_limited_log() {
  local log="$1"
  # Rate-limit and provider throttling signals across aider/gemini/pr-agent/coderabbit
  if rg -qi 'RateLimit|rate limit|429|RESOURCE_EXHAUSTED|Too Many Requests|quota exceeded|insufficient_quota' "$log"; then
    return 0
  fi
  return 1
}

passes=0
failures=0
aider_status=1
gemini_status=1
aider_rl=0
gemini_rl=0

if run_pass "Aider first pass" "$FIRST_LOG" "$SCRIPT_DIR/aider-first-pass.sh"; then
  passes=$((passes + 1))
  aider_status=0
else
  aider_status=$?
  failures=$((failures + 1))
  if is_rate_limited_log "$FIRST_LOG"; then
    aider_rl=1
  fi
fi

if run_pass "Gemini second pass" "$SECOND_LOG" "$SCRIPT_DIR/gemini-second-pass.sh"; then
  passes=$((passes + 1))
  gemini_status=0
else
  gemini_status=$?
  failures=$((failures + 1))
  if is_rate_limited_log "$SECOND_LOG"; then
    gemini_rl=1
  fi
fi

if [ "$passes" -ge "$REQUIRED_PASSES" ]; then
  echo "Review gate passed in primary stage."
  echo "Summary: passes=$passes failures=$failures"
  echo "Logs:"
  echo "  $FIRST_LOG"
  echo "  $SECOND_LOG"
  exit 0
fi

need_fallback=0
if [ "$aider_rl" -eq 1 ] || [ "$gemini_rl" -eq 1 ] || [ "$aider_status" -eq 124 ] || [ "$gemini_status" -eq 124 ]; then
  need_fallback=1
fi

if [ "$need_fallback" -eq 0 ]; then
  echo "Review gate failed: fewer than $REQUIRED_PASSES checks passed and no primary rate-limit condition to trigger fallback."
  echo "Summary: passes=$passes failures=$failures aider_status=$aider_status gemini_status=$gemini_status"
  echo "Logs:"
  echo "  $FIRST_LOG"
  echo "  $SECOND_LOG"
  exit 1
fi

echo "Primary stage had rate limiting; running fallback chain (PR-Agent -> CodeRabbit)..."

if run_pass "PR-Agent third pass" "$THIRD_LOG" "$SCRIPT_DIR/pr-agent-second-pass.sh" "$@"; then
  passes=$((passes + 1))
else
  failures=$((failures + 1))
fi

if [ "$passes" -lt "$REQUIRED_PASSES" ]; then
  if run_pass "CodeRabbit fourth pass" "$FOURTH_LOG" "$SCRIPT_DIR/coderabbit-second-pass.sh" "$@"; then
    passes=$((passes + 1))
  else
    failures=$((failures + 1))
  fi
fi

echo "Review gate summary: passes=$passes failures=$failures required=$REQUIRED_PASSES aider_rl=$aider_rl gemini_rl=$gemini_rl"

if [ "$passes" -lt "$REQUIRED_PASSES" ]; then
  echo "Review gate failed: mandatory two checks not satisfied."
  echo "Logs:"
  echo "  $FIRST_LOG"
  echo "  $SECOND_LOG"
  if [ -f "$THIRD_LOG" ]; then echo "  $THIRD_LOG"; fi
  if [ -f "$FOURTH_LOG" ]; then echo "  $FOURTH_LOG"; fi
  exit 1
fi

echo "Review gate passed."
echo "Logs:"
echo "  $FIRST_LOG"
echo "  $SECOND_LOG"
if [ -f "$THIRD_LOG" ]; then echo "  $THIRD_LOG"; fi
if [ -f "$FOURTH_LOG" ]; then echo "  $FOURTH_LOG"; fi
