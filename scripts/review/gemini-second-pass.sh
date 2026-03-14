#!/usr/bin/env bash
set -euo pipefail

# Gemini Second-Pass Review (Primary Gate 2)
# Local review using Gemini CLI for pre-commit quality gate.
# Supports OAuth with automatic account switching on rate limit.
# Uses gemini-2.5-flash-lite for faster responses.
#
# Account switching works by:
# 1. Looking for oauth_creds files named: oauth_creds.<email_prefix>.json
# 2. Swapping the active oauth_creds.json before each attempt
#
# To set up multiple accounts:
# 1. Authenticate with each account interactively
# 2. Save the oauth_creds.json as oauth_creds.<email_prefix>.json
#    e.g., oauth_creds.msg.goel.json, oauth_creds.messages.goel.json

REPO_PATH="${CODERO_REPO_PATH:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
TIMEOUT_SEC="${CODERO_SECOND_PASS_TIMEOUT_SEC:-45}"
GEMINI_MODEL="${CODERO_GEMINI_MODEL:-gemini-2.5-flash-lite}"
MAX_RETRIES="${CODERO_GEMINI_MAX_RETRIES:-3}"
GEMINI_CONFIG_DIR="${HOME}/.gemini"

# Available accounts (will rotate on rate limit)
# These should match oauth_creds.<account>.json files
GEMINI_ACCOUNTS=("msg.goel@gmail.com" "agussalahi551@gmail.com" "messages.goel@gmail.com")
CURRENT_ACCOUNT_IDX=0

# Alternate Gemini config directories (for OAuth switching)
GEMINI_ALT_HOMES=(
  "$HOME/.gemini"
  "$HOME/.gcli-b-home/.gemini"
  "$HOME/.gcli-oci-noauth-home/.gemini"
)

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    exit 1
  fi
}

get_creds_file_for_account() {
  local account="$1"
  local email_prefix
  email_prefix=$(echo "$account" | cut -d'@' -f1)
  local creds_file="$GEMINI_CONFIG_DIR/oauth_creds.${email_prefix}.json"
  
  if [ -f "$creds_file" ]; then
    echo "$creds_file"
    return 0
  fi
  return 1
}

switch_gemini_account() {
  local target_account="$1"
  local accounts_file="$GEMINI_CONFIG_DIR/google_accounts.json"
  local creds_file="$GEMINI_CONFIG_DIR/oauth_creds.json"
  
  # Check if we have dedicated credentials for this account
  local dedicated_creds
  if dedicated_creds=$(get_creds_file_for_account "$target_account"); then
    # Backup current creds
    [ -f "$creds_file" ] && cp "$creds_file" "${creds_file}.bak"
    # Copy dedicated creds to active
    cp "$dedicated_creds" "$creds_file"
    echo "Switched OAuth credentials to: $target_account"
  else
    # Try alternate Gemini homes
    for alt_home in "${GEMINI_ALT_HOMES[@]}"; do
      if [ -d "$alt_home" ]; then
        local alt_creds="$alt_home/oauth_creds.json"
        local alt_accounts="$alt_home/google_accounts.json"
        if [ -f "$alt_creds" ] && [ -f "$alt_accounts" ]; then
          local alt_active
          alt_active=$(jq -r '.active' "$alt_accounts" 2>/dev/null || echo "")
          if [ "$alt_active" = "$target_account" ]; then
            cp "$alt_creds" "$creds_file"
            echo "Switched OAuth credentials from $alt_home to: $target_account"
            break
          fi
        fi
      fi
    done
  fi
  
  # Update google_accounts.json active field
  if [ -f "$accounts_file" ]; then
    if command -v jq >/dev/null 2>&1; then
      jq --arg new "$target_account" '.active = $new' "$accounts_file" > "${accounts_file}.tmp" && mv "${accounts_file}.tmp" "$accounts_file"
    else
      sed -i "s/\"active\": \"[^\"]*\"/\"active\": \"$target_account\"/" "$accounts_file"
    fi
    echo "Updated active account to: $target_account"
  fi
}

