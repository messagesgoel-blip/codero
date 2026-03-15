#!/usr/bin/env bash
set -euo pipefail

# Aider First-Pass Review (Primary Gate 1)
# Local review using aider AI for pre-commit quality gate.
# Supports multiple free/paid model backends:
# 1. Gemini (free tier) - set GEMINI_API_KEY
# 2. OpenRouter (free tier models) - set OPENROUTER_API_KEY
# 3. MiniMax (free tier) - set MINIMAX_API_KEY
# 4. LiteLLM (local proxy) - set LITELLM_MASTER_KEY
#
# Default: Uses Gemini 2.5 Flash Lite (fast, reliable)

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
TIMEOUT_SEC="${CODERO_FIRST_PASS_TIMEOUT_SEC:-90}"

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

# Default to Gemini (free, reliable with API key)
AIDER_MODEL="${CODERO_AIDER_MODEL:-gemini-2.5-flash-lite}"
AIDER_LITELLM_MODEL="${CODERO_AIDER_LITELLM_MODEL:-code}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    return 1
  fi
  return 0
}

load_api_config() {
  # Priority 1: Gemini API (free tier, reliable)
  if [ -n "${CODERO_AIDER_GEMINI_API_KEY:-}" ]; then
    echo "gemini||${CODERO_AIDER_GEMINI_API_KEY}"
    return 0
  fi
  
  
  if [ -n "${GEMINI_API_KEY:-}" ]; then
    echo "gemini||${GEMINI_API_KEY}"
    return 0
  fi


  if [ -f "$REPO_PATH/.env" ]; then
    local gemini_key
    gemini_key="$(grep -E '^CODERO_AIDER_GEMINI_API_KEY=' "$REPO_PATH/.env" 2>/dev/null | head -n 1 | cut -d'=' -f2- || true)"
    gemini_key="${gemini_key%\"}"
    gemini_key="${gemini_key#\"}"
    gemini_key="${gemini_key%\'}"
    gemini_key="${gemini_key#\'}"
    if [ -n "$gemini_key" ]; then
      echo "gemini||${gemini_key}"
      return 0
    fi

    gemini_key="$(grep -E '^GEMINI_API_KEY=' "$REPO_PATH/.env" 2>/dev/null | head -n 1 | cut -d'=' -f2- || true)"
    gemini_key="${gemini_key%\"}"
    gemini_key="${gemini_key#\"}"
    gemini_key="${gemini_key%\'}"
    gemini_key="${gemini_key#\'}"
    if [ -n "$gemini_key" ]; then
      echo "gemini||${gemini_key}"
      return 0
    fi
  fi


  
  # Priority 2: OpenRouter (free models)
  if [ -n "${OPENROUTER_API_KEY:-}" ]; then
    echo "openrouter|https://openrouter.ai/api/v1|${OPENROUTER_API_KEY}"
    return 0
  fi
  
  if [ -f "$REPO_PATH/.env" ]; then
    local or_key
    or_key="$(grep -E '^OPENROUTER_API_KEY=' "$REPO_PATH/.env" 2>/dev/null | head -n 1 | cut -d'=' -f2- || true)"
    or_key="${or_key%\"}"
    or_key="${or_key#\"}"
    or_key="${or_key%\'}"
    or_key="${or_key#\'}"
    if [ -n "$or_key" ]; then
      echo "openrouter|https://openrouter.ai/api/v1|$or_key"
      return 0
    fi
  fi
  
  # Priority 3: MiniMax (good for code)
  
  if [ -n "${MINIMAX_API_KEY:-}" ]; then
    echo "minimax|https://api.minimax.chat/v1|${MINIMAX_API_KEY}"
    return 0
  fi


  if [ -f "$REPO_PATH/.env" ]; then
    local minimax_key
    minimax_key="$(grep -E '^MINIMAX_API_KEY=' "$REPO_PATH/.env" 2>/dev/null | head -n 1 | cut -d'=' -f2- || true)"
    minimax_key="${minimax_key%\"}"
    minimax_key="${minimax_key#\"}"
    minimax_key="${minimax_key%\'}"
    minimax_key="${minimax_key#\'}"
    if [ -n "$minimax_key" ]; then
      echo "minimax|https://api.minimax.chat/v1|$minimax_key"
      return 0
    fi
  fi


  
  # Priority 4: LiteLLM proxy
  local litellm_key base_url key
  base_url="${LITELLM_URL:-${LITELLM_BASE_URL:-http://localhost:4000/v1}}"

  for key in LITELLM_MASTER_KEY LITELLM_API_KEY; do
    litellm_key="${!key:-}"
    if [ -n "$litellm_key" ]; then
      echo "litellm|${base_url}|${litellm_key}"
      return 0
    fi
  done

  if [ -f "$REPO_PATH/.env" ]; then
    for key in LITELLM_MASTER_KEY LITELLM_API_KEY; do
      litellm_key="$(grep -E "^${key}=" "$REPO_PATH/.env" 2>/dev/null | head -n 1 | cut -d'=' -f2- || true)"
      litellm_key="${litellm_key%\"}"
      litellm_key="${litellm_key#\"}"
      litellm_key="${litellm_key%\'}"
      litellm_key="${litellm_key#\'}"
      if [ -n "$litellm_key" ]; then
        echo "litellm|${base_url}|${litellm_key}"
        return 0
      fi
    done
  fi

  litellm_env_path="${LITELLM_ENV_PATH:-/opt/docker/apps/litellm/.env}"
  if [ -f "$litellm_env_path" ]; then
    raw="$(grep -E '^LITELLM_MASTER_KEY=' "$litellm_env_path" | head -n 1 | cut -d= -f2- || true)"
    raw="${raw%\"}"
    raw="${raw#\"}"
    raw="${raw%\'}"
    raw="${raw#\'}"
    if [ -n "$raw" ]; then
      echo "litellm|${base_url}|$raw"
      return 0
    fi
    raw="$(grep -E '^LITELLM_API_KEY=' "$litellm_env_path" | head -n 1 | cut -d= -f2- || true)"
    raw="${raw%\"}"
    raw="${raw#\"}"
    raw="${raw%\'}"
    raw="${raw#\'}"
    if [ -n "$raw" ]; then
      echo "litellm|${base_url}|$raw"
      return 0
    fi
  fi

  litellm_key="${OPENAI_API_KEY:-}"
  if [ -n "$litellm_key" ]; then
    echo "litellm|${base_url}|${litellm_key}"
    return 0
  fi

  if [ -f "$REPO_PATH/.env" ]; then
    litellm_key="$(grep -E '^OPENAI_API_KEY=' "$REPO_PATH/.env" 2>/dev/null | head -n 1 | cut -d'=' -f2- || true)"
    litellm_key="${litellm_key%\"}"
    litellm_key="${litellm_key#\"}"
    litellm_key="${litellm_key%\'}"
    litellm_key="${litellm_key#\'}"
    if [ -n "$litellm_key" ]; then
      echo "litellm|${base_url}|${litellm_key}"
      return 0
    fi
  fi
  
  return 1
}

