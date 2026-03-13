# Contributing

## Workflow

1. Create a feature branch from `main`.
2. Claim/update task in `docs/agent-task-board.md`.
3. Implement small, reviewable changes.
4. Run `make ci` locally.
5. Open a PR using the template.
6. Include risk and rollback notes.

See `AGENTS.md` and `docs/agent-preflight.md` for mandatory agent workflow.

## Required Checks

- Lint passes (`make lint`)
- Unit tests pass (`make unit`)
- Contract tests pass (`make contract`)

## Branching

- `main` is protected (no direct pushes).
- PRs require passing CI and at least one review.

See `docs/branching-policy.md` for details.
