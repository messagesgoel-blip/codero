#!/usr/bin/env bash
set -euo pipefail

# PR-Agent Second-Pass Review (Fallback 1)
# Second-pass review using PR-Agent via LiteLLM for pre-commit quality gate.
# Requires GitHub token and LiteLLM configuration.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
PR_AGENT_BIN="${CODERO_PR_AGENT_BIN:-pr-agent}"
LITELLM_URL_RAW="${CODERO_LITELLM_URL:-${LITELLM_PROXY_URL:-http://localhost:4000/v1}}"
PRIMARY_MODEL="${CODERO_PR_AGENT_MODEL:-${CODERO_SECOND_PASS_LITELLM_MODEL:-review}}"
MODEL_SET_RAW="${CODERO_PR_AGENT_FALLBACK_MODELS:-${CODERO_SECOND_PASS_LITELLM_MODELS:-$PRIMARY_MODEL}}"
TIMEOUT_SEC="${CODERO_PR_AGENT_TIMEOUT_SEC:-240}"

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
    return 1
  fi
  return 0
}

strip_quotes() {
  local raw="${1:-}"
  raw="${raw%\"}"
  raw="${raw#\"}"
  raw="${raw%\'}"
  raw="${raw#\'}"
  printf '%s' "$raw"
}

read_key_from_file() {
  local key="$1"
  local env_file="$2"
  local raw
  raw="$(grep -E "^${key}=" "$env_file" 2>/dev/null | head -n 1 | cut -d'=' -f2- || true)"
  strip_quotes "$raw"
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

build_diff() {
  git -C "$REPO_PATH" diff --cached --no-ext-diff --binary -- .
}

load_litellm_key() {
  local key raw env_file

  if [ -n "${LITELLM_MASTER_KEY:-}" ]; then
    echo "$LITELLM_MASTER_KEY"
    return 0
  fi
  if [ -n "${LITELLM_API_KEY:-}" ]; then
    echo "$LITELLM_API_KEY"
    return 0
  fi

  env_file=""
  if env_file="$(find_env_file)"; then
    :
  fi

  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    for key in LITELLM_MASTER_KEY LITELLM_API_KEY; do
      raw="$(read_key_from_file "$key" "$env_file")"
      if [ -n "$raw" ]; then
        echo "$raw"
        return 0
      fi
    done
  fi

  if [ -f /srv/storage/shared/config/litellm/.env ]; then
    for key in LITELLM_MASTER_KEY LITELLM_API_KEY; do
      raw="$(grep -E "^${key}=" /srv/storage/shared/config/litellm/.env | head -n 1 | cut -d= -f2- || true)"
      raw="${raw%\"}"
      raw="${raw#\"}"
      raw="${raw%\'}"
      raw="${raw#\'}"
      if [ -n "$raw" ]; then
        echo "$raw"
        return 0
      fi
    done
  fi

  if [ -n "${OPENAI_API_KEY:-}" ]; then
    echo "$OPENAI_API_KEY"
    return 0
  fi

  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    raw="$(read_key_from_file "OPENAI_API_KEY" "$env_file")"
    if [ -n "$raw" ]; then
      echo "$raw"
      return 0
    fi
  fi

  return 1
}

model_list_to_json() {
  local input="$1"
  printf '%s' "$input" | jq -R -s -c '
    split(",")
    | map(gsub("^\\s+|\\s+$"; ""))
    | map(select(length > 0))
  '
}

main() {
  if ! require_cmd jq; then
    echo "Error: required command not found: jq" >&2
    exit 1
  fi

  if ! require_cmd "$PR_AGENT_BIN"; then
    echo "Error: pr-agent binary not found ($PR_AGENT_BIN)" >&2
    echo "Install with: pip install pr-agent" >&2
    exit 1
  fi

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  local diff
  diff="$(build_diff)"
  if [ -z "$diff" ]; then
    echo "No staged changes to review."
    exit 0
  fi

  local litellm_key github_token fallback_json litellm_base
  if ! litellm_key="$(load_litellm_key)"; then
    echo "Error: LiteLLM key not found. Set LITELLM_MASTER_KEY, LITELLM_API_KEY, or OPENAI_API_KEY in environment or $REPO_PATH/.env" >&2
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

  local result exit_code=0
  result="$(
    cd "$REPO_PATH" &&
      OPENAI__API_BASE="$litellm_base" \
      OPENAI__KEY="$litellm_key" \
      CONFIG__MODEL="$PRIMARY_MODEL" \
      CONFIG__FALLBACK_MODELS="$fallback_json" \
      GITHUB__USER_TOKEN="$github_token" \
      "$TIMEOUT_CMD" "$TIMEOUT_SEC" "$PR_AGENT_BIN" review --local 2>&1
  )" || exit_code=$?
  if [ $exit_code -ne 0 ]; then
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
