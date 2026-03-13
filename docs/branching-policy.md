# Branching Policy

## Default Branch

- `main` is the protected branch.

## Rules

- No direct pushes to `main`.
- All changes must come through PRs.
- PR requires passing CI checks: lint, unit, contract.
- PR description must include scope, risk, rollback.

## Branch Naming

Recommended format:

- `feat/<short-description>`
- `fix/<short-description>`
- `chore/<short-description>`
