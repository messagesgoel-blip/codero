# codero

Codero is a code review orchestration control plane.

This repository starts as a clean scaffold and follows a module-intake roadmap:
no bulk copying from prior projects. Capabilities are imported only with contracts,
parity tests, and rollback notes.

## Current Scope (Phase 0)

- Repository governance and contribution standards
- ADR process and initial architecture decisions
- Module intake registry
- CI quality gates: lint, unit, contract
- Minimal CLI surface for contract testing
- Reusable two-pass local review gate scripts (LiteLLM first pass + CodeRabbit second pass)

## Quick Start

```bash
make lint
make unit
make contract
make ci
```

## Two-Pass Review Gate

Run manually in any target repo:

```bash
CODERO_REPO_PATH=/path/to/repo /srv/storage/repo/codero/scripts/review/two-pass-review.sh
```

Install pre-commit hook for a repo:

```bash
/srv/storage/repo/codero/scripts/review/install-pre-commit.sh /path/to/repo
```

## Core Docs

- `docs/roadmaps/codero-roadmap-v5.md`
- `docs/roadmap.md`
- `docs/architecture.md`
- `docs/borrowed-components.md`
- `docs/roadmap-intake-map.md`
- `docs/module-intake-registry.md`
- `AGENTS.md`
- `docs/agent-task-board.md`
- `docs/agent-preflight.md`
- `docs/adr/`
- `CONTRIBUTING.md`

## Contract Tests

Contract tests validate key system behaviors against documented contracts:

| Contract | Test File | Coverage |
|----------|-----------|----------|
| Delivery Pipeline | `tests/contract/delivery_pipeline_contract_test.go` | Submit-to-merge flow, lock lifecycle, feedback schema |
| Session Lifecycle | `tests/contract/session_lifecycle_contract_test.go` | Registration, heartbeat, assignment, archival |
| Dashboard API | `tests/contract/dashboard_api_contract_test.go` | REST endpoints, response schemas |
| Queue Operations | `internal/scheduler/queue_test.go` | WFQ scheduling, lease management |

Run contract tests:

```bash
make contract
# or
go test ./tests/contract/... -v
```

## Integration Tests

Integration tests validate end-to-end flows with mock external dependencies:

| Test | Description |
|------|-------------|
| MIG-039 | Submit-to-merge happy path and gate failure path |

```bash
go test ./tests/integration/... -v
```

## CLI Commands Quick Reference

```bash
codero daemon              Start the long-running daemon
codero tui                 Launch the interactive Bubble Tea operator shell
codero tui --view gate --interval 3
codero tui --theme dracula --no-alt-screen

codero gate-status         One-shot gate status display
codero gate-status --watch           Live gate watch (TUI)
codero gate-status --json            Machine-readable JSON output
codero gate-status --no-prompt       Disable interactive prompt (CI-safe)

codero dashboard           Print effective dashboard URL
codero dashboard --check             Validate /dashboard/, overview API, /gate
codero dashboard --open              Open in default browser (interactive only)
codero dashboard --port 9090         Override port for check

codero ports               Show all active network bindings and URLs

codero commit-gate         Run pre-commit review gate
codero register [branch]   Register branch for review
codero queue [repo]        Show queue state
codero branch [branch]     Show branch details
codero events [branch]     Show delivery events
codero scorecard           Generate proving period scorecard
codero preflight           Run system preflight checks
codero status              Report daemon status
codero version             Print version
```

### Web Dashboard

When the daemon is running, the web dashboard is available at:

```text
http://localhost:8080/dashboard/
```

The base path and bind address are configurable:

```yaml
# codero.yaml
observability_host: ""        # bind all interfaces (default); use "127.0.0.1" for loopback-only
observability_port: 8080      # default
dashboard_base_path: /dashboard   # default; change for reverse-proxy deployments
dashboard_public_base_url: ""     # optional: override URL printed by "codero dashboard"
```

Or via environment variables:

```bash
CODERO_OBSERVABILITY_HOST=127.0.0.1
CODERO_DASHBOARD_BASE_PATH=/codero/ui
CODERO_DASHBOARD_PUBLIC_BASE_URL=https://ops.example.com
```


## Canonical roadmap source

The canonical roadmap for Codero is `docs/roadmap.md`.
The detailed v5 execution roadmap is `docs/roadmaps/codero-roadmap-v5.md`.
If these files ever differ, treat `docs/roadmap.md` as the source of truth unless a PR explicitly states that another file supersedes it.
