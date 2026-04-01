#!/usr/bin/env bash
# validate-openclaw-plugins.sh — Codero-local OpenClaw plugin validation
# Part of TOOL-003 (shadow mode, read-only)
#
# KEEP_LOCAL: required for shipped runtime — validates the Codero-local OpenClaw plugin baseline used by the shipped adapter path
# This script validates that the OpenClaw plugin config matches the approved
# allowlist for Codero. It does NOT modify any state or configuration.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

PASS=0
FAIL=0
WARN=0

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

# Approved plugins for Codero baseline
APPROVED_PLUGINS=("litellm" "acpx")

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

check_jq_available() {
    if command -v jq >/dev/null 2>&1; then
        return 0
    else
        log_fail "jq not available — cannot parse JSON config"
        return 1
    fi
}

check_approved_plugin_enabled() {
    local plugin="$1"
    local enabled
    enabled=$(jq -r ".plugins.entries.${plugin}.enabled // false" "$OPENCLAW_CONFIG" 2>/dev/null)

    if [ "$enabled" = "true" ]; then
        log_pass "Approved plugin '$plugin' is enabled"
    else
        log_fail "Approved plugin '$plugin' is NOT enabled (required)"
    fi
}

check_no_unapproved_plugins() {
    local enabled_plugins
    enabled_plugins=$(jq -r '.plugins.entries | to_entries[] | select(.value.enabled == true) | .key' "$OPENCLAW_CONFIG" 2>/dev/null)

    local unapproved_found=false

    while IFS= read -r plugin; do
        [ -z "$plugin" ] && continue

        local is_approved=false
        for approved in "${APPROVED_PLUGINS[@]}"; do
            if [ "$plugin" = "$approved" ]; then
                is_approved=true
                break
            fi
        done

        if [ "$is_approved" = false ]; then
            log_fail "Unapproved plugin '$plugin' is enabled"
            unapproved_found=true
        fi
    done <<< "$enabled_plugins"

    if [ "$unapproved_found" = false ]; then
        log_pass "No unapproved plugins enabled"
    fi
}

check_plugin_count() {
    local count
    count=$(jq '[.plugins.entries | to_entries[] | select(.value.enabled == true)] | length' "$OPENCLAW_CONFIG" 2>/dev/null)

    if [ "$count" -eq 2 ]; then
        log_pass "Exactly 2 plugins enabled (matches baseline)"
    elif [ "$count" -lt 2 ]; then
        log_warn "$count plugins enabled (fewer than baseline)"
    else
        log_fail "$count plugins enabled (more than approved baseline of 2)"
    fi
}

check_no_forbidden_creds_in_plugins() {
    local plugin_configs
    plugin_configs=$(jq -r '.plugins.entries | to_entries[] | .value.config // {}' "$OPENCLAW_CONFIG" 2>/dev/null)

    if echo "$plugin_configs" | grep -qE 'GITHUB_TOKEN|CODERO_DB_PATH|CODERO_REDIS'; then
        log_fail "Forbidden credentials found in plugin configs"
    else
        log_pass "No forbidden credentials in plugin configs"
    fi
}

check_litellm_config() {
    local litellm_config
    litellm_config=$(jq -r '.plugins.entries.litellm.config // {}' "$OPENCLAW_CONFIG" 2>/dev/null)

    # litellm config should be empty or minimal
    if [ "$litellm_config" = "{}" ] || [ -z "$litellm_config" ]; then
        log_pass "litellm plugin config is minimal"
    else
        log_warn "litellm plugin has non-empty config — review for privilege scope"
    fi
}

check_acpx_config() {
    local acpx_config
    acpx_config=$(jq -r '.plugins.entries.acpx.config // {}' "$OPENCLAW_CONFIG" 2>/dev/null)

    # acpx config should be empty or minimal
    if [ "$acpx_config" = "{}" ] || [ -z "$acpx_config" ]; then
        log_pass "acpx plugin config is minimal"
    else
        log_warn "acpx plugin has non-empty config — review for privilege scope"
    fi
}

check_provider_localhost() {
    local base_url
    base_url=$(jq -r '.models.providers.litellm.baseUrl // ""' "$OPENCLAW_CONFIG" 2>/dev/null)

    if [[ "$base_url" == "http://localhost"* ]]; then
        log_pass "LiteLLM provider uses localhost endpoint ($base_url)"
    elif [ -z "$base_url" ]; then
        log_warn "No LiteLLM baseUrl configured"
    else
        log_warn "LiteLLM provider does not use localhost: $base_url"
    fi
}

echo "-----------------------------------------"
echo "OpenClaw Plugin Policy Validation"
echo "Task: TOOL-003 (shadow mode)"
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
echo "--- Approved Plugins ---"
for plugin in "${APPROVED_PLUGINS[@]}"; do
    check_approved_plugin_enabled "$plugin"
done

echo
echo "--- Plugin Allowlist Compliance ---"
check_no_unapproved_plugins
check_plugin_count

echo
echo "--- Plugin Privilege Scope ---"
check_no_forbidden_creds_in_plugins
check_litellm_config
check_acpx_config
check_provider_localhost

echo
echo "-----------------------------------------"
echo "Summary"
echo "-----------------------------------------"
echo -e "Passed: ${GREEN}$PASS${NC}"
echo -e "Failed: ${RED}$FAIL${NC}"
echo -e "Warnings: ${YELLOW}$WARN${NC}"
echo
echo "Approved plugins: ${APPROVED_PLUGINS[*]}"
echo

if [ "$FAIL" -gt 0 ]; then
    echo -e "${RED}Plugin policy validation FAILED${NC}"
    echo "Review the failures above — some may indicate policy violations."
    exit 1
else
    echo -e "${GREEN}Plugin policy validation PASSED${NC}"
    if [ "$WARN" -gt 0 ]; then
        echo "Review warnings above for potential improvements."
    fi
    exit 0
fi
