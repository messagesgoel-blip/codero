#!/usr/bin/env bash
set -euo pipefail

# Generic local first-pass review using LiteLLM chat completions.
# Intended for pre-commit use on uncommitted changes.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
LITELLM_URL="${CODERO_LITELLM_URL:-${LITELLM_PROXY_URL:-http://localhost:4000/v1/chat/completions}}"
LITELLM_MODEL="${CODERO_LITELLM_MODEL:-${LITELLM_MODEL:-qwen3-coder-plus}}"
TIMEOUT_SEC="${CODERO_FIRST_PASS_TIMEOUT_SEC:-120}"

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

build_diff() {
  local tracked untracked file file_diff
  tracked="$(git -C "$REPO_PATH" diff HEAD || true)"
  untracked=""

  while IFS= read -r -d '' file; do
    file_diff="$(git -C "$REPO_PATH" diff --no-index -- /dev/null "$file" || [ $? -eq 1 ])"
    untracked="${untracked}${file_diff}"
  done < <(git -C "$REPO_PATH" ls-files --others --exclude-standard -z)

  printf '%s%s' "$tracked" "$untracked"
}

main() {
  require_cmd git
  require_cmd curl
  require_cmd jq

  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  local key diff prompt response result
  if ! key="$(load_litellm_key)"; then
    echo "Error: LiteLLM key not found. Set LITELLM_MASTER_KEY or add it in $REPO_PATH/.env" >&2
    exit 1
  fi

  diff="$(build_diff)"
  if [ -z "$diff" ]; then
    echo "No uncommitted changes to review."
    exit 0
  fi

  prompt="Review the following git diff for a pre-commit quality gate.\n\nFocus on:\n1. Logic bugs and edge cases\n2. Security issues and secret leaks\n3. Regressions and missing tests\n4. Actionable fixes before commit\n\nReturn concise, prioritized findings.\n\nDIFF:\n$diff"

  echo "--- CODERO FIRST PASS (LiteLLM: $LITELLM_MODEL) ---"
  response="$(curl -sS -X POST "$LITELLM_URL" \
    --max-time "$TIMEOUT_SEC" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $key" \
    -d "{\"model\":\"$LITELLM_MODEL\",\"messages\":[{\"role\":\"system\",\"content\":\"You are a senior software engineer performing a strict local pre-commit review.\"},{\"role\":\"user\",\"content\":$(printf '%s' "$prompt" | jq -Rs .)}],\"temperature\":0.2,\"cache\":{\"prompt\":\"$prompt\"}}")"

  result="$(printf '%s' "$response" | jq -r '.choices[0].message.content // empty')"
  if [ -z "$result" ] || [ "$result" = "null" ]; then
    echo "Error: empty LiteLLM response" >&2
    printf '%s\n' "$response" | jq . >&2 || printf '%s\n' "$response" >&2
    exit 1
  fi

  printf '%s\n' "$result"
  echo "--- CODERO FIRST PASS END ---"
}

main "$@"
