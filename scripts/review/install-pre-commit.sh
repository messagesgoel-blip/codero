#!/usr/bin/env bash
set -euo pipefail

# Install Codero review gate as pre-commit hook for a target repo.
# Supports git worktrees via git rev-parse --git-common-dir.

if [ "$#" -lt 1 ]; then
  echo "Usage: $0 <repo-path>"
  exit 1
fi

TARGET_REPO="$1"
TARGET_REPO="$(cd "$TARGET_REPO" && pwd)"
REPO_PATH="$TARGET_REPO"

if [ ! -e "$REPO_PATH/.git" ]; then
  echo "Error: $REPO_PATH does not appear to be a git repo (no .git entry)" >&2
  exit 1
fi

CODERO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

HOOKS_DIR="$(git -C "$REPO_PATH" rev-parse --git-common-dir)/hooks"
mkdir -p "$HOOKS_DIR"

HOOK_PATH="$HOOKS_DIR/pre-commit"

if [ -f "$HOOK_PATH" ]; then
  cp "$HOOK_PATH" "$HOOK_PATH.bak.$(date +%Y%m%d-%H%M%S)"
fi

cat > "$HOOK_PATH" <<'HOOK'
#!/usr/bin/env bash
set -euo pipefail
export CODERO_REPO_PATH="$(git rev-parse --show-toplevel)"
export CODERO_MODEL_ALIAS="cacheflow_agent"
exec "$CODERO_ROOT/scripts/review/two-pass-review.sh"
HOOK

chmod +x "$HOOK_PATH"

echo "Installed Codero review pre-commit hook: $HOOK_PATH"
echo "  Model alias: cacheflow_agent"
echo "  Mode: fast (default)"
