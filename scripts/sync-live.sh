#!/bin/bash
# sync-live.sh — Deploy codero to live container
# Usage: CODERO_DEPLOY_DIR=/path ./scripts/sync-live.sh [version]
#
# Syncs repo to $CODERO_DEPLOY_DIR, builds Docker image, and recreates container.
# CODERO_DEPLOY_DIR must be set (no default).
# Version defaults to incrementing patch from current .env or v1.0.0.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY_DIR="${CODERO_DEPLOY_DIR:?CODERO_DEPLOY_DIR must be set}"
IMAGE_NAME="codero"
# Network config - can be overridden via env vars
PROXY_NETWORK="${CODERO_PROXY_NETWORK:-proxy}"
PROXY_IP="${CODERO_PROXY_IP:?CODERO_PROXY_IP must be set (e.g., 172.25.0.31)}"

# Determine version
CURRENT_VERSION="${1:-}"
if [[ -z "$CURRENT_VERSION" ]]; then
    # Auto-increment patch version from .env
    if [[ -f "$DEPLOY_DIR/.env" ]]; then
        CURRENT_VERSION=$(grep -E '^CODERO_VERSION=' "$DEPLOY_DIR/.env" | cut -d= -f2)
        CURRENT_VERSION="${CURRENT_VERSION:-v1.0.0}"
    else
        CURRENT_VERSION="v1.0.0"
    fi
    # Increment patch
    MAJOR=$(echo "$CURRENT_VERSION" | sed 's/v\([0-9]*\).*/\1/')
    MINOR=$(echo "$CURRENT_VERSION" | sed 's/v[0-9]*\.\([0-9]*\).*/\1/')
    PATCH=$(echo "$CURRENT_VERSION" | sed 's/v[0-9]*\.[0-9]*\.\([0-9]*\).*/\1/')
    NEW_PATCH=$((PATCH + 1))
    CURRENT_VERSION="v${MAJOR}.${MINOR}.${NEW_PATCH}"
fi

echo "=== Syncing codero to live (version: $CURRENT_VERSION) ==="

# 1. Sync files to deployment directory
echo ">>> Syncing files to $DEPLOY_DIR"
rsync -av --delete \
    --exclude '.git' \
    --exclude '.codero' \
    --exclude '.env' \
    --exclude 'bin/' \
    --exclude '*.log' \
    --exclude 'db/' \
    --exclude 'logs/' \
    --exclude 'pids/' \
    --exclude 'tmp/' \
    --exclude 'snapshots/' \
    --exclude 'redis/' \
    "$REPO_ROOT/" "$DEPLOY_DIR/"

# 2. Update version in .env
echo ">>> Updating version to $CURRENT_VERSION"
sed -i "s/^CODERO_VERSION=.*/CODERO_VERSION=$CURRENT_VERSION/" "$DEPLOY_DIR/.env" 2>/dev/null || \
    echo "CODERO_VERSION=$CURRENT_VERSION" >> "$DEPLOY_DIR/.env"

# 3. Build Docker image
echo ">>> Building Docker image $IMAGE_NAME:$CURRENT_VERSION"
cd "$DEPLOY_DIR"
docker build -t "$IMAGE_NAME:$CURRENT_VERSION" .

# 4. Tag as latest
docker tag "$IMAGE_NAME:$CURRENT_VERSION" "$IMAGE_NAME:latest"

# 5. Recreate container
echo ">>> Recreating container"

# Stop and remove old container
docker rm -f codero 2>/dev/null || true

# Start new container using --env-file for safe env var handling
docker run -d --name codero \
    --network "$PROXY_NETWORK" --ip "$PROXY_IP" \
    --env-file "$DEPLOY_DIR/.env" \
    -p 127.0.0.1:8110:8080 \
    -v codero-db:/data/db \
    -v codero-logs:/data/logs \
    -v codero-pids:/data/pids \
    -v codero-tmp:/data/tmp \
    -v codero-snapshots:/data/snapshots \
    "$IMAGE_NAME:$CURRENT_VERSION" codero daemon

# 6. Wait for health
echo ">>> Waiting for health check..."
sleep 5

HEALTH=$(curl -s http://127.0.0.1:8110/health 2>/dev/null || echo '{"status":"unknown"}')
echo "Health: $HEALTH"

echo ""
echo "=== Deploy complete ==="
echo "Image:  $IMAGE_NAME:$CURRENT_VERSION"
echo "Health: http://127.0.0.1:8110/health"
docker ps --filter name=codero --format "table {{.Names}}\t{{.Image}}\t{{.Status}}"