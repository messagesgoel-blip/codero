# Agent Rules (Repo-Neutral)

This file defines mandatory operating rules for coding agents.
It is intentionally repo-neutral and should work across projects.

## 1) Core Principles

1. One agent, one branch, one active task.
2. No direct work on `main` (or default protected branch).
3. All work must be traceable to a task id.
4. No "done" state without tests and PR.

## 2) Branching and Task Ownership

1. Create a dedicated branch per task: `feat/COD-{task-id}-<short-description>`.
2. Claim the task before coding in the repo's task board (or equivalent tracker).
3. If a task is already `in_progress`, do not start parallel implementation.
4. Keep branch scope narrow; one task branch should not contain unrelated changes.
5. Each agent must work in its own dedicated git worktree; agents must not share the same worktree path.

## 3) Pre-Commit Quality Gate

Before each commit, run the repository's local review gate.
Default pattern:

1. First pass local AI review (LiteLLM/Aider or equivalent).
2. Second pass local CodeRabbit CLI review on uncommitted changes.
3. Fix findings before commit.

If the repo ships `scripts/review/two-pass-review.sh`, use it as the default gate.

## 4) Validation Before Push

Run required checks before pushing:

- lint
- unit tests
- contract/integration tests (as applicable)

Use the project's standard command (for example `make ci`) when available.

## 5) Pull Request Rules

1. PR required for all branches targeting protected branches.
2. PR must include:
   - linked task id
   - scope summary
   - tests run
   - risk + rollback notes
3. Do not merge until required checks are green.

## 6) Merge Authority

Policy baseline:

- Merge operations to protected branches are performed only by the designated merge authority (for this repo: Codex).
- Human emergency overrides must be explicitly documented in the PR conversation.

Technical note:

- Actor-level enforcement depends on GitHub plan/repo type.
- If strict actor enforcement is required, use org-level branch restrictions or equivalent rulesets.

## 7) Legacy/Reference Copy Policy

1. Do not bulk-copy from legacy repos.
2. Intake only module-by-module.
3. For each copied module, require:
   - contract doc
   - parity tests
   - rollback note in PR

## 8) Safety Rules

1. Never expose secrets/tokens in code, logs, or artifacts.
2. Do not rewrite shared history (`push --force`) unless explicitly approved.
3. Stop and escalate if conflicting instructions or unexpected repo state appears.

## 9) Definition of Done

Work is done only when all are true:

1. Task ownership is updated.
2. Local review gate completed.
3. Required checks passed.
4. Changes committed and pushed on task branch.
5. PR opened and ready for review/merge.
