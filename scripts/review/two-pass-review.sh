#!/usr/bin/env bash
set -euo pipefail

# 6-Pass Pre-Commit Review Gate Orchestrator
# Implements codero standard with deterministic + AI fallback chain:
# 0. semgrep-zero-pass.sh (Deterministic blocker)
# 1. copilot-third-pass.sh (Primary Gate 1 - gpt-5-mini/gpt-4o-mini)
# 2. aider-first-pass.sh (Primary Gate 2 - MiniMax/OpenRouter)
# 3. gemini-second-pass.sh (Primary Gate 3 - Gemini OAuth)
# 4. pr-agent-second-pass.sh (Fallback 1)
# 5. coderabbit-second-pass.sh (Fallback 2)
#
# Rules:
# - Stop if 2+ checks succeed
# - Fail if any review finds issues (not just availability failures)
# - Rate-limited/timeout counts as "available but failed" - fallback to next
# - Only allow commit if 2+ checks pass

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
LOG_DIR="${CODERO_REVIEW_LOG_DIR:-$REPO_PATH/.codero/review-logs}"
TS="$(date +%Y%m%d-%H%M%S)"
MODEL_ALIAS="${CODERO_MODEL_ALIAS:-cacheflow_agent}"
MODE="${CODERO_MODE:-fast}"
MIN_SUCCESSFUL_AI_GATES="${CODERO_MIN_SUCCESSFUL_AI_GATES:-2}"

mkdir -p "$LOG_DIR"

declare -a PASSED=()
declare -a FAILED=()

log_status() {
  local pass_fail="$1"
  local gate="$2"
  local gate_num="$3"
  echo "[$TS] GATE $gate_num ($gate): $pass_fail" >> "$LOG_DIR/orchestrator-$TS.log"
}

run_gate() {
  local gate_name="$1"
  local gate_num="$2"
  local script="$SCRIPT_DIR/${gate_name}.sh"
  local log_file="$LOG_DIR/${gate_name}-${TS}.log"

  echo "=== GATE $gate_num: $gate_name ===" | tee -a "$LOG_DIR/orchestrator-$TS.log"

  if [ ! -x "$script" ]; then
    echo "Warning: Script not found or not executable: $script" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    log_status "skip" "$gate_name" "$gate_num"
    return 1
  fi

  local output exit_code
  set +e
  output="$(timeout "${CODERO_GATE_TIMEOUT:-180}" bash "$script" 2>&1)"
  exit_code=$?
  set -e

  if [ $exit_code -eq 124 ]; then
    echo "GATE $gate_num FAILED: Timeout after ${CODERO_GATE_TIMEOUT:-180}s" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    echo "$output" | tee "$log_file"
    log_status "failed_timeout" "$gate_name" "$gate_num"
    return 1
  fi

  if [ $exit_code -ne 0 ]; then
    echo "GATE $gate_num FAILED: Exit code $exit_code" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    echo "$output" | tee "$log_file"
    log_status "failed" "$gate_name" "$gate_num"
    return 1
  fi

  if echo "$output" | grep -qiE "(error|warning|fix|issue|problem|sgx|vulnerable|secret|credential)"; then
    echo "GATE $gate_num PASSED but found issues:" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    echo "$output" | tee "$log_file"
    log_status "passed_with_issues" "$gate_name" "$gate_num"
    return 1
  fi

  echo "GATE $gate_num PASSED: No issues found" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "$output" | tee "$log_file"
  log_status "passed" "$gate_name" "$gate_num"
  return 0
}

main() {
  echo "========================================"
  echo "5-PASS PRE-COMMIT REVIEW GATE"
  echo "Model Alias: $MODEL_ALIAS"
  echo "Mode: $MODE"
  echo "Repo: $REPO_PATH"
  echo "Log Dir: $LOG_DIR"
  echo "========================================"

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: Repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  local passed_count=0
  local total_attempts=0
  local semgrep_script="$SCRIPT_DIR/semgrep-zero-pass.sh"

  echo "Starting gate chain..." | tee -a "$LOG_DIR/orchestrator-$TS.log"

  # Gate 0 is deterministic and mandatory.
  if [ ! -x "$semgrep_script" ]; then
    echo "Error: mandatory Gate 0 missing or not executable: $semgrep_script" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    exit 1
  fi
  total_attempts=$((total_attempts + 1))
  if ! run_gate "semgrep-zero-pass" "$total_attempts"; then
    echo ""
    echo "✗ FAIL: Semgrep Gate 0 failed. Commit blocked."
    exit 1
  fi

  for gate in copilot-third-pass aider-first-pass gemini-second-pass pr-agent-second-pass coderabbit-second-pass; do
    if [ "$passed_count" -ge "$MIN_SUCCESSFUL_AI_GATES" ]; then
      echo "2+ gates passed, stopping gate chain." | tee -a "$LOG_DIR/orchestrator-$TS.log"
      break
    fi

    total_attempts=$((total_attempts + 1))
    echo "Attempting gate $total_attempts: $gate" | tee -a "$LOG_DIR/orchestrator-$TS.log"

    if run_gate "$gate" "$total_attempts"; then
      passed_count=$((passed_count + 1))
      PASSED+=("$gate")
    else
      FAILED+=("$gate")
    fi
  done

  echo "" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "========================================" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "REVIEW SUMMARY" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "========================================" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "Gates attempted: $total_attempts" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "AI gates passed: $passed_count (minimum required: $MIN_SUCCESSFUL_AI_GATES)" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "Passed gates: ${PASSED[*]}" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "Failed gates: ${FAILED[*]}" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "Model: $MODEL_ALIAS" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "Mode: $MODE" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "Timestamp: $TS" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "========================================" | tee -a "$LOG_DIR/orchestrator-$TS.log"

  if [ "$passed_count" -ge "$MIN_SUCCESSFUL_AI_GATES" ]; then
    local parallel_script="$SCRIPT_DIR/parallel-agent-pass.sh"
    if [ -x "$parallel_script" ]; then
      echo "Running parallel-agent pass..."
      timeout "${CODERO_GATE_TIMEOUT:-180}" env CODERO_REPO_PATH="$REPO_PATH" "$parallel_script"
    fi

    echo ""
    echo "✓ SUCCESS: Semgrep passed + AI gate quorum met ($passed_count/$total_attempts)"
    echo "Logs:"
    for log in "$LOG_DIR"/*.log; do
      echo "  $log"
    done
    exit 0
  else
    echo ""
    echo "✗ FAIL: AI gate quorum not met ($passed_count/$total_attempts)"
    echo "At least $MIN_SUCCESSFUL_AI_GATES AI reviews must pass for commit." | tee -a "$LOG_DIR/orchestrator-$TS.log"
    echo "Logs:"
    for log in "$LOG_DIR"/*.log; do
      echo "  $log"
    done
    exit 1
  fi
}

main "$@"
