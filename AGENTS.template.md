# AGENTS Template (Repo-Neutral)

Copy this file into a repository as `AGENTS.md`, then fill in project-specific values.

## Scope

- Active repo: `<repo-path>`
- Default branch: `<default-branch>`
- Task tracker location: `<task-board-path-or-url>`
- Legacy/reference repos (if any): `<list-or-none>`

## Core Rules

1. One agent = one branch = one active task.
2. Never work directly on protected branches.
3. Every change must map to a task id.
4. No work is done until tests pass and PR is open.

## Branching

- Branch format: `<branch-format>`
  Example: `feat/COD-123-short-description`

## Task Claim Protocol

1. Claim task before coding.
2. If task is already `in_progress`, do not start competing work.
3. Update status at start, block, handoff, and completion.

## Pre-Commit Review Gate

Before every commit, run local review gate:

1. First pass AI/local review (`<first-pass-command>`)
2. Second pass CodeRabbit CLI review (`<second-pass-command>`)
3. Fix findings before commit

Combined command (if available): `<combined-gate-command>`

## Validation Before Push

Run required checks:

- lint: `<lint-command>`
- unit: `<unit-command>`
- contract/integration: `<contract-command>`
- full gate (optional): `<ci-command>`

## Pull Request Rules

1. PR required for merge to protected branches.
2. PR must include:
   - linked task id
   - scope summary
   - tests run
   - risk + rollback notes
3. Do not merge with failing required checks.

## Merge Authority

- Merge authority: `<merge-authority>`
- Emergency override policy: `<override-policy>`

## Legacy Copy/Port Policy

- No bulk copy from legacy repos.
- Intake only module-by-module.
- Required for every imported module:
  1. contract doc
  2. parity tests
  3. rollback notes

## Safety Rules

1. Never expose secrets/tokens in code, logs, or artifacts.
2. No force-push to shared branches unless explicitly approved.
3. Stop and escalate on conflicting instructions or unexpected repo state.

## Definition of Done

All must be true:

1. Task ownership/status updated.
2. Local review gate completed.
3. Required tests/checks pass.
4. Changes committed and pushed on task branch.
5. PR opened and ready for review.
