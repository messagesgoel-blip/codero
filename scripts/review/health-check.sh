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
usable_ai_gates=0
MIN_AI_QUORUM="${CODERO_MIN_SUCCESSFUL_AI_GATES:-2}"

# Check tool availability
echo "=== Tool Availability ==="
required_tools=(semgrep)
checked_tools=(semgrep openai aider copilot pr-agent coderabbit)

is_required_tool() {
  local tool="$1"
  local req
  for req in "${required_tools[@]}"; do
    if [ "$req" = "$tool" ]; then
      return 0
    fi
  done
  return 1
}

has_tool() {
  command -v "$1" >/dev/null 2>&1
}

for tool in "${checked_tools[@]}"; do
  if has_tool "$tool"; then
    echo "✓ $tool: installed"
  else
    if is_required_tool "$tool"; then
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

key_is_set() {
  local key="$1"
  if [ -n "${!key-}" ]; then
    return 0
  fi
  if [ -n "$env_file" ] && [ -f "$env_file" ]; then
    local file_val
    file_val="$(grep -E "^${key}=" "$env_file" 2>/dev/null | head -n 1 | cut -d= -f2- || true)"
    [ -n "$file_val" ] && return 0
  fi
  return 1
}

echo "=== API Keys ==="
# Backend/provider credentials (any one required)
provider_keys=(CODERO_AIDER_GEMINI_API_KEY CODERO_GEMINI_SECOND_PASS_API_KEY GEMINI_API_KEY OPENROUTER_API_KEY MINIMAX_API_KEY LITELLM_MASTER_KEY LITELLM_API_KEY OPENAI_API_KEY)
provider_found=false
for key in "${provider_keys[@]}"; do
  if key_is_set "$key"; then
    if [ -n "${!key-}" ]; then
      echo "✓ $key: set (via environment)"
    else
      file_val="$(grep -E "^${key}=" "$env_file" 2>/dev/null | head -n 1 | cut -d= -f2- || true)"
      echo "✓ $key: set (${#file_val} chars)"
    fi
    provider_found=true
  else
    echo "- $key: not set"
  fi
done

# GitHub authentication (any token required)
github_keys=(GH_TOKEN GITHUB_TOKEN CODERO_GITHUB_TOKEN)
github_found=false
for key in "${github_keys[@]}"; do
  if key_is_set "$key"; then
    if [ -n "${!key-}" ]; then
      echo "✓ $key: set (via environment)"
    else
      file_val="$(grep -E "^${key}=" "$env_file" 2>/dev/null | head -n 1 | cut -d= -f2- || true)"
      echo "✓ $key: set (${#file_val} chars)"
    fi
    github_found=true
  else
    echo "- $key: not set"
  fi
done

if [ "$provider_found" = false ]; then
  echo "⚠ No provider backend keys found (some AI gates may be unavailable)"
fi
if [ "$github_found" = false ]; then
  echo "⚠ No GitHub auth keys found (pr-agent gate may be unavailable)"
fi
echo ""

# Check scripts
echo "=== Scripts ==="
required_scripts=(scripts/review/two-pass-review.sh scripts/review/semgrep-zero-pass.sh)
optional_ai_scripts=(scripts/review/copilot-third-pass.sh scripts/review/aider-first-pass.sh scripts/review/gemini-second-pass.sh scripts/review/pr-agent-second-pass.sh scripts/review/coderabbit-second-pass.sh)
for script in "${required_scripts[@]}"; do
  if [ -x "$script" ]; then
    echo "✓ $script: executable"
  else
    echo "✗ $script: missing or not executable"
    failures=$((failures + 1))
  fi
done
for script in "${optional_ai_scripts[@]}"; do
  if [ -x "$script" ]; then
    echo "✓ $script: executable"
  else
    echo "⚠ $script: missing or not executable (optional)"
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

echo "=== AI Gate Readiness ==="
if [ -x "scripts/review/copilot-third-pass.sh" ] && (has_tool copilot || (has_tool openai && key_is_set OPENAI_API_KEY)); then
  echo "✓ Copilot gate: usable"
  usable_ai_gates=$((usable_ai_gates + 1))
else
  echo "⚠ Copilot gate: unavailable (need copilot CLI, or openai CLI + OPENAI_API_KEY)"
fi

if [ -x "scripts/review/aider-first-pass.sh" ] && has_tool aider && [ "$provider_found" = true ]; then
  echo "✓ Aider gate: usable"
  usable_ai_gates=$((usable_ai_gates + 1))
else
  echo "⚠ Aider gate: unavailable (needs aider + provider key)"
fi

