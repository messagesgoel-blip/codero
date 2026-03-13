#!/usr/bin/env bash
set -euo pipefail

# Second-pass fallback review using PR-Agent via LiteLLM.
# Requires a PR URL; provide CODERO_PR_URL or ensure gh can resolve it.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
PR_AGENT_BIN="${CODERO_PR_AGENT_BIN:-pr-agent}"
LITELLM_URL_RAW="${CODERO_LITELLM_URL:-${LITELLM_PROXY_URL:-http://localhost:4000/v1}}"
PRIMARY_MODEL="${CODERO_SECOND_PASS_LITELLM_MODEL:-${CODERO_LITELLM_MODEL:-qwen3-coder-plus}}"
MODEL_SET_RAW="${CODERO_SECOND_PASS_LITELLM_MODELS:-${CODERO_LITELLM_MODELS:-$PRIMARY_MODEL}}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    exit 1
  fi
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

resolve_pr_url() {
  if [ -n "${CODERO_PR_URL:-}" ]; then
    echo "$CODERO_PR_URL"
    return 0
  fi

  if command -v gh >/dev/null 2>&1; then
    (
      cd "$REPO_PATH"
      gh pr view --json url -q .url 2>/dev/null || true
    )
    return 0
  fi

  echo ""
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
  require_cmd git

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  if ! command -v "$PR_AGENT_BIN" >/dev/null 2>&1; then
    echo "Error: PR-Agent binary not found: $PR_AGENT_BIN" >&2
    echo "Install qodo-ai/pr-agent CLI and ensure it is in PATH." >&2
    exit 1
  fi

  local litellm_key pr_url fallback_json litellm_base
  if ! litellm_key="$(load_litellm_key)"; then
    echo "Error: LiteLLM key not found. Set LITELLM_MASTER_KEY or add it in $REPO_PATH/.env" >&2
    exit 1
  fi

  pr_url="$(resolve_pr_url)"
  if [ -z "$pr_url" ]; then
    echo "Error: PR URL not found. Set CODERO_PR_URL or run from a branch with an open PR accessible via gh." >&2
    exit 1
  fi

  fallback_json="$(model_list_to_json "$MODEL_SET_RAW")"
  litellm_base="${LITELLM_URL_RAW%/chat/completions}"

  echo "--- CODERO SECOND PASS (PR-Agent fallback via LiteLLM) ---"
  echo "PR URL: $pr_url"
  echo "LiteLLM base: $litellm_base"
  echo "Model: $PRIMARY_MODEL"
  echo "Fallback models: $fallback_json"

  (
    cd "$REPO_PATH"
    OPENAI__API_BASE="$litellm_base" \
    OPENAI__KEY="$litellm_key" \
    CONFIG__MODEL="$PRIMARY_MODEL" \
    CONFIG__FALLBACK_MODELS="$fallback_json" \
    "$PR_AGENT_BIN" --pr_url "$pr_url" review "$@" 2>&1
  )

  echo "--- CODERO SECOND PASS END ---"
}

main "$@"
