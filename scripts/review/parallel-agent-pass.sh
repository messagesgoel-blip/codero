#!/usr/bin/env bash
set -euo pipefail

# Extra quality gate for parallel-agent sessions.
# Flow: Copilot CLI first, LiteLLM fallback. Script no-ops when parallel work is not detected.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
MAP_DIR="${CACHEFLOW_AGENT_TTY_MAP_DIR:-/tmp/cacheflow_agent_tty_map}"
ACTIVE_MMIN="${PARALLEL_GATE_ACTIVE_MMIN:-240}"
PARALLEL_THRESHOLD="${PARALLEL_GATE_THRESHOLD:-2}"
DIFF_LIMIT_BYTES="${PARALLEL_GATE_DIFF_LIMIT_BYTES:-120000}"

COPILOT_BIN="${PARALLEL_GATE_COPILOT_BIN:-copilot}"
# gpt-4.1 is the closest stable option currently available in Copilot CLI.
COPILOT_MODEL="${PARALLEL_GATE_COPILOT_MODEL:-gpt-4.1}"
COPILOT_TIMEOUT_SEC="${PARALLEL_GATE_COPILOT_TIMEOUT_SEC:-90}"

LITELLM_URL="${PARALLEL_GATE_LITELLM_URL:-${CODERO_LITELLM_URL:-${LITELLM_PROXY_URL:-http://localhost:4000/v1/chat/completions}}}"
LITELLM_MODEL="${PARALLEL_GATE_LITELLM_MODEL:-gpt-4o-mini}"
LITELLM_TIMEOUT_SEC="${PARALLEL_GATE_LITELLM_TIMEOUT_SEC:-90}"

log() {
  printf '%s\n' "$*" >&2
}

count_parallel_agents() {
  local total=0

  if [ -n "${PARALLEL_AGENT_COUNT:-}" ] && [ "${PARALLEL_AGENT_COUNT}" -ge 0 ] 2>/dev/null; then
    total="${PARALLEL_AGENT_COUNT}"
  fi

  if [ -d "$MAP_DIR" ]; then
    local map_count
    map_count="$(find "$MAP_DIR" -maxdepth 1 -type f -mmin "-${ACTIVE_MMIN}" 2>/dev/null | wc -l | tr -d ' ')"
    if [ "${map_count:-0}" -gt "$total" ] 2>/dev/null; then
      total="$map_count"
    fi
  fi

  if [ -n "${CACHEFLOW_PARALLEL_AGENT_COUNT:-}" ] && [ "${CACHEFLOW_PARALLEL_AGENT_COUNT}" -gt "$total" ] 2>/dev/null; then
    total="${CACHEFLOW_PARALLEL_AGENT_COUNT}"
  fi

  printf '%s' "$total"
}

build_diff() {
  local staged tracked untracked file file_diff combined
  staged="$(git -C "$REPO_PATH" diff --cached || true)"

  if [ -n "$staged" ]; then
    combined="$staged"
  else
    tracked="$(git -C "$REPO_PATH" diff HEAD || true)"
    untracked=""
    while IFS= read -r -d '' file; do
      file_diff="$(git -C "$REPO_PATH" diff --no-index -- /dev/null "$file" || [ $? -eq 1 ])"
      untracked="${untracked}${file_diff}"
    done < <(git -C "$REPO_PATH" ls-files --others --exclude-standard -z)
    combined="${tracked}${untracked}"
  fi

  printf '%s' "$combined" | head -c "$DIFF_LIMIT_BYTES"
}

load_litellm_key() {
  if [ -n "${PARALLEL_GATE_LITELLM_KEY:-}" ]; then
    printf '%s' "$PARALLEL_GATE_LITELLM_KEY"
    return 0
  fi
  if [ -n "${CODERO_LITELLM_MASTER_KEY:-}" ]; then
    printf '%s' "$CODERO_LITELLM_MASTER_KEY"
    return 0
  fi
  if [ -n "${LITELLM_MASTER_KEY:-}" ]; then
    printf '%s' "$LITELLM_MASTER_KEY"
    return 0
  fi
  if [ -n "${LITELLM_API_KEY:-}" ]; then
    printf '%s' "$LITELLM_API_KEY"
    return 0
  fi
  if [ -n "${OPENAI_API_KEY:-}" ]; then
    printf '%s' "$OPENAI_API_KEY"
    return 0
  fi

  if [ -f "$REPO_PATH/.env" ]; then
    local raw
    raw="$(grep -E '^(LITELLM_MASTER_KEY|LITELLM_API_KEY|OPENAI_API_KEY)=' "$REPO_PATH/.env" | head -n 1 | cut -d'=' -f2- || true)"
    raw="${raw%\"}"
    raw="${raw#\"}"
    raw="${raw%\'}"
    raw="${raw#\'}"
    if [ -n "$raw" ]; then
      printf '%s' "$raw"
      return 0
    fi
  fi

  return 1
}

