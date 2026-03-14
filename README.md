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
CODERO_REPO_PATH=/path/to/repo /home/sanjay/codero/scripts/review/two-pass-review.sh
```

Install pre-commit hook for a repo:

```bash
/home/sanjay/codero/scripts/review/install-pre-commit.sh /path/to/repo
```

## Core Docs

- `docs/roadmaps/codero-roadmap-v5.md`
- `docs/architecture.md`
- `docs/module-intake-registry.md`
- `AGENTS.md`
- `docs/agent-task-board.md`
- `docs/agent-preflight.md`
- `docs/adr/`
- `CONTRIBUTING.md`

## Canonical roadmap source

The canonical roadmap for Codero is `docs/roadmap.md`.
The detailed v5 execution roadmap is `docs/roadmaps/codero-roadmap-v5.md`.
If these files ever differ, treat `docs/roadmap.md` as the source of truth unless a PR explicitly states that another file supersedes it.
