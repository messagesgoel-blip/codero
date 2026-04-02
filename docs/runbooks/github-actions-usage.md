# GitHub Actions Usage Watch

This runbook exists to keep GitHub Actions spend and queue churn visible before
the account burns through its private-repo minute budget.

## Current Risk Model

- The account-level cap to watch is the private-repo hosted-runner budget.
- Public repositories still create workflow noise, but they do not show up in
  the private-minute billing usage returned by GitHub's billing endpoint.
- On this account, the immediate budget risk should be driven by billed private
  repos, while `codero` should be optimized mainly to avoid duplicate PR churn.

## Local Usage Check

Run:

```bash
scripts/automation/github-actions-usage.sh
```

The script reports:

- current-month billed private-repo Linux minutes
- current-month billed Actions storage GB-hours
- billed repos for the current month
- recent workflow run volume for the selected repos

Useful variants:

```bash
scripts/automation/github-actions-usage.sh --days 7
scripts/automation/github-actions-usage.sh --repo codero --repo whimsy
scripts/automation/github-actions-usage.sh --cap-minutes 2000
```

## What To Do When Usage Climbs

- If billed private-repo minutes are already above `1500 / 2000`, pause
  non-essential private-repo workflows before enabling new CI-heavy work.
- If public-repo run volume spikes, tighten triggers and add `concurrency`
  cancellation before adding more jobs.
- Keep heavyweight jobs off broad branch-push triggers unless they are needed
  for post-merge assurance.
- Prefer path filters for Go/security analysis jobs when docs-only changes do
  not need them.

## Codero Policy Applied Here

The `codero` workflows now reduce duplicate spend by:

- limiting the main CI fan-out to `main` pushes and PRs to `main`
- using workflow-level `concurrency` cancellation on PR updates
- keeping secret scanning conservative while dropping duplicate `push` runs
- skipping heavyweight analysis jobs when the diff is outside the runtime/test
  paths they actually validate
