# Contract: Review Gate v1

## Scope

Local pre-commit gate contract used across repositories.

## Stages

1. `local-first-pass.sh`
- Input: uncommitted tracked + untracked diff
- Backend: LiteLLM chat completion endpoint
- Output: plain text findings to stdout
- Exit: `0` success, non-zero on missing key/endpoint/parse failure

2. `coderabbit-second-pass.sh`
- Input: current working tree diff (`coderabbit review --type uncommitted`)
- Output: plain text findings to stdout
- Exit: `0` success, non-zero if CLI missing/fails

3. `two-pass-review.sh`
- Runs Stage 1 then Stage 2
- Writes per-stage logs under `.codero/review-logs/`
- Exit: non-zero if either stage fails

## Compatibility Promise

- Script names and stage order remain stable in v1.
- Environment variables listed in `docs/review-workflow.md` remain supported.
