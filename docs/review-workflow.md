# Review Workflow (Two-Pass Gate)

Codero adopts the proven Mathkit-v2 local review workflow:

1. First pass: LiteLLM (`scripts/review/local-first-pass.sh`)
2. Second pass: CodeRabbit CLI on uncommitted diff (`scripts/review/coderabbit-second-pass.sh`)

Combined gate:

- `scripts/review/two-pass-review.sh`

## Install as pre-commit hook for a repo

```bash
/srv/storage/local/codero/scripts/review/install-pre-commit.sh /path/to/repo
```

Install for multiple repos:

```bash
/srv/storage/local/codero/scripts/review/install-pre-commit-all.sh /srv/storage/local/codero/docs/managed-repos.txt
```

## Environment

- `CODERO_REPO_PATH` optional target repo root
- `CODERO_LITELLM_URL` default `http://localhost:4000/v1/chat/completions`
- `CODERO_LITELLM_MODEL` default `qwen3-coder-plus`
- `LITELLM_MASTER_KEY` required (or present in target repo `.env`)

## Behavior

- If no uncommitted changes exist, first pass exits cleanly.
- Any first-pass failure blocks commit.
- CodeRabbit command failure blocks commit.
- Logs are written under `<repo>/.codero/review-logs/`.
