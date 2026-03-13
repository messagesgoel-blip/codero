#!/usr/bin/env bash
set -euo pipefail

# Install pre-commit hook on all repos listed in a text file.
# Default file: docs/managed-repos.txt

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CODERO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LIST_FILE="${1:-$CODERO_ROOT/docs/managed-repos.txt}"

if [ ! -f "$LIST_FILE" ]; then
  echo "Error: repo list file not found: $LIST_FILE" >&2
  exit 1
fi

while IFS= read -r line; do
  repo="$(echo "$line" | sed 's/#.*$//' | xargs)"
  [ -z "$repo" ] && continue
  "$SCRIPT_DIR/install-pre-commit.sh" "$repo"
done < "$LIST_FILE"