build_diff() {
  git -C "$REPO_PATH" diff --cached --no-ext-diff --binary -- .
}

main() {
  if ! require_cmd aider; then
    echo "Error: required command not found: aider" >&2
    echo "Install with: pip install aider-chat" >&2
    exit 1
  fi

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  local diff
  diff="$(build_diff)"
  if [ -z "$diff" ]; then
    echo "No uncommitted changes to review."
    exit 0
  fi

  echo "--- CODERO FIRST PASS (Aider) ---"

  local api_config
  if ! api_config="$(load_api_config)"; then
    echo "Error: No API configuration found." >&2
    echo "Set one of: MINIMAX_API_KEY, OPENROUTER_API_KEY, GEMINI_API_KEY, or LITELLM_MASTER_KEY" >&2
    exit 1
  fi

  local provider base_url api_key selected_model
  provider="$(echo "$api_config" | cut -d'|' -f1)"
  base_url="$(echo "$api_config" | cut -d'|' -f2)"
  api_key="$(echo "$api_config" | cut -d'|' -f3)"
  selected_model="$AIDER_MODEL"
  if [ "$provider" = "litellm" ] && [ -z "${CODERO_AIDER_MODEL:-}" ]; then
    selected_model="$AIDER_LITELLM_MODEL"
  fi
  
  echo "Provider: $provider"
  echo "Model: $selected_model"
  echo "Timeout: ${TIMEOUT_SEC}s"

  local message
  message="Review the staged code changes for bugs, security issues, and code quality. List findings with file locations. If no issues, say 'No issues found.'"

  cd "$REPO_PATH"
  
  local aider_args=(
    --model "$selected_model"
    --no-auto-commits
    --no-gitignore
    --no-show-model-warnings
    --yes
    --message "$message"
  )
  
  case "$provider" in
    minimax)
      aider_args+=(
        --openai-api-base "$base_url"
        --openai-api-key "$api_key"
      )
      ;;
    openrouter)
      aider_args+=(
        --openai-api-base "$base_url"
        --openai-api-key "$api_key"
      )
      export OPENROUTER_API_KEY="$api_key"
      ;;
    litellm)
      aider_args+=(
        --openai-api-base "$base_url"
        --openai-api-key "$api_key"
      )
      ;;
    gemini)
      export GEMINI_API_KEY="$api_key"
      ;;
  esac
  
  exit_code=0
  "$TIMEOUT_CMD" "$TIMEOUT_SEC" aider "${aider_args[@]}" 2>&1 || exit_code=$?
  if [ $exit_code -ne 0 ]; then
    if [ $exit_code -eq 124 ]; then
      echo "Error: Aider review timed out after ${TIMEOUT_SEC}s"
      exit 1
    fi
    exit 1
  fi

  echo "--- CODERO FIRST PASS END ---"
}

main "$@"