build_diff() {
  local tracked untracked file file_diff
  tracked="$(git -C "$REPO_PATH" diff HEAD 2>/dev/null || true)"
  untracked=""

  while IFS= read -r -d '' file; do
    if [ -f "$file" ]; then
      file_diff="$(git -C "$REPO_PATH" diff --no-index -- /dev/null "$file" 2>/dev/null || [ $? -eq 1 ])"
      untracked="${untracked}${file_diff}"
    fi
  done < <(git -C "$REPO_PATH" ls-files --others --exclude-standard -z 2>/dev/null)

  printf '%s%s' "$tracked" "$untracked"
}

is_rate_limited() {
  local output="$1"
  if echo "$output" | grep -qiE "(429|rate.*limit|Resource has been exhausted|quota|RESOURCE_EXHAUSTED)"; then
    return 0
  fi
  return 1
}

run_gemini_review() {
  local diff="$1"
  
  local prompt
  prompt="Review this code diff for bugs, security issues, and regressions. Be concise. List file locations. If no issues, say 'No issues found.'

DIFF:
$diff"

  timeout "$TIMEOUT_SEC" gemini -m "$GEMINI_MODEL" -p "$prompt" 2>&1
}

main() {
  require_cmd git
  require_cmd gemini

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

  echo "--- CODERO SECOND PASS (Gemini CLI: $GEMINI_MODEL) ---"
  echo "Model: $GEMINI_MODEL"
  echo "Timeout: ${TIMEOUT_SEC}s per attempt"

  local attempt=0
  local result=""
  local exit_code=0
  local rate_limited_accounts=()

  while [ $attempt -lt $MAX_RETRIES ]; do
    attempt=$((attempt + 1))
    local current_account="${GEMINI_ACCOUNTS[$CURRENT_ACCOUNT_IDX]}"
    
    # Skip accounts we've already tried that were rate limited
    if [[ " ${rate_limited_accounts[*]} " =~ " ${current_account} " ]]; then
      CURRENT_ACCOUNT_IDX=$(( (CURRENT_ACCOUNT_IDX + 1) % ${#GEMINI_ACCOUNTS[@]} ))
      continue
    fi
    
    echo "Attempt $attempt/$MAX_RETRIES using account: $current_account"
    
    # Switch to the account we want to try
    switch_gemini_account "$current_account"
    
    result="$(run_gemini_review "$diff")"
    exit_code=$?

    if [ $exit_code -eq 124 ]; then
      echo "Timeout on account $current_account, switching..."
      rate_limited_accounts+=("$current_account")
      CURRENT_ACCOUNT_IDX=$(( (CURRENT_ACCOUNT_IDX + 1) % ${#GEMINI_ACCOUNTS[@]} ))
      continue
    fi

    if is_rate_limited "$result"; then
      echo "Rate limited on account $current_account, switching..."
      rate_limited_accounts+=("$current_account")
      CURRENT_ACCOUNT_IDX=$(( (CURRENT_ACCOUNT_IDX + 1) % ${#GEMINI_ACCOUNTS[@]} ))
      sleep 1
      continue
    fi

    # Success or other error - don't retry
    break
  done

  if [ $exit_code -ne 0 ] && [ $exit_code -ne 124 ]; then
    # Check if result contains actual review content despite error code
    if echo "$result" | grep -qiE "(finding|issue|bug|security|suggestion|review|no issues)"; then
      echo "$result"
      echo "--- CODERO SECOND PASS END ---"
      exit 0
    fi
    echo "$result" >&2
    exit 1
  fi

  if [ $exit_code -eq 124 ]; then
    echo "Error: All Gemini accounts timed out after ${TIMEOUT_SEC}s each"
    exit 1
  fi

  echo "$result"
  echo "--- CODERO SECOND PASS END ---"
}

main "$@"