if [ -x "scripts/review/gemini-second-pass.sh" ] && has_tool curl && has_tool jq && [ "$provider_found" = true ]; then
  echo "✓ Gemini gate: usable"
  usable_ai_gates=$((usable_ai_gates + 1))
else
  echo "⚠ Gemini gate: unavailable (needs curl + jq + provider key)"
fi

if [ -x "scripts/review/pr-agent-second-pass.sh" ] && has_tool pr-agent && [ "$provider_found" = true ] && [ "$github_found" = true ]; then
  echo "✓ pr-agent gate: usable"
  usable_ai_gates=$((usable_ai_gates + 1))
else
  echo "⚠ pr-agent gate: unavailable (needs pr-agent + provider key + GitHub token)"
fi

if [ -x "scripts/review/coderabbit-second-pass.sh" ] && has_tool coderabbit; then
  echo "✓ coderabbit gate: usable"
  usable_ai_gates=$((usable_ai_gates + 1))
else
  echo "⚠ coderabbit gate: unavailable (needs coderabbit CLI)"
fi

echo "AI gates usable: $usable_ai_gates (minimum required: $MIN_AI_QUORUM)"
if [ "$usable_ai_gates" -lt "$MIN_AI_QUORUM" ]; then
  echo "✗ AI quorum not met for pre-commit gate"
  failures=$((failures + 1))
fi
echo ""

# Quick gate test (no diff)
echo "=== Quick Gate Test (empty diff) ==="

# Helper to probe a gate script
probe_gate() {
  local script="$1"
  local name="$2"
  local tool="${3:-}"
  local gate_required=false
  if [ -n "$TIMEOUT_CMD" ]; then
    if [ -n "$tool" ] && is_required_tool "$tool"; then
      gate_required=true
    fi

    if [ -n "$tool" ] && ! has_tool "$tool"; then
      if [ "$gate_required" = true ]; then
        echo "✗ $name gate probe skipped: required tool '$tool' is missing"
        failures=$((failures + 1))
      else
        echo "⚠ $name gate probe skipped: optional tool '$tool' is missing"
      fi
      return
    fi

    local tmp_index
    tmp_index="$(mktemp "${TMPDIR:-/tmp}/codero-health-index.XXXXXX")"
    rm -f "$tmp_index"

    if git -C "$REPO_ROOT" rev-parse --verify HEAD >/dev/null 2>&1; then
      GIT_INDEX_FILE="$tmp_index" git -C "$REPO_ROOT" read-tree HEAD
    else
      GIT_INDEX_FILE="$tmp_index" git -C "$REPO_ROOT" read-tree --empty
    fi

    local output
    output="$(GIT_INDEX_FILE="$tmp_index" CODERO_REPO_PATH="$REPO_ROOT" "$TIMEOUT_CMD" 10 bash "$script" 2>&1 || true)"
    rm -f "$tmp_index"

    if echo "$output" | grep -qiE "No uncommitted changes|No staged changes|No staged files|No changes|skipped|PASSED"; then
      echo "✓ $name gate: responds correctly to empty diff"
      passes=$((passes + 1))
    elif [ "$name" = "coderabbit" ] && echo "$output" | grep -qiE "Authentication required|auth login"; then
      echo "⚠ $name gate: not authenticated (run 'coderabbit auth login')"
    else
      echo "⚠ $name gate: may have issues (output: $(echo "$output" | tail -1 | cut -c1-50))"
      if [ "$gate_required" = true ]; then
        failures=$((failures + 1))
      fi
    fi
  else
    echo "⚠ Skipping $name gate probe (timeout utility missing)"
  fi
}

# Probe gates
probe_gate "scripts/review/semgrep-zero-pass.sh" "Semgrep" "semgrep"
probe_gate "scripts/review/copilot-third-pass.sh" "Copilot" "copilot"
probe_gate "scripts/review/aider-first-pass.sh" "Aider" "aider"
probe_gate "scripts/review/gemini-second-pass.sh" "Gemini" ""
probe_gate "scripts/review/pr-agent-second-pass.sh" "pr-agent" "pr-agent"
probe_gate "scripts/review/coderabbit-second-pass.sh" "coderabbit" "coderabbit"

# Summary
echo "========================================"
echo "SUMMARY"
echo "========================================"
echo "Tools: checked"
echo "API Keys: checked"
echo "Scripts: checked"
echo "AI readiness: $usable_ai_gates/$MIN_AI_QUORUM"
echo "Quick tests: $passes passed"
echo ""

if [ $failures -eq 0 ]; then
  echo "✓ All checks passed - Review gate is healthy"
  exit 0
else
  echo "✗ $failures issue(s) found - Review gate needs attention"
  exit 1
fi
