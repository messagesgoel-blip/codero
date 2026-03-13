#!/usr/bin/env bash
set -euo pipefail

# Install Codero two-pass review as pre-commit hook for a target repo.

if [ "$#" -lt 1 ]; then
  echo "Usage: $0 <repo-path>"
  exit 1
fi

TARGET_REPO="$1"
TARGET_REPO="$(cd "$TARGET_REPO" && pwd)"

if [ ! -d "$TARGET_REPO/.git" ]; then
  echo "Error: not a git repository: $TARGET_REPO" >&2
  exit 1
fi

CODERO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOOK_PATH="$TARGET_REPO/.git/hooks/pre-commit"

if [ -f "$HOOK_PATH" ]; then
  cp "$HOOK_PATH" "$HOOK_PATH.bak.$(date +%Y%m%d-%H%M%S)"
fi

cat > "$HOOK_PATH" <<HOOK
#!/usr/bin/env bash
set -euo pipefail
export CODERO_REPO_PATH="\$(git rev-parse --show-toplevel)"
exec "$CODERO_ROOT/scripts/review/two-pass-review.sh"
HOOK

chmod +x "$HOOK_PATH"

echo "Installed Codero two-pass pre-commit hook: $HOOK_PATH"
