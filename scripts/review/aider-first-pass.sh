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

load_api_config() {
  local env_file=""
  if env_file="$(find_env_file)"; then
    :
  fi

  # Priority 1: Gemini API (free tier, reliable)
  if [ -n "${CODERO_AIDER_GEMINI_API_KEY:-}" ]; then
    echo "gemini||${CODERO_AIDER_GEMINI_API_KEY}"
    return 0
  fi
  
  
  if [ -n "${GEMINI_API_KEY:-}" ]; then
    echo "gemini||${GEMINI_API_KEY}"
    return 0
  fi


  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    local gemini_key
    gemini_key="$(read_key_from_file "CODERO_AIDER_GEMINI_API_KEY" "$env_file")"
    if [ -n "$gemini_key" ]; then
      echo "gemini||${gemini_key}"
      return 0
    fi

    gemini_key="$(read_key_from_file "GEMINI_API_KEY" "$env_file")"
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
  
  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    local or_key
    or_key="$(read_key_from_file "OPENROUTER_API_KEY" "$env_file")"
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


  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    local minimax_key
    minimax_key="$(read_key_from_file "MINIMAX_API_KEY" "$env_file")"
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

  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    for key in LITELLM_MASTER_KEY LITELLM_API_KEY; do
      litellm_key="$(read_key_from_file "$key" "$env_file")"
      if [ -n "$litellm_key" ]; then
        echo "litellm|${base_url}|${litellm_key}"
        return 0
      fi
    done
  fi

  litellm_env_path="${LITELLM_ENV_PATH:-/srv/storage/shared/config/litellm/.env}"
  if [ -f "$litellm_env_path" ]; then
    for key in LITELLM_MASTER_KEY LITELLM_API_KEY OPENAI_API_KEY; do
      litellm_key="$(read_key_from_file "$key" "$litellm_env_path")"
      if [ -n "$litellm_key" ]; then
        echo "litellm|${base_url}|${litellm_key}"
        return 0
      fi
    done
  fi

  litellm_key="${OPENAI_API_KEY:-}"
  if [ -n "$litellm_key" ]; then
    echo "litellm|${base_url}|${litellm_key}"
    return 0
  fi

  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    litellm_key="$(read_key_from_file "OPENAI_API_KEY" "$env_file")"
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

  if git -C "$REPO_PATH" diff --cached --quiet --exit-code -- .; then
    echo "No staged changes to review."
    exit 0
  fi

  local diff
  diff="$(build_diff)"
  if [ -z "$diff" ]; then
    echo "No staged changes to review."
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

  local message review_payload
  message="Review the staged code changes for bugs, security issues, and code quality. List findings with file locations. If no issues, say 'No issues found.'"
  review_payload="$message

STAGED DIFF:
$diff"

  cd "$REPO_PATH"
  
  local aider_args=(
    --model "$selected_model"
    --no-auto-commits
    --no-gitignore
    --no-show-model-warnings
    --yes
    --message "$review_payload"
  )
  
  case "$provider" in
    minimax)
      aider_args+=(
        --openai-api-base "$base_url"
      )
      export OPENAI_API_KEY="$api_key"
      ;;
    openrouter)
      aider_args+=(
        --openai-api-base "$base_url"
      )
      export OPENAI_API_KEY="$api_key"
      export OPENROUTER_API_KEY="$api_key"
      ;;
    litellm)
      aider_args+=(
        --openai-api-base "$base_url"
      )
      export OPENAI_API_KEY="$api_key"
      ;;
    gemini)
      export GEMINI_API_KEY="$api_key"
      ;;
  esac
  
  local exit_code=0
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
