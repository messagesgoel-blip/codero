#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_BIN="${CODERO_BUILD_BIN:-${ROOT_DIR}/codero}"
LIVE_BIN="${CODERO_LIVE_BIN:-}"
PATH_BIN="${CODERO_PATH_BIN:-}"

if [ ! -f "$SRC_BIN" ]; then
  echo "sync-live-bin: source binary not found: ${SRC_BIN}" >&2
  exit 1
fi

sync_one() {
  local dest="$1"
  local dest_dir
  dest_dir="$(dirname "$dest")"
  if [ ! -d "$dest_dir" ]; then
    echo "sync-live-bin: skip missing dir: ${dest_dir}"
    return 0
  fi
  local tmp="${dest}.tmp.$$"
  cp "$SRC_BIN" "$tmp"
  chmod 0755 "$tmp"
  mv -f "$tmp" "$dest"
  echo "sync-live-bin: updated ${dest}"
}

if [ -n "$LIVE_BIN" ]; then
  sync_one "$LIVE_BIN"
else
  echo "sync-live-bin: CODERO_LIVE_BIN not set; skipping live sync"
fi

if [ -n "$PATH_BIN" ]; then
  sync_one "$PATH_BIN"
else
  echo "sync-live-bin: CODERO_PATH_BIN not set; skipping PATH sync"
fi
