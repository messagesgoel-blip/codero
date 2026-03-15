#!/usr/bin/env bash
set -euo pipefail

# Install Codero 6-pass review as pre-commit hook for a target repo.
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
REPO_ROOT="$(git rev-parse --show-toplevel)"
export CODERO_REPO_PATH="$REPO_ROOT"
export CODERO_MODEL_ALIAS="cacheflow_agent"
export CODERO_ROOT="ACTUAL_CODERO_ROOT"
export CODERO_ENV_FILE="${CODERO_ENV_FILE:-$CODERO_ROOT/.env}"

# Prefer local scripts if available, fallback to global
if [ -x "$REPO_ROOT/scripts/review/two-pass-review.sh" ]; then
  exec "$REPO_ROOT/scripts/review/two-pass-review.sh"
elif [ -x "$CODERO_ROOT/scripts/review/two-pass-review.sh" ]; then
  exec "$CODERO_ROOT/scripts/review/two-pass-review.sh"
else
  echo "Error: two-pass-review.sh not found in repo or CODERO_ROOT ($CODERO_ROOT)" >&2
  exit 1
fi
HOOK

export CODERO_ROOT
perl -0777 -pe 's/ACTUAL_CODERO_ROOT/\Q$ENV{CODERO_ROOT}\E/g' "$HOOK_PATH" > "${HOOK_PATH}.tmp"
mv "${HOOK_PATH}.tmp" "$HOOK_PATH"

chmod +x "$HOOK_PATH"

echo "Installed Codero 6-pass pre-commit hook: $HOOK_PATH"
echo "  Model alias: cacheflow_agent"
echo "  Mode: fast (default)"
