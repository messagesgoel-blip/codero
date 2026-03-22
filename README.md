# codero

Codero is a code review orchestration control plane.

The current execution focus is no longer repo bootstrap or generic module intake.
The Agents v3 session/compliance layer is merged on `main`, and the next active work is finishing Task Layer v2 on top of that baseline.

## Current Scope

- Agents v3 session, assignment, compliance, and dashboard baseline on `main`
- Task Layer v2 roadmap tracked in `docs/roadmap.md` as `TL-001` through `TL-008`
- Two-pass local review gate scripts and operator surfaces
- Broader platformization, tenant work, and scale/failover hardening deferred until agent/task work is complete

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

- `docs/roadmap.md`
- `docs/roadmaps/archive/codero-roadmap-v5.md` (archived detailed roadmap)
- `docs/architecture.md`
- `docs/module-intake-registry.md`
- `AGENTS.md`
- `docs/agent-task-board.md`
- `docs/agent-preflight.md`
- `docs/adr/`
- `CONTRIBUTING.md`

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
The detailed v5 roadmap is archived at `docs/roadmaps/archive/codero-roadmap-v5.md`.
If these files ever differ, treat `docs/roadmap.md` as the source of truth unless a PR explicitly states that another file supersedes it.
