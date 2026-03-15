#!/usr/bin/env bash
set -euo pipefail


# Detect timeout command (support GNU coreutils on macOS)
TIMEOUT_CMD=""
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_CMD="timeout"
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_CMD="gtimeout"
else
  echo "Error: timeout command not found. Install coreutils." >&2
  exit 1
fi
# 6-Pass Pre-Commit Review Gate Orchestrator
# Default order:
# 1. copilot-third-pass.sh (Primary AI gate)
# 2. semgrep-zero-pass.sh (Deterministic blocker; mandatory)
# 3. aider-first-pass.sh
# 4. gemini-second-pass.sh
# 5. pr-agent-second-pass.sh
# 6. coderabbit-second-pass.sh
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
AUTH_HOME="${CODERO_AUTH_HOME:-}"
GATE_ORDER="${CODERO_GATE_ORDER:-copilot-first}"

mkdir -p "$LOG_DIR"

declare -a PASSED=()
declare -a FAILED=()

log_status() {
  local pass_fail="$1"
  local gate="$2"
  local gate_num="$3"
  echo "[$TS] GATE $gate_num ($gate): $pass_fail" >> "$LOG_DIR/orchestrator-$TS.log"
}

find_env_file() {
  if [ -n "${CODERO_ENV_FILE:-}" ] && [ -f "${CODERO_ENV_FILE}" ]; then
    echo "${CODERO_ENV_FILE}"
    return 0
  fi

  if [ -f "$REPO_PATH/.env" ]; then
    echo "$REPO_PATH/.env"
    return 0
  fi

  local common_dir repo_root_env
  common_dir="$(git -C "$REPO_PATH" rev-parse --git-common-dir 2>/dev/null || true)"
  if [ -n "$common_dir" ]; then
    repo_root_env="$(cd "$REPO_PATH" && cd "$common_dir/.." 2>/dev/null && pwd)/.env"
    if [ -f "$repo_root_env" ]; then
      echo "$repo_root_env"
      return 0
    fi
  fi

  return 1
}

