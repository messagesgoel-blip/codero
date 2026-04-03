#!/usr/bin/env bash
# KEEP_LOCAL: required for shipped runtime — dev/live deploy is tied to local worktree layout
# deploy-dev.sh — Build and restart both dev and live Codero containers,
# then update the local CLI binary. Used during active development to keep
# all three surfaces (dev daemon, live daemon, CLI) in sync with the worktree.
#
# Usage:
#   ./scripts/deploy-dev.sh          # build + restart + update CLI
#   ./scripts/deploy-dev.sh --build  # just build the image, don't restart
#   ./scripts/deploy-dev.sh --cli    # just update the CLI binary
#
# Both containers build from the same worktree at:
#   /srv/storage/repo/codero/.worktrees/main
#
# Dev:  docker-compose.yml  → codero-dev  → 127.0.0.1:8110
# Live: $CODERO_LIVE_COMPOSE_DIR/docker-compose.yml → codero-live → 127.0.0.1:8111

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIVE_COMPOSE_DIR="${CODERO_LIVE_COMPOSE_DIR:-/srv/storage/repo/codero/.deploy/live}"
CLI_DEST="${CODERO_CLI_DEST:-${HOME}/.local/bin/codero}"
SHARED_DEST="/srv/storage/shared/tools/bin/codero"
HEALTH_TIMEOUT=30

red()   { printf '\033[0;31m%s\033[0m\n' "$*"; }
green() { printf '\033[0;32m%s\033[0m\n' "$*"; }
dim()   { printf '\033[0;90m%s\033[0m\n' "$*"; }

die() { red "FATAL: $*" >&2; exit 1; }

wait_healthy() {
    local url="$1" name="$2" timeout="$3"
    local elapsed=0
    sleep 2  # give container a moment to start
    while (( elapsed < timeout )); do
        if curl -sf "$url" >/dev/null 2>&1; then
            local ver
            ver=$(curl -sf "$url" | python3 -c "import json,sys; print(json.load(sys.stdin).get('version','?'))" 2>/dev/null || echo "?")
            green "  $name healthy (version: $ver, ${elapsed}s)"
            return 0
        fi
        sleep 1
        (( elapsed++ ))
    done
    red "  $name failed to become healthy within ${timeout}s"
    return 1
}

build_cli() {
    dim "Building CLI binary..."
    (cd "$REPO_ROOT" && go build -buildvcs=false -o ./bin/codero ./cmd/codero) \
        || die "go build failed"

    # Update shared tools binary (fail if copy fails)
    install -D -m 0755 "$REPO_ROOT/bin/codero" "$SHARED_DEST" \
        || die "failed to update shared tools binary at $SHARED_DEST"

    # Update user binary (atomic rename to handle "text file busy")
    if [ -f "$CLI_DEST" ]; then
        cp "$REPO_ROOT/bin/codero" "${CLI_DEST}.new"
        mv "${CLI_DEST}.new" "$CLI_DEST"
    else
        cp "$REPO_ROOT/bin/codero" "$CLI_DEST"
    fi
    green "CLI binary updated"
}

build_containers() {
    dim "Building dev container..."
    (cd "$REPO_ROOT" && docker compose build codero 2>&1 | tail -2) \
        || die "dev build failed"

    dim "Building live container..."
    (cd "$LIVE_COMPOSE_DIR" && docker compose build codero 2>&1 | tail -2) \
        || die "live build failed"

    green "Both images built"
}

restart_containers() {
    dim "Restarting dev container..."
    (cd "$REPO_ROOT" && docker compose up -d codero 2>&1 | { grep -v "^$" || true; }) \
        || die "dev restart failed"

    dim "Restarting live container..."
    (cd "$LIVE_COMPOSE_DIR" && docker compose up -d codero 2>&1 | { grep -v "^$" || true; }) \
        || die "live restart failed"

    dim "Waiting for health..."
    wait_healthy "http://127.0.0.1:8110/health" "dev"  "$HEALTH_TIMEOUT"
    wait_healthy "http://127.0.0.1:8111/health" "live" "$HEALTH_TIMEOUT"
}

# --- Main ---

case "${1:-}" in
    --build)
        build_containers
        ;;
    --cli)
        build_cli
        ;;
    *)
        echo "Codero dev deploy — syncing worktree to all surfaces"
        echo ""
        build_cli
        build_containers
        restart_containers
        echo ""
        green "All done. Dev :8110 | Live :8111 | CLI: $(codero version 2>/dev/null || echo 'updated')"
        ;;
esac
