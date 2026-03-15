#!/usr/bin/env bash
set -euo pipefail

# Copilot Third-Pass Review (Gate 5)
# Review using GitHub Copilot CLI or OpenAI API with gpt-4o-mini.
# Supports both Copilot OAuth and OpenAI API key authentication.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
TIMEOUT_SEC="${CODERO_COPILOT_TIMEOUT_SEC:-180}"
COPILOT_MODEL="${CODERO_COPILOT_MODEL:-gpt-5-mini}"
OPENAI_MODEL="${CODERO_OPENAI_MODEL:-gpt-4o-mini}"
USE_OPENAI="${CODERO_USE_OPENAI:-false}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    return 1
  fi
  return 0
}

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


load_openai_key() {
  if [ -n "${OPENAI_API_KEY:-}" ]; then
    echo "$OPENAI_API_KEY"
    return 0
  fi

  if [ -f "$REPO_PATH/.env" ]; then
    local raw
    raw="$(grep -E '^OPENAI_API_KEY=' "$REPO_PATH/.env" | head -n 1 | cut -d'=' -f2- || true)"
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
  git -C "$REPO_PATH" diff --cached --no-ext-diff --binary -- .
}

review_with_copilot() {
  local diff="$1"

  if ! require_cmd copilot; then
    echo "Error: copilot CLI not found" >&2
    return 1
  fi

  echo "--- CODERO COPILOT PASS (Model: $COPILOT_MODEL) ---"

  # Copilot CLI rejects classic PAT in GH_TOKEN; prefer interactive OAuth/session.
  local _saved_gh_token=""
  if [ -n "${GH_TOKEN:-}" ] && [[ "${GH_TOKEN}" == ghp_* ]]; then
    _saved_gh_token="${GH_TOKEN}"
    unset GH_TOKEN
  fi

  local prompt
  prompt="You are performing a strict pre-commit quality gate review. Focus on:
1. Logic bugs and edge cases
2. Security issues and secret leaks
3. Regressions and missing tests
4. Actionable fixes before commit

Return concise, prioritized findings with code locations.

DIFF:
$diff"

  local result exit_code=0
  result=$("$TIMEOUT_CMD" "$TIMEOUT_SEC" copilot --model "$COPILOT_MODEL" -p "$prompt" 2>&1) || exit_code=$?
  if [ $exit_code -ne 0 ]; then
    if [ -n "$_saved_gh_token" ]; then
      export GH_TOKEN="$_saved_gh_token"
    fi
    if [ $exit_code -eq 124 ]; then
      echo "Error: Copilot review timed out after ${TIMEOUT_SEC}s"
      return 1
    fi
    echo "$result" >&2
    return 1
  fi

  if [ -n "$_saved_gh_token" ]; then
    export GH_TOKEN="$_saved_gh_token"
  fi
  echo "$result"
  echo "--- CODERO COPILOT PASS END ---"
}

review_with_openai() {
  local diff="$1"

  if ! require_cmd openai; then
    echo "Error: openai CLI not found" >&2
    return 1
  fi

  local api_key
  if ! api_key="$(load_openai_key)"; then
    echo "Error: OPENAI_API_KEY not found. Set it in environment or $REPO_PATH/.env" >&2
    return 1
  fi

  echo "--- CODERO OPENAI PASS (Model: $OPENAI_MODEL) ---"

  local prompt
  prompt="You are performing a strict pre-commit quality gate review. Focus on:
1. Logic bugs and edge cases
2. Security issues and secret leaks
3. Regressions and missing tests
4. Actionable fixes before commit

Return concise, prioritized findings with code locations.

DIFF:
$diff"

  local result exit_code=0
  result=$("$TIMEOUT_CMD" "$TIMEOUT_SEC" env OPENAI_API_KEY="$api_key" openai api chat.completions.create \
    -m "$OPENAI_MODEL" \
    -g system "You are a strict code reviewer. Focus on bugs, security, and best practices." \
    -g user "$prompt" 2>&1) || exit_code=$?
  if [ $exit_code -ne 0 ]; then
    if [ $exit_code -eq 124 ]; then
      echo "Error: OpenAI review timed out after ${TIMEOUT_SEC}s"
      return 1
    fi
    echo "$result" >&2
    return 1
  fi

  echo "$result"
  echo "--- CODERO OPENAI PASS END ---"
}

main() {
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

  # Try Copilot CLI first, then OpenAI fallback
  if [ "$USE_OPENAI" = "true" ]; then
    review_with_openai "$diff"
  elif require_cmd copilot; then
    review_with_copilot "$diff"
  elif require_cmd openai; then
    review_with_openai "$diff"
  else
    echo "Error: Neither copilot nor openai CLI found" >&2
    echo "Install one of:" >&2
    echo "  - copilot: npm install -g @github/copilot-cli" >&2
    echo "  - openai: pip install openai" >&2
    exit 1
  fi
}

main "$@"
