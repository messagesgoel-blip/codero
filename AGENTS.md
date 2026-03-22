# Codero Repo Policy

Global references live in `/srv/storage/AGENTS.md`. This file defines Codero-specific policy.

## Canonical Path
`/srv/storage/repo/codero/`

## Issue Prefix
`COD`

## Service Ownership
Codero owns the orchestration control plane: daemon lifecycle, queue and lease coordination, heartbeat processing, repo and task state transitions, and operator status surfaces (CLI, TUI, API).

Does NOT own:
- Cacheflow runtime behavior
- Mathkit runtime behavior
- Cross-repo direct code imports

## Stack
- Runtime: Go (`cmd/`, `internal/`)
- Storage: SQLite durable state and Redis coordination
- Ops surfaces: `/health`, `/queue`, `/metrics`, `/gate`

## Repo-Specific Tooling
- Use shared hook verification and shared testing entrypoints for Codero.
- Prefer `/srv/storage/shared/testing/codero.sh`, `/srv/storage/shared/testing/run-tests`, and `/srv/storage/shared/testing/gate-matrix.sh`.

## Ownership And Gatekeepers
| Path | Owns | Gatekeeper |
|---|---|---|
| `cmd/*` | CLI entrypoints | self |
| `internal/*` | daemon and runtime logic | self |
| `docs/*` | contracts, runbooks, roadmap artifacts | self |
| `scripts/*` | shipped runtime scripts only | flag before editing |
| `infra/*` | deployment and ops integration | flag before editing |

## Repo-Specific Do-Not-Edit
- `**/generated/**` (generated artifacts)
- `**/*.lock` manual edits (tool-managed only)
- `.githooks/*` (managed by shared toolkit install-hooks)
- `.codero/gate-heartbeat/*` runtime state files

## Git And PR Policy
- Branch naming: `feat/<ISSUE_PREFIX>-{issue-id}-{short-description}` using this repo's issue prefix.
- Do not commit directly to protected branches (`main`, `dev`).
- Pre-commit hook must pass before any commit.
- `git commit --no-verify` is prohibited except emergency-only use.
- Emergency bypass requires commit message prefix `[EMERGENCY]` and an immediate follow-up fix commit restoring hook compliance.
- One agent = one branch = one dedicated worktree path.
- PR required before task can be marked complete.
- PR title should include the issue key when applicable.
- PR must include: linked issue, scope summary, tests run, deploy impact, and env var changes.
- Request `@coderabbitai review` and `@coderabbitai summary` on PR open.
- Do not merge with unresolved CodeRabbit blocking comments.

## Testing Notes
- Prefer contract and integration coverage for state transitions and heartbeat flows.
- Keep `docs/contracts/` in sync with runtime behavior.
- For gate behavior changes, validate CLI and `/gate` endpoint parity.
- Common local verification: `go test ./...` and `go test ./... -race`.

## Definition Of Done
A Codero task is done when required tests and gates pass, the change is committed on a task branch, and the PR is opened with required context.
