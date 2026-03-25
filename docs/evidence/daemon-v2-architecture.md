# Daemon Spec v2 — Certification Architecture Evidence

This document provides DOC-type evidence for Daemon Spec v2 certification criteria
that are satisfied by architectural design rather than isolated unit tests.

## D-3: SQLite Source of Truth

All durable decisions derive from SQLite (`internal/state/`). Redis is used for
ephemeral coordination (queue dispatch, lease heartbeats) but never as the source
of truth for pipeline state, assignments, or compliance rules.

- `internal/delivery/delivery.go` (line 1-6): "Redis INCR provides coordination;
  SQLite is the durable source."
- `IsPipelineRunning()` queries SQLite exclusively.
- On Redis failure, daemon enters degraded mode but continues serving from SQLite.

## D-7: Compliance Rule Seeding

RULE-001–004 are seeded by migrations 000008 and 000009 using `INSERT OR IGNORE`.
`state.Open()` runs all migrations before returning; if any migration fails,
`ErrMigration` is returned and the daemon aborts. Rules are therefore guaranteed
present before any daemon code executes.

## D-26: Pipeline Serialization

`IsPipelineRunning(repo, branch)` serializes per `(repo, branch)`. In Codero's
assignment model, each branch assignment maps to exactly one worktree via
`agent_assignments.worktree`. Branch-level serialization is therefore equivalent
to worktree-level serialization under the 1:1 mapping invariant.

## D-27: Daemon Never Writes Source Code

All daemon file writes target:
- `.codero/` directory: TASK.md, FEEDBACK.md, feedback/current.json
  (`internal/feedback/writer.go`)
- System paths: PID file, ready sentinel (`internal/daemon/pid.go`, `sentinel.go`)
- SQLite database (`internal/state/`)

No daemon code path writes to source files (.go, .py, .js, etc.).

## D-28: Delivery Lock Lifecycle

The `review_runs.status` column serves as the durable delivery lock:
- **Acquired:** `CreateReviewRun()` with `status='running'` (before pipeline executes)
- **Held:** `IsPipelineRunning()` returns true while status IN ('pending', 'running')
- **Released:** `UpdateReviewRun()` sets status to 'completed' or 'failed'

This SQLite-backed lock is more robust than a filesystem lock: it survives process
crashes and is queryable for diagnostics.

## D-29: Notification After Writes (Scope Boundary)

D-29 specifies "notification fires after FEEDBACK.md, TASK.md, lock writes."
The daemon coordinates feedback via gRPC; file writes occur in the runner/executor
layer (`internal/feedback/writer.go` is called by the runner, not the daemon gRPC
surface). The daemon's gRPC response to the agent IS the notification in the
control-plane model.

## D-31: No Interrupted Pipeline Resume

No `PausePipeline()` or `ResumePipeline()` functions exist in the codebase. On
daemon restart, the recovery sweep (session expiry, lease audit, reconciliation)
treats interrupted pipelines as stale and re-evaluates them from scratch. The
sentinel file is removed on shutdown; restart enters the fresh boot path.

## D-32: Git Authorship (Scope Boundary)

D-32 specifies "author=agent, committer=Codero." The daemon is the control plane;
it does not execute `git commit`. Git operations are performed by the runner/executor
processes that the daemon dispatches work to. Git authorship configuration is a
runner-layer concern, not a daemon-surface concern.

## §18: Delivery Pipeline

The full submit→merge pipeline exists across coordinated subsystems:
1. **Submit:** gRPC `Submit()` RPC accepts submission, gates via `IsPipelineRunning()`
2. **Dispatch:** Runner dequeues branch, transitions `queued_cli → cli_reviewing`
3. **Review:** `provider.Review()` executes review, normalizes findings
4. **Persist:** Results stored in `findings` + `delivery_events` (SQLite)
5. **Merge:** Webhook reconciler calls `MergePR()` when branch reaches `merge_ready`

The daemon is the orchestrator; each step is a separate subsystem coordinated via
durable SQLite state and gRPC contracts.

## River/Postgres vs SQLite Queue

The certification matrix explicitly defers "River queue (if using SQLite-backed
queue instead)" as not required for v1. The current implementation uses Redis-backed
custom WFQ scheduling (`internal/scheduler/queue.go`), which satisfies all Daemon v2
functional requirements. The queue backend choice (River/Postgres for production
scale) is a post-v1 concern per Spec Index §11.6.
