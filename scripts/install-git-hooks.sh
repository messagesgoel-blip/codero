#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo_root"

if [ ! -d ".githooks" ]; then
  echo "missing .githooks directory at $repo_root" >&2
  exit 1
fi

git config core.hooksPath .githooks
[ ! -f .githooks/pre-commit ] || chmod +x .githooks/pre-commit

echo "Git hooks installed for codero (core.hooksPath=.githooks)."
