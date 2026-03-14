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

If the repo ships `scripts/review/two-pass-review.sh`, use it as the default prehook gate.
In this repo, `scripts/review/two-pass-review.sh` is authoritative.

Current mandatory gate policy:

1. Run `aider` first pass.
2. Run `gemini` second pass.
3. Two successful checks are mandatory before commit.
4. If a primary check is rate-limited or times out, run fallback chain:
   - `pr-agent` third
   - `coderabbit` fourth (only if still below two successful checks)
5. Fix findings before commit.

Operational rule:

- All agents should use these shared review scripts.
- Do not modify these shared scripts per-agent or per-task. Use the repository defaults as-is unless a dedicated infra/policy task explicitly changes them for everyone.

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
