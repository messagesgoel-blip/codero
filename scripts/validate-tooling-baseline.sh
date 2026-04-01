#!/usr/bin/env bash
# validate-tooling-baseline.sh — Codero-local baseline validation
# Part of TOOL-001 (shadow mode, read-only)
#
# KEEP_LOCAL: required for shipped runtime — validates the Codero-local shared tooling baseline used by the shipped adapter path
# This script validates that all shared tooling paths Codero depends on exist.
# It does NOT modify any state or shared tooling.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

PASS=0
FAIL=0
WARN=0

STORAGE_SHARED_ROOT="${CODERO_STORAGE_SHARED_ROOT:-/srv/storage/shared}"
COMMON_SHARED_ROOT="${CODERO_COMMON_SHARED_ROOT:-$(dirname "$(dirname "$STORAGE_SHARED_ROOT")")/shared}"
SHARED_ENV_BOOTSTRAP="${CODERO_SHARED_ENV_BOOTSTRAP:-$COMMON_SHARED_ROOT/agent-env.sh}"
SHARED_TOOL_BIN="${CODERO_SHARED_TOOL_BIN:-$STORAGE_SHARED_ROOT/tools/bin}"
SHARED_TOOL_VENVS="${CODERO_SHARED_TOOL_VENVS:-$STORAGE_SHARED_ROOT/tools/venvs}"
SHARED_TOOLKIT_BIN="${CODERO_SHARED_TOOLKIT_BIN:-$STORAGE_SHARED_ROOT/agent-toolkit/bin}"
SHARED_MEMORY_ROOT="${CODERO_SHARED_MEMORY_ROOT:-$STORAGE_SHARED_ROOT/memory}"
SHARED_GO_MOD_CACHE="${CODERO_SHARED_GO_MOD_CACHE:-$COMMON_SHARED_ROOT/go-mod-cache}"
SHARED_PIP_CACHE="${CODERO_SHARED_PIP_CACHE:-$COMMON_SHARED_ROOT/pip-cache}"
SHARED_NPM_CACHE="${CODERO_SHARED_NPM_CACHE:-$COMMON_SHARED_ROOT/npm-cache}"
SHARED_SEMGREP_CACHE="${CODERO_SHARED_SEMGREP_CACHE:-$COMMON_SHARED_ROOT/semgrep-cache}"
SHARED_PLAYWRIGHT_ROOT="${CODERO_SHARED_PLAYWRIGHT_ROOT:-$COMMON_SHARED_ROOT/playwright}"

check_exists() {
    local path="$1"
    local desc="$2"
    local mandatory="${3:-yes}"

    if [ -e "$path" ]; then
        if [ "$mandatory" = "yes" ]; then
            echo -e "${GREEN}PASS${NC}: $desc ($path)"
            ((PASS++))
        else
            echo -e "${GREEN}PASS${NC}: $desc ($path) [optional]"
            ((PASS++))
        fi
        return 0
    else
        if [ "$mandatory" = "yes" ]; then
            echo -e "${RED}FAIL${NC}: $desc ($path)"
            ((FAIL++))
        else
            echo -e "${YELLOW}WARN${NC}: $desc ($path) [optional, missing]"
            ((WARN++))
        fi
        return 1
    fi
}

check_executable() {
    local path="$1"
    local desc="$2"
    local mandatory="${3:-yes}"

    if [ -x "$path" ]; then
        echo -e "${GREEN}PASS${NC}: $desc is executable ($path)"
        ((PASS++))
        return 0
    elif [ -e "$path" ]; then
        if [ "$mandatory" = "yes" ]; then
            echo -e "${RED}FAIL${NC}: $desc exists but not executable ($path)"
            ((FAIL++))
        else
            echo -e "${YELLOW}WARN${NC}: $desc exists but not executable ($path) [optional]"
            ((WARN++))
        fi
        return 1
    else
        if [ "$mandatory" = "yes" ]; then
            echo -e "${RED}FAIL${NC}: $desc does not exist ($path)"
            ((FAIL++))
        else
            echo -e "${YELLOW}WARN${NC}: $desc does not exist ($path) [optional, missing]"
            ((WARN++))
        fi
        return 1
    fi
}

echo "-----------------------------------------"
echo "Codero Tooling Baseline Validation"
echo "Task: TOOL-001 (shadow mode)"
echo "Date: $(date -Iseconds)"
echo "-----------------------------------------"
echo

echo "--- Shared Env Bootstrap ---"
check_exists "$SHARED_ENV_BOOTSTRAP" "agent-env.sh" yes || true
echo

echo "--- Shared Tool Directories ---"
check_exists "$SHARED_TOOL_BIN" "shared tools bin" yes || true
check_exists "$SHARED_TOOL_VENVS" "shared venvs" yes || true
check_exists "$SHARED_TOOLKIT_BIN" "agent-toolkit bin" yes || true
echo

echo "--- Mandatory Shared Binaries ---"
check_executable "$SHARED_TOOL_BIN/agent-tmux-bridge" "PTY bridge" || true
check_executable "$SHARED_TOOLKIT_BIN/gate-heartbeat" "gate-heartbeat" || true
check_executable "$SHARED_TOOLKIT_BIN/codero-finish.sh" "codero-finish.sh" || true
echo

echo "--- Optional Shared Binaries ---"
check_executable "$SHARED_TOOLKIT_BIN/install-hooks" "install-hooks" no || true
check_executable "$SHARED_TOOLKIT_BIN/ci-watch.sh" "ci-watch.sh" no || true
echo

echo "--- Shared Caches ---"
check_exists "$SHARED_GO_MOD_CACHE" "Go module cache" yes || true
check_exists "$SHARED_PIP_CACHE" "pip cache" no || true
check_exists "$SHARED_NPM_CACHE" "npm cache" no || true
check_exists "$SHARED_SEMGREP_CACHE" "semgrep cache" no || true
echo

echo "--- Shared Browsers ---"
check_exists "$SHARED_PLAYWRIGHT_ROOT" "Playwright browsers" no || true
echo

echo "--- Shared Memory ---"
check_exists "$SHARED_MEMORY_ROOT/MEMORY.md" "shared memory" yes || true
check_exists "$SHARED_MEMORY_ROOT/OPENCLAW-PTY-NOTES.md" "OpenClaw PTY notes" yes || true
echo

echo "--- Shared Venvs ---"
check_exists "$SHARED_TOOL_VENVS/aider" "aider venv" no || true
check_exists "$SHARED_TOOL_VENVS/pr-agent" "pr-agent venv" no || true
check_exists "$SHARED_TOOL_VENVS/tooling" "tooling venv" no || true
echo

echo "-----------------------------------------"
echo "Summary"
echo "-----------------------------------------"
echo -e "Passed: ${GREEN}$PASS${NC}"
echo -e "Failed: ${RED}$FAIL${NC}"
echo -e "Warnings: ${YELLOW}$WARN${NC}"
echo

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}Baseline validation FAILED${NC}"
    exit 1
else
    echo -e "${GREEN}Baseline validation PASSED${NC}"
    exit 0
fi
