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

## Managed Agent Profiles
- In Codero agent runtime work, `agent_id` means the Codero launch profile ID, not the raw upstream binary name.
- Current supported managed agent kinds are: `claude`, `codex`, `opencode`, `copilot`, `gemini`.
- Codero-managed shims under `~/.codero/bin` are the only supported source of installed agent profiles.
- Shell aliases and ad hoc wrapper scripts outside `~/.codero/bin` are not a supported discovery source.
- Aliases belong to one owning profile; deleting the profile must remove all of its aliases/shims.
- Registry behavior must preserve:
  - daemon background refresh every 24 hours
  - fresh scan on dashboard/CLI reads so deleted profiles disappear immediately
- When editing agent profile/runtime code, keep profile metadata separate from agent kind:
  - `agent_id` = launch profile ID
  - `agent_kind` = upstream CLI family
- Current validated schema fields live in user config/registry for future launch translation:
  - `agent_kind`, `auth_mode`, `home_strategy`, `home_dir`, `config_strategy`, `config_path`, `permission_profile`, `default_args`
- Until profile translation lands in runtime, `env_vars` are the only applied per-profile launch override; do not assume `home_strategy`, `permission_profile`, or `default_args` already affect live launches.

## Module Intake Rules
- Roadmap first: every intake must map to an existing roadmap item, contract gap, or proving gap.
- Contract first: no copied code lands without contract linkage, tests, rollback notes, and attribution metadata.
- Smallest useful slice only: Codero borrows implementation patterns, not upstream branding, taxonomy, or UI identity.
- Direct code intake is allowed only for narrow, identifiable slices from permissive licenses that Codero can cover with tests.
- Safe default direct-intake licenses:
  - MIT
  - Apache-2.0
  - BSD-2-Clause
  - BSD-3-Clause
  - ISC
  - 0BSD
- Case-by-case only until explicitly reviewed:
  - MPL-2.0
  - LGPL
  - EPL
- Reference-only by default for Codero's current license posture:
  - GPL
  - AGPL
  - SSPL
  - BSL
- Intake modes:
  - `direct code`: copy a small permissive slice with attribution and tests
  - `behavior`: reimplement the upstream behavior inside Codero's own architecture
  - `reference-only`: use the source for research, screenshots, naming, or operational guidance only
- Every intake record must capture:
  - roadmap target
  - source repo and URL
  - source license
  - source commit, tag, or review date
  - intake mode
  - files touched in Codero
  - contract or spec link
  - tests added or updated
  - rollback path
  - attribution note
- Required workflow:
  1. Identify the roadmap item or contract gap.
  2. Choose one to three upstream candidates.
  3. Classify each candidate by license and intake mode.
  4. Record the source in `docs/borrowed-components.md`.
  5. Record the intake in `docs/module-intake-registry.md` when a normative module or copied slice is adopted.
  6. Write or update the relevant contract before landing implementation.
  7. Import or reimplement the smallest useful slice.
  8. Add parity, unit, contract, or integration coverage.
  9. Document rollback notes and attribution.
- Things we do not copy:
  - full upstream terminal UIs
  - upstream branding or terminology when Codero already has a clearer term
  - end-to-end frameworks when only one subsystem is needed
  - code that would force Codero into an incompatible open-source license
  - code that bypasses Codero contracts, tests, or deterministic state rules
- Related intake docs:
  - `docs/borrowed-components.md`
  - `docs/roadmap-intake-map.md`
  - `docs/module-intake-registry.md`
- Codero is intended to become an open-source project. Before the first public release, keep direct code intake conservative and ensure root license/notice files plus copied-code attribution exist.

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

## Live Deployment

**Deployment directory:** Set via `CODERO_DEPLOY_DIR` env var (defaults to standard deploy location)

**Sync script:** `scripts/sync-live.sh`

```bash
# Required env vars
export CODERO_DEPLOY_DIR=/path/to/deploy
export CODERO_PROXY_IP=172.25.0.31  # IP on proxy network
export CODERO_PROXY_NETWORK=proxy   # Optional, defaults to proxy

# One-command deploy (auto-increments patch version)
./scripts/sync-live.sh

# Or specify version
./scripts/sync-live.sh v1.0.3
```

The script:
1. Syncs repo files to `$CODERO_DEPLOY_DIR` (excludes `.env`, `.git`, runtime dirs)
2. Updates `CODERO_VERSION` in `.env`
3. Builds Docker image
4. Recreates container with current env vars via `--env-file`
5. Waits for health check

**Manual deploy steps:**
```bash
# 1. Set deploy directory
DEPLOY_DIR="${CODERO_DEPLOY_DIR:-<default-path>}"

# 2. Sync files
rsync -av --exclude '.git' --exclude 'bin/' /srv/storage/repo/codero/.worktrees/main/ "$DEPLOY_DIR/"

# 3. Update version
sed -i 's/CODERO_VERSION=.*/CODERO_VERSION=v1.0.X/' "$DEPLOY_DIR/.env"

# 4. Build and restart
cd "$DEPLOY_DIR"
docker build -t codero:v1.0.X .
docker rm -f codero
# Then docker run with env vars from .env
```

**Container config:**
- Network: `proxy` at `172.25.0.31`
- Port: `127.0.0.1:8110:8080`
- Redis: `codero-redis-prod` on same network
- Volumes: `codero-db`, `codero-logs`, `codero-pids`, `codero-tmp`, `codero-snapshots`

**Health endpoint:** `http://127.0.0.1:8110/health`
