#!/usr/bin/env bash
set -euo pipefail

# PR-Agent Second-Pass Review (Fallback 1)
# Second-pass review using PR-Agent via LiteLLM for pre-commit quality gate.
# Requires GitHub token and LiteLLM configuration.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
PR_AGENT_BIN="${CODERO_PR_AGENT_BIN:-pr-agent}"
LITELLM_URL_RAW="${CODERO_LITELLM_URL:-${LITELLM_PROXY_URL:-http://localhost:4000/v1}}"
PRIMARY_MODEL="${CODERO_PR_AGENT_MODEL:-${CODERO_SECOND_PASS_LITELLM_MODEL:-qwen3-coder-plus}}"
MODEL_SET_RAW="${CODERO_PR_AGENT_FALLBACK_MODELS:-${CODERO_SECOND_PASS_LITELLM_MODELS:-$PRIMARY_MODEL}}"
TIMEOUT_SEC="${CODERO_PR_AGENT_TIMEOUT_SEC:-240}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    return 1
  fi
  return 0
}

load_litellm_key() {
  if [ -n "${LITELLM_MASTER_KEY:-}" ]; then
    echo "$LITELLM_MASTER_KEY"
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
      echo "$raw"
      return 0
    fi
  fi

  return 1
}

model_list_to_json() {
  local input="$1"
  local item first=1 out="["
  IFS=',' read -r -a items <<< "$input"
  for item in "${items[@]}"; do
    item="$(echo "$item" | xargs)"
    [ -z "$item" ] && continue
    if [ "$first" -eq 0 ]; then
      out+=" ,"
    fi
    out+="\"$item\""
    first=0
  done
  out+="]"
  printf '%s' "$out"
}

main() {
  if ! require_cmd "$PR_AGENT_BIN"; then
    echo "Error: pr-agent binary not found ($PR_AGENT_BIN)" >&2
    echo "Install with: pip install pr-agent" >&2
    exit 1
  fi

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  local litellm_key github_token fallback_json litellm_base
  if ! litellm_key="$(load_litellm_key)"; then
    echo "Error: LiteLLM key not found. Set LITELLM_MASTER_KEY or add it in $REPO_PATH/.env" >&2
    exit 1
  fi

  github_token="${CODERO_GITHUB_TOKEN:-${GH_TOKEN:-${GITHUB_TOKEN:-}}}"
  if [ -z "$github_token" ]; then
    echo "Error: GitHub token not found. Set CODERO_GITHUB_TOKEN, GH_TOKEN, or GITHUB_TOKEN." >&2
    exit 1
  fi

  fallback_json="$(model_list_to_json "$MODEL_SET_RAW")"
  litellm_base="${LITELLM_URL_RAW%/chat/completions}"

  echo "--- CODERO PR-AGENT FALLBACK (via LiteLLM) ---"
  echo "LiteLLM base: $litellm_base"
  echo "Model: $PRIMARY_MODEL"
  echo "Fallback models: $fallback_json"

  local result
  if ! result="$(timeout "$TIMEOUT_SEC" sh -c "
    cd '$REPO_PATH' &&
    OPENAI__API_BASE='$litellm_base' \
    OPENAI__KEY='$litellm_key' \
    CONFIG__MODEL='$PRIMARY_MODEL' \
    CONFIG__FALLBACK_MODELS='$fallback_json' \
    GITHUB__USER_TOKEN='$github_token' \
    $PR_AGENT_BIN --help 2>&1
  " 2>&1)"; then
    exit_code=$?
    if [ $exit_code -eq 124 ]; then
      echo "Error: PR-Agent review timed out after ${TIMEOUT_SEC}s"
      exit 1
    fi
    echo "$result" >&2
    exit 1
  fi

  echo "$result"
  echo "--- CODERO PR-AGENT FALLBACK END ---"
}

main "$@"