trim_whitespace() {
  local value="${1-}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

load_env() {
  local env_file line key value
  if env_file="$(find_env_file)"; then
    echo "Loading environment from: $env_file" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    while IFS= read -r line || [ -n "$line" ]; do
      line="$(trim_whitespace "$line")"
      [ -z "$line" ] && continue
      case "$line" in
        \#*) continue ;;
      esac

      if [[ ! "$line" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then
        echo "Skipping unsupported env line: $line" >> "$LOG_DIR/orchestrator-$TS.log"
        continue
      fi

      key="${line%%=*}"
      value="${line#*=}"
      value="$(trim_whitespace "$value")"

      if [[ "$value" == *'$('* || "$value" == *'`'* ]]; then
        echo "Skipping unsafe env value for key: $key" >> "$LOG_DIR/orchestrator-$TS.log"
        continue
      fi

      if [[ "$value" == \"*\" ]] && [ "${#value}" -ge 2 ]; then
        value="${value:1:${#value}-2}"
        value="${value//\\n/$'\n'}"
        value="${value//\\t/$'\t'}"
        value="${value//\\\"/\"}"
        value="${value//\\\\/\\}"
      elif [[ "$value" == \'*\' ]] && [ "${#value}" -ge 2 ]; then
        value="${value:1:${#value}-2}"
      fi

      export "$key=$value"
      echo "Loaded env key: $key" >> "$LOG_DIR/orchestrator-$TS.log"
    done < "$env_file"
  else
    echo "No .env file found; gates will rely on existing environment variables." | tee -a "$LOG_DIR/orchestrator-$TS.log"
  fi
}

setup_auth_home() {
  # Prefer explicit CODERO_AUTH_HOME if provided and valid; otherwise fall back to existing HOME.
  if [ -n "$AUTH_HOME" ] && [ -d "$AUTH_HOME" ]; then
    export HOME="$AUTH_HOME"
    echo "Using auth home: $HOME" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    return 0
  fi

  if [ -z "${HOME:-}" ]; then
    HOME="$(getent passwd "$(id -u)" | cut -d: -f6 2>/dev/null || echo "$REPO_PATH")"
    export HOME
    echo "Using default auth home: $HOME" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  fi
}



run_gate() {
  local gate_name="$1"
  local gate_num="$2"
  local script="$SCRIPT_DIR/${gate_name}.sh"
  local log_file="$LOG_DIR/${gate_name}-$TS.log"
  local exit_code output findings

  if [ ! -x "$script" ]; then
    echo "✗ $gate_name script not found or not executable: $script" >&2
    log_status "failed" "$gate_name" "$gate_num"
    return 1
  fi

  exit_code=0
  output=$("$TIMEOUT_CMD" "${CODERO_GATE_TIMEOUT:-180}" bash "$script" 2>&1) || exit_code=$?

  if [ $exit_code -eq 0 ]; then
    # Even on zero exit, check output for findings
    if echo "$output" | grep -qE '[0-9]+ findings'; then
      findings=$(echo "$output" | grep -oE '[0-9]+ findings' | head -1 | awk '{print $1}')
      if [ -n "$findings" ] && [ "$findings" -gt 0 ]; then
        echo "GATE $gate_num FAILED with issues ($findings findings): see log" | tee -a "$LOG_DIR/orchestrator-$TS.log"
        echo "$output" | tee "$log_file"
        log_status "failed_with_issues" "$gate_name" "$gate_num"
        return 1
      fi
    fi
    echo "GATE $gate_num PASSED: No issues found" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    echo "$output" | tee "$log_file"
    log_status "passed_clean" "$gate_name" "$gate_num"
    return 0
  fi

  if [ $exit_code -eq 124 ]; then
    echo "GATE $gate_num TIMEOUT" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    echo "$output" | tee "$log_file"
    log_status "failed_timeout" "$gate_name" "$gate_num"
    return 1
  fi

  # Any non-zero exit is a failure, even if findings parse to zero.
  if echo "$output" | grep -qE '[0-9]+ findings'; then
    findings=$(echo "$output" | grep -oE '[0-9]+ findings' | head -1 | awk '{print $1}')
    if [ -n "$findings" ] && [ "$findings" -gt 0 ]; then
      echo "GATE $gate_num FAILED with issues ($findings findings): see log" | tee -a "$LOG_DIR/orchestrator-$TS.log"
      echo "$output" | tee "$log_file"
      log_status "failed_with_issues" "$gate_name" "$gate_num"
    fi
  fi

  echo "GATE $gate_num FAILED: non-zero exit code ($exit_code)" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "$output" | tee "$log_file"
  log_status "failed_nonzero_exit" "$gate_name" "$gate_num"
  return 1
}



run_copilot_then_semgrep() {
  PASSED_COUNT=0
  TOTAL_ATTEMPTS=0
  local semgrep_script="$SCRIPT_DIR/semgrep-zero-pass.sh"

  # Gate 1: Copilot (AI)
  TOTAL_ATTEMPTS=$((TOTAL_ATTEMPTS + 1))
  echo "Attempting gate $TOTAL_ATTEMPTS: copilot-third-pass" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  if run_gate "copilot-third-pass" "$TOTAL_ATTEMPTS"; then
    PASSED_COUNT=$((PASSED_COUNT + 1))
    PASSED+=("copilot-third-pass")
  else
    FAILED+=("copilot-third-pass")
  fi

  # Gate 2: Semgrep (mandatory deterministic blocker)
  if [ ! -x "$semgrep_script" ]; then
    echo "Error: mandatory Semgrep gate missing or not executable: $semgrep_script" | tee -a "$LOG_DIR/orchestrator-$TS.log"
    exit 1
  fi
  TOTAL_ATTEMPTS=$((TOTAL_ATTEMPTS + 1))
  if ! run_gate "semgrep-zero-pass" "$TOTAL_ATTEMPTS"; then
    echo ""
    echo "✗ FAIL: Semgrep Gate failed. Commit blocked."
    exit 1
  fi

  for gate in aider-first-pass gemini-second-pass pr-agent-second-pass coderabbit-second-pass; do
    if [ "$PASSED_COUNT" -ge "$MIN_SUCCESSFUL_AI_GATES" ]; then
      echo "AI gate quorum met, stopping gate chain." | tee -a "$LOG_DIR/orchestrator-$TS.log"
      break
    fi

    TOTAL_ATTEMPTS=$((TOTAL_ATTEMPTS + 1))
    echo "Attempting gate $TOTAL_ATTEMPTS: $gate" | tee -a "$LOG_DIR/orchestrator-$TS.log"

    if run_gate "$gate" "$TOTAL_ATTEMPTS"; then
      PASSED_COUNT=$((PASSED_COUNT + 1))
      PASSED+=("$gate")
    else
      FAILED+=("$gate")
    fi
  done

  return 0
}

main() {
  # Load environment first so .env values influence configuration
  load_env

  # Re-derive configuration from environment (now that .env is loaded)
  MODEL_ALIAS="${CODERO_MODEL_ALIAS:-cacheflow_agent}"
  MODE="${CODERO_MODE:-fast}"
  MIN_SUCCESSFUL_AI_GATES="${CODERO_MIN_SUCCESSFUL_AI_GATES:-2}"
  AUTH_HOME="${CODERO_AUTH_HOME:-}"
  GATE_ORDER="${CODERO_GATE_ORDER:-copilot-first}"

  echo "========================================"
  echo "6-PASS PRE-COMMIT REVIEW GATE"
  echo "Model Alias: $MODEL_ALIAS"
  echo "Mode: $MODE"
  echo "Repo: $REPO_PATH"
  echo "Log Dir: $LOG_DIR"
  echo "========================================"

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: Repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  setup_auth_home

  echo "Starting gate chain..." | tee -a "$LOG_DIR/orchestrator-$TS.log"
  PASSED_COUNT=0
  TOTAL_ATTEMPTS=0
  case "$GATE_ORDER" in
    copilot-first)
      run_copilot_then_semgrep
      ;;
    *)
      echo "Error: unsupported CODERO_GATE_ORDER='$GATE_ORDER' (only 'copilot-first' is supported)" | tee -a "$LOG_DIR/orchestrator-$TS.log"
      exit 1
      ;;
  esac

  local passed_count="$PASSED_COUNT"
  local total_attempts="$TOTAL_ATTEMPTS"

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
  echo "Gate order: $GATE_ORDER" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "Timestamp: $TS" | tee -a "$LOG_DIR/orchestrator-$TS.log"
  echo "========================================" | tee -a "$LOG_DIR/orchestrator-$TS.log"

  if [ "$passed_count" -ge "$MIN_SUCCESSFUL_AI_GATES" ]; then
    local parallel_script="$SCRIPT_DIR/parallel-agent-pass.sh"
    if [ -x "$parallel_script" ]; then
      echo "Running parallel-agent pass..."
      local parallel_exit=0
      "$TIMEOUT_CMD" "${CODERO_GATE_TIMEOUT:-180}" env CODERO_REPO_PATH="$REPO_PATH" "$parallel_script" || parallel_exit=$?
      if [ "$parallel_exit" -ne 0 ]; then
        echo "⚠ Parallel-agent pass completed with exit code $parallel_exit" >&2 | tee -a "$LOG_DIR/orchestrator-$TS.log"
      fi
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
