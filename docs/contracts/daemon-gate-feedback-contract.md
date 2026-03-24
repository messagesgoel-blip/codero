# Daemon Gate Feedback Contract

Status: in-progress
Owner: codex

## Summary

- `GateService/PostFindings` records gate output exactly once per `assignment_id` + `gate_run_id`.
- `FeedbackService/GetFeedback` reflects stored gate findings for the same assignment without requiring a second fetch path.
- Replaying the same gate run for the same assignment is a conflict, not a second successful write.

## Contract

- `PostFindings` with a valid `session_id`, `assignment_id`, `gate_run_id`, and known `overall_status` persists the gate snapshot and returns `codes.OK`.
- `GetFeedback` for that `assignment_id` must expose:
  - the assignment `task_id`
  - a gate feedback source
  - `suggested_substatus=needs_revision` for gate `warn` / `fail`
  - `suggested_substatus=ready` for gate `pass`
- A duplicate `PostFindings` for the same `assignment_id` and `gate_run_id` returns `codes.AlreadyExists`.

## Validation

- `go test ./internal/daemon/grpc -run 'TestPostFindings_SuccessAndDuplicate|TestSubmit_DuplicateRunningPipeline|TestIngestTask_ConflictingTaskIDForBranch|TestGetGitHubStatus_'`
