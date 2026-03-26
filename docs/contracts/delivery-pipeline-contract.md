# Delivery Pipeline Contract

**Version:** 1.0
**Last Updated:** 2026-03-26
**MIG Reference:** MIG-037

## Overview

The delivery pipeline orchestrates the submit-to-merge flow for agent assignments. It manages state transitions, gate checks, git operations, PR creation, and feedback delivery.

## FSM States

The pipeline uses a finite state machine with the following states:

```
idle → staging → gating → committing → pushing → pr_management → monitoring → feedback_delivery → merge_evaluation → merging → post_merge → idle
```

### State Transitions

| From | To | Trigger |
|------|-----|---------|
| idle | staging | Submit() called |
| staging | gating | Files staged successfully |
| gating | committing | Gate passed |
| gating | feedback_delivery | Gate failed |
| committing | pushing | Commit created |
| pushing | pr_management | Push succeeded |
| pushing | feedback_delivery | Push failed |
| pr_management | monitoring | PR created or skipped |
| monitoring | merge_evaluation | PR checks passed |
| monitoring | feedback_delivery | PR checks failed |
| merge_evaluation | merging | Merge conditions met |
| merging | post_merge | Merge completed |
| post_merge | idle | Archive created |
| feedback_delivery | idle | Feedback written |

## Concurrency Model

### Lock File

The pipeline uses `delivery.lock` in the worktree root for concurrent submit protection:

- Acquired atomically at Submit() start
- Released on any terminal state (success or failure)
- Lock content: `{assignment_id, started_at}`

### ErrPipelineBusy

When a submit is attempted while a pipeline is already running:

```
ErrPipelineBusy error
```

This maps to HTTP 409 Conflict in the API layer.

## Feedback Schema

### FEEDBACK.md

```markdown
# Feedback

## Gate Findings

| Check | Status | File | Line | Message |
|-------|--------|------|------|---------|
| secret-scan | FAIL | config.yml | 42 | secret detected |

## CI Failures

| Job | Run ID | URL |
|-----|--------|-----|
| unit-tests | 12345 | https://... |

## Code Review

| Source | Severity | Count |
|--------|----------|-------|
| CodeRabbit | high | 2 |

## Summary

Brief human-readable summary of blockers.
```

### feedback/current.json

```json
{
  "version": "1.0",
  "generated_at": "2026-03-26T12:00:00Z",
  "assignment_id": "assign-123",
  "gate_findings": [
    {
      "check_id": "secret-scan",
      "status": "FAIL",
      "file": "config.yml",
      "line": 42,
      "message": "secret detected in config"
    }
  ],
  "ci_failures": [
    {
      "job_name": "unit-tests",
      "run_id": "12345",
      "url": "https://github.com/..."
    }
  ],
  "code_review": {
    "source": "CodeRabbit",
    "findings": [
      {"severity": "high", "count": 2}
    ]
  },
  "summary": "2 blockers require attention"
}
```

## Interfaces

### GitOps

```go
type GitOps interface {
    Stage(worktree string) error
    Commit(worktree string, opts CommitOpts) (string, error)
    Push(worktree, remote, branch string) error
}
```

### GateRunner

```go
type GateRunner interface {
    RunPipeline(ctx context.Context, worktree string, stagedFiles []string) (*Report, error)
}
```

### GitHubClient

```go
type GitHubClient interface {
    CreatePRIfEnabled(ctx context.Context, repo, head, base, title, body string) (prNumber int, created bool, err error)
    TriggerCodeRabbitReview(ctx context.Context, repo string, prNumber int) error
}
```

### Writer

```go
type Writer interface {
    WriteTASK(worktree string, task Task) error
    WriteFEEDBACK(worktree string, feedback FeedbackPackage) error
    ClearFEEDBACK(worktree string) error
}
```

### Notifier

```go
type Notifier interface {
    Notify(worktree, notificationType, assignmentID string)
}
```

## Contract Tests

Location: `tests/contract/delivery_pipeline_contract_test.go`

| Test | Description |
|------|-------------|
| TestMIG037_HappyPath_SubmitGatePassCommitPushPR | Happy path: submit → gate pass → commit → push → PR |
| TestMIG037_GateFailure_WritesFeedback | Gate failure writes FEEDBACK, no commit |
| TestMIG037_PushFailure_WritesFeedback | Push failure writes FEEDBACK, state recovers |
| TestMIG037_LockLifecycle | Lock acquired and released properly |
| TestMIG037_ConcurrentSubmit_409 | Concurrent submit returns ErrPipelineBusy |
| TestMIG037_FeedbackSchema | FEEDBACK.md format validation |
| TestMIG037_FeedbackJSONSchema | feedback/current.json format validation |

## Error Recovery

1. **Gate failure**: State returns to idle, FEEDBACK written, lock released
2. **Push failure**: State returns to idle, FEEDBACK written, lock released
3. **Any error**: Pipeline state persists for debugging, lock always released

## Metrics

The pipeline emits the following metrics:

- `delivery_submit_total{result}` — Submit attempts by result
- `delivery_gate_duration_seconds` — Gate check duration
- `delivery_commit_duration_seconds` — Commit operation duration
- `delivery_push_duration_seconds` — Push operation duration
- `delivery_pipeline_duration_seconds` — Full pipeline duration