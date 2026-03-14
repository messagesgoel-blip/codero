# Agent Preflight Checklist

Run this before starting work on a task branch.

1. Confirm you are in `/home/sanjay/codero`.
2. Pull latest `main`.
3. Create branch `feat/COD-{issue-id}-{short-description}`.
4. Claim task in `docs/agent-task-board.md` (`in_progress`).
5. Confirm target files are not owned by another active task.
6. Implement changes.
7. Run two-pass review: `scripts/review/two-pass-review.sh`.
8. Run `make ci`.
9. Commit with clear message and push branch.
10. Open PR and fill template completely.
