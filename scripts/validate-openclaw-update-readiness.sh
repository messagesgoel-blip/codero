#!/usr/bin/env bash
# validate-openclaw-update-readiness.sh — Codero-local update readiness validation
# Part of TOOL-004 (shadow mode, read-only)
#
# KEEP_LOCAL: required for shipped runtime — validates update readiness for the Codero-local OpenClaw baseline shipped with this repo
# This script validates that the OpenClaw baseline is ready for controlled updates.
# It checks version metadata, validates no auto-update is enabled, and runs all
# baseline validators. It does NOT modify any state or configuration.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
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

log_info() {
    echo -e "${CYAN}INFO${NC}: $1"
}

check_config_exists() {
    if [ -f "$OPENCLAW_CONFIG" ]; then
        log_pass "OpenClaw config exists"
        return 0
    else
        log_fail "OpenClaw config missing ($OPENCLAW_CONFIG)"
        return 1
    fi
}

check_jq_available() {
    if command -v jq >/dev/null 2>&1; then
        return 0
    else
        log_fail "jq not available — cannot parse JSON config"
        return 1
    fi
}

check_version_documented() {
    local version
    version=$(jq -r '.meta.lastTouchedVersion // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null)

    if [ "$version" != "unknown" ] && [ -n "$version" ]; then
        log_pass "Version is documented: $version"
        echo "       Last touched: $(jq -r '.meta.lastTouchedAt // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null)"
    else
        log_fail "Version not documented in config"
    fi
}

check_no_auto_update() {
    if grep -qi 'auto.update\|autoupdate\|auto_update' "$OPENCLAW_CONFIG" 2>/dev/null; then
        log_fail "Auto-update configuration found (must be disabled)"
    else
        log_pass "No auto-update configuration found"
    fi
}

check_wizard_metadata() {
    local wizard_version
    wizard_version=$(jq -r '.wizard.lastRunVersion // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null)

    if [ "$wizard_version" != "unknown" ] && [ -n "$wizard_version" ]; then
        log_pass "Wizard metadata available: version $wizard_version"
        echo "       Last run: $(jq -r '.wizard.lastRunAt // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null)"
        echo "       Command: $(jq -r '.wizard.lastRunCommand // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null)"
    else
        log_warn "Wizard metadata incomplete"
    fi
}

run_baseline_validator() {
    local script="$1"
    local name="$2"

    if [ -x "$script" ]; then
        echo
        log_info "Running $name..."
        if "$script" >/dev/null 2>&1; then
            log_pass "$name passed"
        else
            log_fail "$name failed"
        fi
    else
        log_warn "$name script not found or not executable: $script"
    fi
}

check_pty_bridge() {
    if [ -x "$PTY_BRIDGE_BIN" ]; then
        log_pass "PTY bridge is executable"
    else
        log_fail "PTY bridge missing or not executable"
    fi
}

check_validators_exist() {
    for validator in validate-tooling-baseline.sh validate-openclaw-privileges.sh validate-openclaw-plugins.sh; do
        if [ -x "$SCRIPT_DIR/$validator" ]; then
            log_pass "Validator exists: $validator"
        else
            log_warn "Validator missing: $validator"
        fi
    done
}

echo "-----------------------------------------"
echo "OpenClaw Update Readiness Validation"
echo "Task: TOOL-004 (shadow mode)"
echo "Date: $(date -Iseconds)"
echo "Config: $OPENCLAW_CONFIG"
echo "-----------------------------------------"
echo

# Prerequisite checks
if ! check_config_exists; then
    echo -e "\n${RED}Cannot continue: config file not found${NC}"
    exit 1
fi

if ! check_jq_available; then
    echo -e "\n${RED}Cannot continue: jq required for JSON parsing${NC}"
    exit 1
fi

echo
echo "--- Version Metadata ---"
check_version_documented
check_wizard_metadata

echo
echo "--- Update Control ---"
check_no_auto_update

echo
echo "--- Validator Availability ---"
check_validators_exist

echo
echo "--- PTY Bridge ---"
check_pty_bridge

echo
echo "--- Running Baseline Validators ---"
run_baseline_validator "$SCRIPT_DIR/validate-tooling-baseline.sh" "Tooling baseline validator"
run_baseline_validator "$SCRIPT_DIR/validate-openclaw-privileges.sh" "Privilege profile validator"
run_baseline_validator "$SCRIPT_DIR/validate-openclaw-plugins.sh" "Plugin policy validator"

echo
echo "-----------------------------------------"
echo "Current Baseline Summary"
echo "-----------------------------------------"
echo "Version:     $(jq -r '.meta.lastTouchedVersion // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null)"
echo "Plugins:     $(jq -r '.plugins.entries | keys | join(", ")' "$OPENCLAW_CONFIG" 2>/dev/null)"
echo "Gateway:     $(jq -r '.gateway.bind // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null) / $(jq -r '.gateway.auth.mode // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null)"
echo "Provider:    $(jq -r '.models.providers.litellm.baseUrl // "unknown"' "$OPENCLAW_CONFIG" 2>/dev/null)"

echo
echo "-----------------------------------------"
echo "Validation Summary"
echo "-----------------------------------------"
echo -e "Passed: ${GREEN}$PASS${NC}"
echo -e "Failed: ${RED}$FAIL${NC}"
echo -e "Warnings: ${YELLOW}$WARN${NC}"
echo

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}Update readiness validation FAILED${NC}"
    echo "Address failures before proceeding with OpenClaw updates."
    exit 1
else
    echo -e "${GREEN}Update readiness validation PASSED${NC}"
    echo "Baseline is ready for controlled update review."
    if [ "$WARN" -gt 0 ]; then
        echo "Review warnings above for potential improvements."
    fi
    exit 0
fi