build_prompt() {
  local diff="$1"
  cat <<PROMPT
You are a strict parallel-agent pre-commit reviewer.

Return exactly:
1) First line: PARALLEL_GATE:PASS or PARALLEL_GATE:FAIL
2) Then max 5 bullets with concrete blockers (or "- none").

Fail when you find:
- merge conflicts, broken logic, obvious regressions, security leaks, or missing critical tests.
- unresolved TODO/FIXME that blocks safe merge.

Diff to review:
$diff
PROMPT
}

run_with_copilot() {
  local prompt="$1"
  command -v "$COPILOT_BIN" >/dev/null 2>&1 || return 127

  set +e
  local output status
  output="$(
    cd "$REPO_PATH"
    printf '%s' "$prompt" | timeout "$COPILOT_TIMEOUT_SEC" "$COPILOT_BIN" -p "" -s --output-format text --allow-all-tools --allow-all-paths --no-ask-user --model "$COPILOT_MODEL" 2>&1
  )"
  status=$?
  set -e

  if [ "$status" -ne 0 ]; then
    printf '%s\n' "$output" >&2
    return "$status"
  fi

  printf '%s\n' "$output"
  return 0
}

run_with_litellm() {
  command -v curl >/dev/null 2>&1 || return 127
  command -v jq >/dev/null 2>&1 || return 127

  local prompt="$1" key response result
  key="$(load_litellm_key)" || return 127

  response="$(curl -sS -X POST "$LITELLM_URL" \
    --max-time "$LITELLM_TIMEOUT_SEC" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $key" \
    -d "{\"model\":\"$LITELLM_MODEL\",\"messages\":[{\"role\":\"system\",\"content\":\"You are a strict code reviewer.\"},{\"role\":\"user\",\"content\":$(printf '%s' "$prompt" | jq -Rs .)}],\"temperature\":0}")"

  result="$(printf '%s' "$response" | jq -r '.choices[0].message.content // empty')"
  if [ -z "$result" ] || [ "$result" = "null" ]; then
    printf '%s\n' "$response" >&2
    return 1
  fi

  printf '%s\n' "$result"
}

main() {
  local parallel_count diff prompt out
  parallel_count="$(count_parallel_agents)"

  if [ "${parallel_count:-0}" -lt "$PARALLEL_THRESHOLD" ]; then
    log "parallel-agent pass skipped: active_agents=$parallel_count threshold=$PARALLEL_THRESHOLD"
    exit 0
  fi

  diff="$(build_diff)"
  if [ -z "$diff" ]; then
    log "parallel-agent pass skipped: no diff"
    exit 0
  fi

  prompt="$(build_prompt "$diff")"
  log "parallel-agent pass active: active_agents=$parallel_count model_pref=$COPILOT_MODEL fallback=$LITELLM_MODEL"

  if out="$(run_with_copilot "$prompt")"; then
    printf '%s\n' "$out"
  else
    log "copilot pass failed; trying LiteLLM fallback"
    out="$(run_with_litellm "$prompt")" || {
      log "parallel-agent pass failed: no successful backend"
      exit 1
    }
    printf '%s\n' "$out"
  fi

  if printf '%s' "$out" | grep -q '^PARALLEL_GATE:PASS'; then
    exit 0
  fi

  if printf '%s' "$out" | grep -q '^PARALLEL_GATE:FAIL'; then
    log "parallel-agent pass blocked"
    exit 1
  fi

  log "parallel-agent pass returned no explicit PASS/FAIL marker"
  exit 1
}

main "$@"
