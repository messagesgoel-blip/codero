#!/usr/bin/env bash
set -euo pipefail

# Gemini Second-Pass Review (Primary Gate 4)
# API-key based review flow (LiteLLM or Gemini API).
# No OAuth/Gemini CLI dependency.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
# API calls can exceed 45s on larger staged diffs; override via CODERO_SECOND_PASS_TIMEOUT_SEC.
TIMEOUT_SEC="${CODERO_SECOND_PASS_TIMEOUT_SEC:-180}"
GEMINI_MODEL="${CODERO_GEMINI_MODEL:-gemini-2.5-flash-lite}"

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
  raw="$(grep -E "^${key}=" "$env_file" 2>/dev/null | head -n 1 | cut -d= -f2- || true)"
  strip_quotes "$raw"
}

write_secret_headers_file() {
  local header_line="$1"
  local header_file
  header_file="$(mktemp "${TMPDIR:-/tmp}/codero-curl-headers.XXXXXX")"
  chmod 600 "$header_file"
  {
    echo "Content-Type: application/json"
    echo "$header_line"
  } > "$header_file"
  printf '%s' "$header_file"
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
  local base_url key raw litellm_env_path env_file
  env_file=""
  if env_file="$(find_env_file)"; then
    :
  fi

  base_url="${GEMINI_API_BASE:-https://generativelanguage.googleapis.com/v1beta}"
  raw="$(strip_quotes "${CODERO_GEMINI_SECOND_PASS_API_KEY:-}")"
  if [ -n "$raw" ]; then
    printf 'gemini|%s|%s' "$base_url" "$raw"
    return 0
  fi
  raw="$(strip_quotes "${GEMINI_API_KEY:-}")"
  if [ -n "$raw" ]; then
    printf 'gemini|%s|%s' "$base_url" "$raw"
    return 0
  fi
  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    raw="$(read_key_from_file "CODERO_GEMINI_SECOND_PASS_API_KEY" "$env_file")"
    if [ -n "$raw" ]; then
      printf 'gemini|%s|%s' "$base_url" "$raw"
      return 0
    fi
    raw="$(read_key_from_file "GEMINI_API_KEY" "$env_file")"
    if [ -n "$raw" ]; then
      printf 'gemini|%s|%s' "$base_url" "$raw"
      return 0
    fi
  fi

  base_url="${LITELLM_URL:-${LITELLM_BASE_URL:-${LITELLM_HOST:-http://localhost:4000}/v1}}"
  for key in LITELLM_MASTER_KEY LITELLM_API_KEY OPENAI_API_KEY; do
    raw="${!key-}"
    raw="$(strip_quotes "$raw")"
    if [ -n "$raw" ]; then
      printf 'litellm|%s|%s' "$base_url" "$raw"
      return 0
    fi
  done

  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    for key in LITELLM_MASTER_KEY LITELLM_API_KEY OPENAI_API_KEY; do
      raw="$(read_key_from_file "$key" "$env_file")"
      if [ -n "$raw" ]; then
        printf 'litellm|%s|%s' "$base_url" "$raw"
        return 0
      fi
    done
  fi

  litellm_env_path="${LITELLM_ENV_PATH:-/opt/docker/apps/litellm/.env}"
  if [ -f "$litellm_env_path" ]; then
    for key in LITELLM_MASTER_KEY LITELLM_API_KEY OPENAI_API_KEY; do
      raw="$(read_key_from_file "$key" "$litellm_env_path")"
      if [ -n "$raw" ]; then
        printf 'litellm|%s|%s' "$base_url" "$raw"
        return 0
      fi
    done
  fi

  return 1
}

build_diff() {
  git -C "$REPO_PATH" diff --cached --no-ext-diff --binary -- .
}

