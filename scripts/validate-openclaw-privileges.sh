#!/usr/bin/env bash
# validate-openclaw-privileges.sh — Codero-local OpenClaw privilege validation
# Part of TOOL-002 (shadow mode, read-only)
#
# KEEP_LOCAL: required for shipped runtime — validates the Codero-local OpenClaw privilege baseline used by the shipped adapter path
# This script validates that the OpenClaw config matches the intended privilege
# profile for Codero. It does NOT modify any state or configuration.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

PASS=0
FAIL=0
WARN=0

STORAGE_SHARED_ROOT="${CODERO_STORAGE_SHARED_ROOT:-/srv/storage/shared}"
PTY_BRIDGE_BIN="${AGENT_TMUX_BRIDGE_BIN:-$STORAGE_SHARED_ROOT/tools/bin/agent-tmux-bridge}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_OWNER="${CODERO_OPERATOR_USER:-$(stat -c %U "$SCRIPT_DIR" 2>/dev/null || true)}"
OWNER_HOME="$(getent passwd "$REPO_OWNER" 2>/dev/null | cut -d: -f6 || true)"
DEFAULT_OPENCLAW_CONFIG="${OPENCLAW_CONFIG_PATH:-}"
if [ -z "$DEFAULT_OPENCLAW_CONFIG" ] && [ -n "$OWNER_HOME" ]; then
    DEFAULT_OPENCLAW_CONFIG="$OWNER_HOME/.openclaw-codero-smoke/openclaw.json"
fi
if [ -z "$DEFAULT_OPENCLAW_CONFIG" ]; then
    DEFAULT_OPENCLAW_CONFIG="$HOME/.openclaw-codero-smoke/openclaw.json"
fi
OPENCLAW_CONFIG="${OPENCLAW_CONFIG:-$DEFAULT_OPENCLAW_CONFIG}"

log_pass() {
    echo -e "${GREEN}PASS${NC}: $1"
    ((PASS++))
}

log_fail() {
    echo -e "${RED}FAIL${NC}: $1"
    ((FAIL++))
}

log_warn() {
    echo -e "${YELLOW}WARN${NC}: $1"
    ((WARN++))
}

check_config_exists() {
    if [ -f "$OPENCLAW_CONFIG" ]; then
        log_pass "OpenClaw config exists ($OPENCLAW_CONFIG)"
        return 0
    else
        log_fail "OpenClaw config missing ($OPENCLAW_CONFIG)"
        return 1
    fi
}

check_gateway_loopback() {
    # Shadow-mode heuristic: grep keeps this validator read-only and dependency-light.
    # If this moves from documentation validation to enforcement, replace with jq parsing.
    if grep -q '"bind": "loopback"' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_pass "Gateway bind is loopback-only"
    else
        log_fail "Gateway bind is NOT loopback-only (security risk)"
    fi
}

check_gateway_auth() {
    # Shadow-mode heuristic: grep is acceptable for the current config shape only.
    # Harden this with jq before using the result as an enforcement signal.
    if grep -q '"mode": "token"' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_pass "Gateway auth is token-based"
    else
        log_warn "Gateway auth mode is not token-based"
    fi
}

check_no_github_token() {
    if grep -q 'GITHUB_TOKEN' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_fail "GITHUB_TOKEN found in config (forbidden)"
    else
        log_pass "No GITHUB_TOKEN in config"
    fi
}

check_no_db_path() {
    if grep -q 'CODERO_DB_PATH' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_fail "CODERO_DB_PATH found in config (forbidden)"
    else
        log_pass "No CODERO_DB_PATH in config"
    fi
}

check_no_redis_creds() {
    if grep -q 'CODERO_REDIS' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_fail "CODERO_REDIS credentials found in config (forbidden)"
    else
        log_pass "No CODERO_REDIS credentials in config"
    fi
}

check_approved_plugins() {
    local plugins_ok=true

    # Check that litellm and acpx are present (approved)
    if ! grep -q '"litellm":' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_warn "Approved plugin 'litellm' not found"
        plugins_ok=false
    fi

    if ! grep -q '"acpx":' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_warn "Approved plugin 'acpx' not found"
        plugins_ok=false
    fi

    # Count enabled plugins (rough check)
    local plugin_count
    plugin_count=$(grep -c '"enabled": true' "$OPENCLAW_CONFIG" 2>/dev/null || echo 0)

    if [ "$plugin_count" -gt 2 ]; then
        log_warn "More than 2 plugins enabled ($plugin_count) — review allowlist"
        plugins_ok=false
    fi

    if [ "$plugins_ok" = true ]; then
        log_pass "Plugin configuration matches approved allowlist"
    fi
}

check_workspace_isolated() {
    if grep -q '"workspace":' "$OPENCLAW_CONFIG" 2>/dev/null; then
        local workspace
        workspace=$(grep -o '"workspace": "[^"]*"' "$OPENCLAW_CONFIG" | head -1 | cut -d'"' -f4)
        if [[ "$workspace" == *".openclaw"* ]] || [[ "$workspace" == *"openclaw-codero"* ]]; then
            log_pass "Workspace is isolated under OpenClaw state root ($workspace)"
        else
            log_warn "Workspace path may not be isolated: $workspace"
        fi
    else
        log_warn "No workspace path found in config"
    fi
}

check_pty_bridge() {
    if [ -x "$PTY_BRIDGE_BIN" ]; then
        log_pass "Shared PTY bridge is executable"
    else
        log_fail "Shared PTY bridge missing or not executable"
    fi
}

check_provider_localhost() {
    if grep -q '"baseUrl": "http://localhost' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_pass "Model provider uses localhost endpoint"
    else
        log_warn "Model provider may not use localhost — verify network posture"
    fi
}

echo "-----------------------------------------"
echo "OpenClaw Privilege Profile Validation"
echo "Task: TOOL-002 (shadow mode)"
echo "Date: $(date -Iseconds)"
echo "Config: $OPENCLAW_CONFIG"
echo "-----------------------------------------"
echo

# Must check config exists first
if ! check_config_exists; then
    echo
    echo -e "${RED}Cannot continue: config file not found${NC}"
    exit 1
fi

echo
echo "--- Gateway Security ---"
check_gateway_loopback
check_gateway_auth

echo
echo "--- Forbidden Credentials ---"
check_no_github_token
check_no_db_path
check_no_redis_creds

echo
echo "--- Plugin Policy ---"
check_approved_plugins

echo
echo "--- Isolation ---"
check_workspace_isolated
check_provider_localhost

echo
echo "--- PTY Transport ---"
check_pty_bridge

echo
echo "-----------------------------------------"
echo "Summary"
echo "-----------------------------------------"
echo -e "Passed: ${GREEN}$PASS${NC}"
echo -e "Failed: ${RED}$FAIL${NC}"
echo -e "Warnings: ${YELLOW}$WARN${NC}"
echo

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}Privilege validation FAILED${NC}"
    echo "Review the failures above — some may indicate security risks."
    exit 1
else
    echo -e "${GREEN}Privilege validation PASSED${NC}"
    if [ "$WARN" -gt 0 ]; then
        echo "Review warnings above for potential improvements."
    fi
    exit 0
fi
