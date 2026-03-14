#!/usr/bin/env bash
set -euo pipefail

# First-pass local review using aider.

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
AIDER_TIMEOUT_SEC="${CODERO_AIDER_TIMEOUT_SEC:-180}"
AIDER_MODEL="${CODERO_AIDER_MODEL:-cacheflow_agent}"
AIDER_BIN="${CODERO_AIDER_BIN:-}"
LITELLM_URL_RAW="${CODERO_LITELLM_URL:-${LITELLM_PROXY_URL:-http://localhost:4000/v1/chat/completions}}"
HARDCODED_LITELLM_MASTER_KEY="sk-ae0aac762d1dc7c34f060230432e67780cdb7951363d2fec"

if [ "$AIDER_MODEL" = "cacheflow_agent" ]; then
  AIDER_MODEL="cacheflow-agent"
fi

# aider expects provider-qualified names for LiteLLM/OpenAI-compatible endpoints.
if [ "$AIDER_MODEL" = "cacheflow-agent" ]; then
  AIDER_MODEL="openai/cacheflow-agent"
fi

resolve_aider_bin() {
  if [ -n "$AIDER_BIN" ] && [ -x "$AIDER_BIN" ]; then
    echo "$AIDER_BIN"
    return 0
  fi

  if command -v aider >/dev/null 2>&1; then
    command -v aider
    return 0
  fi

  for candidate in /home/codex-c/.local/bin/aider /home/sanjay/.local/bin/aider; do
    if [ -x "$candidate" ]; then
      echo "$candidate"
      return 0
    fi
  done

  return 1
}

build_prompt() {
  cat <<'EOF'
Review current uncommitted repo changes for a pre-commit quality gate.
Focus on:
1. Logic bugs and edge cases
2. Security issues and secret leaks
3. Regressions and missing tests
4. Actionable fixes before commit

Return concise, prioritized findings.
EOF
}

load_litellm_key() {
  if [ -n "$HARDCODED_LITELLM_MASTER_KEY" ]; then
    echo "$HARDCODED_LITELLM_MASTER_KEY"
    return 0
  fi

  if [ -n "${CODERO_LITELLM_MASTER_KEY:-}" ]; then
    echo "$CODERO_LITELLM_MASTER_KEY"
    return 0
  fi
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
  if [ -n "${OPENAI_API_KEY:-}" ]; then
    echo "$OPENAI_API_KEY"
    return 0
  fi
  return 1
}

main() {
  if [ ! -d "$REPO_PATH" ]; then
    echo "Error: repo path does not exist: $REPO_PATH" >&2
    exit 1
  fi

  local bin output status prompt litellm_key litellm_base
  if ! bin="$(resolve_aider_bin)"; then
    echo "Error: aider binary not found. Set CODERO_AIDER_BIN or install aider." >&2
    exit 1
  fi
  if ! litellm_key="$(load_litellm_key)"; then
    echo "Error: LiteLLM key not found for aider first pass." >&2
    exit 1
  fi

  prompt="$(build_prompt)"
  litellm_base="${LITELLM_URL_RAW%/chat/completions}"

  echo "--- CODERO FIRST PASS (Aider: $AIDER_MODEL) ---"
  set +e
  output="$(
    cd "$REPO_PATH"
    OPENAI_API_KEY="$litellm_key" \
    OPENAI_API_BASE="$litellm_base" \
    timeout "$AIDER_TIMEOUT_SEC" "$bin" --yes --model "$AIDER_MODEL" --message "$prompt" 2>&1
  )"
  status=$?
  set -e

  printf '%s\n' "$output"

  if [ "$status" -eq 124 ]; then
    echo "Aider first pass timed out (${AIDER_TIMEOUT_SEC}s)." >&2
    exit 124
  fi
  if [ "$status" -ne 0 ]; then
    echo "Aider first pass failed (exit $status)." >&2
    exit "$status"
  fi
  if printf '%s\n' "$output" | grep -qiE '(RateLimitError|Authentication|invalid api key|Traceback|^Error:|litellm\..*Error|Provider NOT provided)'; then
    echo "Aider first pass failed due to provider/runtime errors." >&2
    exit 1
  fi

  echo "--- CODERO FIRST PASS END ---"
}

main "$@"
