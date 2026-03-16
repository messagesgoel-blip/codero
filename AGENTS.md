# Codero Repo Policy

Global policy authority: `/srv/storage/AGENTS.md`.
If any rule conflicts, global policy wins.

## Agent File Lock
- Do not create new agent-policy files for this repo.
- Keep all policy/notes in this file and `/srv/storage/AGENTS.md`.
- Keep secrets only in `/srv/storage/SECRETS.md` (never in repo files).

## Canonical Path
`/srv/storage/repo/codero/`

## Service Ownership
Codero owns the orchestration control plane: daemon lifecycle, queue/lease/heartbeat coordination, repo/task state transitions, and operator status surfaces (CLI/TUI/API).

Does NOT own:
- Cacheflow runtime behavior
- Mathkit runtime behavior
- Cross-repo direct code imports

## Stack
- Runtime: Go (`cmd/`, `internal/`)
- Storage: SQLite durable state + Redis coordination
- API/ops surfaces: daemon observability endpoints (`/health`, `/queue`, `/metrics`, `/gate`)
- Venv/tool env: `/srv/storage/shared/tools/venvs/codero`

## Shared Tools (Mandatory)
- Resolve tooling from: `/srv/storage/shared/tools/bin`
- Required shared tools include: `ruff`, `gitleaks`, `semgrep`, `pre-commit`, `poetry`
- Do not install tools into this repo.
- Verify hook/tool wiring with:
  - `(cd /srv/storage/repo/codero && /srv/storage/shared/agent-toolkit/bin/install-hooks --verify)`

## Shared Testing (Mandatory)
- Use shared test entrypoints:
  - `/srv/storage/shared/testing/codero.sh`
  - `/srv/storage/shared/testing/run-tests`
  - `/srv/storage/shared/testing/gate-matrix.sh`
- Repo-local test helpers are allowed only when shipped with product/runtime behavior.

## Ownership And Gatekeepers
| Path | Owns | Gatekeeper |
|---|---|---|
| `cmd/*` | CLI entrypoints | self |
| `internal/*` | daemon/runtime logic | self |
| `docs/*` | contracts/runbooks/roadmap artifacts | self |
| `scripts/*` | shipped runtime scripts only | flag before editing |
| `infra/*` | deployment/ops integration | flag before editing |

## Repo-Specific Do-Not-Edit
- `**/generated/**` (generated artifacts)
- `**/*.lock` manual edits (tool-managed only)
- `.githooks/*` (managed by shared toolkit install-hooks)
- `.codero/gate-heartbeat/*` runtime state files (ephemeral)

## Branch And PR Policy
- Branch naming (repo convention): `feat/COD-{issue-id}-{short-description}`.
- No direct commits to protected branches (`main`, `dev`).
- PR required before task can be marked complete.
- PR must include: linked issue, scope summary, tests run, deploy impact, env var changes.
- On PR open, request:
  - `@coderabbitai review`
  - `@coderabbitai summary`
- Do not merge with unresolved CodeRabbit blocking comments.
- One agent = one branch = one dedicated worktree path.

## Review Gate Policy
- Agents must not run or modify repo-local legacy gate scripts directly.
- Use only shared heartbeat gate entrypoint:
  - `/srv/storage/shared/agent-toolkit/bin/gate-heartbeat`
- First call starts run.
- If output is `STATUS: PENDING`, poll again after `POLL_AFTER_SEC` (default 180s).
- Proceed only on `STATUS: PASS`.
- On `STATUS: FAIL`, address returned comments in code; do not alter gate infrastructure in feature tasks.

## Local Dev Commands
```bash
# from repo root
go test ./...
go test ./... -race

# shared session contract
/srv/storage/shared/agent-toolkit/bin/agent-preflight codero
/srv/storage/shared/agent-toolkit/bin/agent-finish-task codero "<summary>"
```

## Testing Notes
- Prefer contract + integration coverage for state transitions and heartbeat flows.
- Keep API contract docs in `docs/contracts/` in sync with runtime behavior.
- For gate behavior changes, validate CLI and `/gate` endpoint parity.

## Script Locality Rule
Repo-local scripts are disallowed unless they are shipped runtime requirements and include:
`KEEP_LOCAL: required for shipped runtime <reason>`

All other automation belongs in `/srv/storage/shared/agent-toolkit` or `/srv/storage/shared/testing`.

## Definition Of Done
A task is done only when all are true:
1. Changes are in canonical path `/srv/storage/repo/codero`.
2. Required tests/gates pass.
3. Changes are committed on a task branch.
4. PR is opened with required context.
