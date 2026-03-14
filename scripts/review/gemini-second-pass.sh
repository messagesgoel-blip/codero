#!/usr/bin/env bash
set -euo pipefail

# Generic Gemini review pass for uncommitted changes.
# Uses gcli-b/gcli aliases with preconfigured auth (no API key handling).

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
GEMINI_MODEL="${CODERO_GEMINI_MODEL:-gemini-2.5-flash}"
FAST_MODE="${CODERO_GEMINI_FAST_MODE:-1}"
TIMEOUT_SEC="${CODERO_GEMINI_TIMEOUT_SEC:-45}"
GEMINI_BIN="${CODERO_GEMINI_BIN:-}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    exit 1
  fi
}

resolve_gemini_bin() {
  if [ -n "$GEMINI_BIN" ] && command -v "$GEMINI_BIN" >/dev/null 2>&1; then
    command -v "$GEMINI_BIN"
    return 0
  fi
  if command -v gcli-b >/dev/null 2>&1; then
    command -v gcli-b
    return 0
  fi
  if command -v gcli >/dev/null 2>&1; then
    command -v gcli
    return 0
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
  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  local bin diff prompt response status
  if ! bin="$(resolve_gemini_bin)"; then
    echo "Error: Gemini CLI alias not found. Install/use gcli-b or gcli." >&2
    exit 1
  fi

  diff="$(build_diff)"
  if [ -z "$diff" ]; then
    echo "No uncommitted changes to review."
    exit 0
  fi

  if [ "$FAST_MODE" = "1" ]; then
    prompt="Fast pre-commit review. Return max 5 prioritized bullets: critical bug/security/regression only.\n\nDIFF:\n$diff"
  else
    prompt="Review the following git diff for a pre-commit quality gate.\n\nFocus on:\n1. Logic bugs and edge cases\n2. Security issues and secret leaks\n3. Regressions and missing tests\n4. Actionable fixes before commit\n\nReturn concise, prioritized findings.\n\nDIFF:\n$diff"
  fi

  echo "--- CODERO SECOND PASS (Gemini: $GEMINI_MODEL) ---"
  set +e
  response="$(
    cd "$REPO_PATH"
    printf '%s' "$prompt" | timeout "$TIMEOUT_SEC" "$bin" -m "$GEMINI_MODEL" -p "" --output-format text 2>&1
  )"
  status=$?
  set -e

  if [ "$status" -eq 124 ]; then
    echo "Gemini second pass timed out (${TIMEOUT_SEC}s)." >&2
    exit 124
  fi
  if [ "$status" -ne 0 ]; then
    printf '%s\n' "$response" >&2
    echo "Gemini second pass failed (exit $status)." >&2
    exit "$status"
  fi

  if [ -z "$response" ]; then
    echo "Error: empty Gemini response" >&2
    exit 1
  fi

  printf '%s\n' "$response"
  echo "--- CODERO SECOND PASS END ---"
}

main "$@"
