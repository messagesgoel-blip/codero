#!/usr/bin/env bash
set -euo pipefail

# Health Check for Pre-Commit Review Gate
# Verifies all gates are operational

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

echo "========================================"
echo "PRE-COMMIT REVIEW GATE HEALTH CHECK"
echo "========================================"
echo ""

passes=0
failures=0

# Check tool availability
echo "=== Tool Availability ==="
required_tools=(semgrep openai)
for tool in semgrep aider copilot coderabbit openai; do
  if command -v "$tool" >/dev/null 2>&1; then
    echo "✓ $tool: installed"
  else
    if [[ " ${required_tools[*]} " =~ " $tool " ]]; then
      echo "✗ $tool: NOT INSTALLED (required)"
      failures=$((failures + 1))
    else
      echo "⚠ $tool: not installed (optional)"
    fi
  fi
done
echo ""

# Detect timeout command (support GNU coreutils on macOS)
TIMEOUT_CMD=""
if command -v timeout >/dev/null 2>&1; then
  TIMEOUT_CMD="timeout"
elif command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_CMD="gtimeout"
else
  echo "✗ timeout utility not found (install coreutils)" >&2
  failures=$((failures + 1))
fi

# Check API keys in .env
# Discover .env location using same logic as two-pass-review.sh
env_file=""
if [ -n "${CODERO_ENV_FILE:-}" ] && [ -f "${CODERO_ENV_FILE}" ]; then
  env_file="${CODERO_ENV_FILE}"
elif [ -f "$REPO_ROOT/.env" ]; then
  env_file="$REPO_ROOT/.env"
else
  common_dir="$(git -C "$REPO_ROOT" rev-parse --git-common-dir 2>/dev/null || true)"
  if [ -n "$common_dir" ]; then
    repo_root_env="$(cd "$REPO_ROOT" && cd "$common_dir/.." 2>/dev/null && pwd)/.env"
    if [ -f "$repo_root_env" ]; then
      env_file="$repo_root_env"
    fi
  fi
fi

echo "=== API Keys ==="
# Backend/provider credentials (any one required)
provider_keys=(CODERO_AIDER_GEMINI_API_KEY CODERO_GEMINI_SECOND_PASS_API_KEY GEMINI_API_KEY OPENROUTER_API_KEY MINIMAX_API_KEY LITELLM_MASTER_KEY LITELLM_API_KEY OPENAI_API_KEY)
provider_found=false
for key in "${provider_keys[@]}"; do
  if [ -n "${!key-}" ]; then
    echo "✓ $key: set (via environment)"
    provider_found=true
    continue
  fi
  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    file_val="$(grep -E "^${key}=" "$env_file" 2>/dev/null | head -n 1 | cut -d= -f2- || true)"
    if [ -n "$file_val" ]; then
      echo "✓ $key: set (${#file_val} chars)"
      provider_found=true
      continue
    fi
  fi
  echo "- $key: not set"
done

# GitHub authentication (any token required)
github_keys=(GH_TOKEN GITHUB_TOKEN CODERO_GITHUB_TOKEN)
github_found=false
for key in "${github_keys[@]}"; do
  if [ -n "${!key-}" ]; then
    echo "✓ $key: set (via environment)"
    github_found=true
    continue
  fi
  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    file_val="$(grep -E "^${key}=" "$env_file" 2>/dev/null | head -n 1 | cut -d= -f2- || true)"
    if [ -n "$file_val" ]; then
      echo "✓ $key: set (${#file_val} chars)"
      github_found=true
      continue
    fi
  fi
  echo "- $key: not set"
done

if [ "$provider_found" = false ]; then
  echo "✗ No provider backend keys found"
  failures=$((failures + 1))
fi
if [ "$github_found" = false ]; then
  echo "✗ No GitHub auth keys found"
  failures=$((failures + 1))
fi
echo ""
# Check scripts
echo "=== Scripts ==="
for script in scripts/review/two-pass-review.sh scripts/review/semgrep-zero-pass.sh scripts/review/copilot-third-pass.sh scripts/review/aider-first-pass.sh scripts/review/gemini-second-pass.sh scripts/review/pr-agent-second-pass.sh scripts/review/coderabbit-second-pass.sh; do
  if [ -x "$script" ]; then
    echo "✓ $script: executable"
  else
    echo "✗ $script: missing or not executable"
    failures=$((failures + 1))
  fi
done
echo ""

# Check pre-commit hook (support worktrees via git-path)
echo "=== Pre-commit Hook ==="
hook_path="$(git rev-parse --git-path hooks/pre-commit 2>/dev/null || echo ".git/hooks/pre-commit")"
if [ -n "$hook_path" ] && [ -x "$hook_path" ]; then
  echo "✓ pre-commit hook: executable ($hook_path)"
else
  echo "✗ pre-commit hook: missing or not executable"
  failures=$((failures + 1))
fi
echo ""

# Quick gate test (no diff)
echo "=== Quick Gate Test (empty diff) ==="

# Helper to probe a gate script
probe_gate() {
  local script="$1"
  local name="$2"
  if [ -n "$TIMEOUT_CMD" ]; then
    local output
    output="$("$TIMEOUT_CMD" 10 bash "$script" 2>&1 || true)"
    if echo "$output" | grep -qiE "No uncommitted changes|No staged changes|No staged files|No changes|skipped|PASSED"; then
      echo "✓ $name gate: responds correctly to empty diff"
      passes=$((passes + 1))
    elif [ "$name" = "coderabbit" ] && echo "$output" | grep -qiE "Authentication required|auth login"; then
      echo "⚠ $name gate: not authenticated (run 'coderabbit auth login')"
    else
      echo "⚠ $name gate: may have issues (output: $(echo "$output" | tail -1 | cut -c1-50))"
      failures=$((failures + 1))
    fi
  else
    echo "⚠ Skipping $name gate probe (timeout utility missing)"
  fi
}

# Probe gates
probe_gate "scripts/review/semgrep-zero-pass.sh" "Semgrep"
probe_gate "scripts/review/copilot-third-pass.sh" "Copilot"
probe_gate "scripts/review/aider-first-pass.sh" "Aider"
probe_gate "scripts/review/gemini-second-pass.sh" "Gemini"
probe_gate "scripts/review/pr-agent-second-pass.sh" "pr-agent"
probe_gate "scripts/review/coderabbit-second-pass.sh" "coderabbit"

# Summary
echo "========================================"
echo "SUMMARY"
echo "========================================"
echo "Tools: checked"
echo "API Keys: checked"
echo "Scripts: checked"
echo "Quick tests: $passes passed"
echo ""

if [ $failures -eq 0 ]; then
  echo "✓ All checks passed - Review gate is healthy"
  exit 0
else
  echo "✗ $failures issue(s) found - Review gate needs attention"
  exit 1
fi