run_via_litellm() {
  local base_url="$1"
  local api_key="$2"
  local prompt="$3"
  local endpoint payload response exit_code content err header_file

  endpoint="$base_url"
  if [[ "$endpoint" != */chat/completions ]]; then
    endpoint="${endpoint%/}/chat/completions"
  fi

  payload="$(jq -n \
    --arg model "$GEMINI_MODEL" \
    --arg prompt "$prompt" \
    '{
      model: $model,
      messages: [
        { role: "system", content: "You are a strict code reviewer. Focus on logic bugs, regressions, security, and missing tests." },
        { role: "user", content: $prompt }
      ],
      temperature: 0
    }')"

  header_file="$(write_secret_headers_file "Authorization: Bearer ${api_key}")"
  exit_code=0
  response=$("$TIMEOUT_CMD" "$TIMEOUT_SEC" curl -sS -X POST "$endpoint" \
    -H "@${header_file}" \
    -d "$payload" 2>&1) || exit_code=$?
  rm -f "$header_file"

  if [ $exit_code -ne 0 ]; then
    if [ $exit_code -eq 124 ]; then
      echo "Error: Gemini review timed out after ${TIMEOUT_SEC}s" >&2
      return 124
    fi
    echo "$response" >&2
    return 1
  fi

  content="$(printf '%s' "$response" | jq -r '.choices[0].message.content // empty' 2>/dev/null || true)"
  if [ -n "$content" ]; then
    printf '%s\n' "$content"
    return 0
  fi

  err="$(printf '%s' "$response" | jq -r '.error.message // .error // empty' 2>/dev/null || true)"
  if [ -n "$err" ]; then
    echo "$err" >&2
  else
    echo "$response" >&2
  fi
  return 1
}

run_via_gemini_api() {
  local base_url="$1"
  local api_key="$2"
  local prompt="$3"
  local endpoint payload response exit_code content err header_file

  # Keep keys out of URL query strings to avoid leakage in process lists and URL logs.
  endpoint="${base_url%/}/models/${GEMINI_MODEL}:generateContent"
  payload="$(jq -n \
    --arg prompt "$prompt" \
    '{
      contents: [
        {
          role: "user",
          parts: [
            { text: $prompt }
          ]
        }
      ]
    }')"

  header_file="$(write_secret_headers_file "x-goog-api-key: ${api_key}")"
  exit_code=0
  response=$("$TIMEOUT_CMD" "$TIMEOUT_SEC" curl -sS -X POST "$endpoint" \
    -H "@${header_file}" \
    -d "$payload" 2>&1) || exit_code=$?
  rm -f "$header_file"

  if [ $exit_code -ne 0 ]; then
    if [ $exit_code -eq 124 ]; then
      echo "Error: Gemini review timed out after ${TIMEOUT_SEC}s" >&2
      return 124
    fi
    echo "$response" >&2
    return 1
  fi

  content="$(printf '%s' "$response" | jq -r '[.candidates[]?.content.parts[]?.text] | join("\n")' 2>/dev/null || true)"
  if [ -n "$content" ] && [ "$content" != "null" ]; then
    printf '%s\n' "$content"
    return 0
  fi

  err="$(printf '%s' "$response" | jq -r '.error.message // .error // empty' 2>/dev/null || true)"
  if [ -n "$err" ]; then
    echo "$err" >&2
  else
    echo "$response" >&2
  fi
  return 1
}

run_gemini_review() {
  local provider="$1"
  local base_url="$2"
  local api_key="$3"
  local diff="$4"
  local prompt

  prompt="Review this staged code diff for bugs, security issues, and regressions. Be concise. List file locations. If no issues, say 'No issues found.'

DIFF:
$diff"

  case "$provider" in
    litellm)
      run_via_litellm "$base_url" "$api_key" "$prompt"
      ;;
    gemini)
      run_via_gemini_api "$base_url" "$api_key" "$prompt"
      ;;
    *)
      echo "Unsupported provider: $provider" >&2
      return 1
      ;;
  esac
}

main() {
  if ! require_cmd git; then
    echo "Error: required command not found: git" >&2
    exit 1
  fi
  if ! require_cmd curl; then
    echo "Error: required command not found: curl" >&2
    exit 1
  fi
  if ! require_cmd jq; then
    echo "Error: required command not found: jq" >&2
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

  local api_config provider base_url api_key
  if ! api_config="$(load_api_config)"; then
    echo "Error: no Gemini backend key found." >&2
    echo "Set one of: LITELLM_MASTER_KEY, LITELLM_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY" >&2
    exit 1
  fi

  provider="$(echo "$api_config" | cut -d'|' -f1)"
  base_url="$(echo "$api_config" | cut -d'|' -f2)"
  api_key="$(echo "$api_config" | cut -d'|' -f3)"

  echo "--- CODERO SECOND PASS (Gemini API) ---"
  echo "Provider: $provider"
  echo "Model: $GEMINI_MODEL"
  echo "Timeout: ${TIMEOUT_SEC}s"

  local result exit_code=0
  result="$(run_gemini_review "$provider" "$base_url" "$api_key" "$diff")" || exit_code=$?
  if [ $exit_code -ne 0 ]; then
    if [ -n "$result" ]; then
      echo "$result" >&2
    fi
    exit 1
  fi

  echo "$result"
  echo "--- CODERO SECOND PASS END ---"
}

main "$@"
