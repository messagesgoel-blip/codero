# Agent Handling Contract

**Version:** 1.1
**Last Updated:** 2026-03-30
**Status:** active

## Purpose

This document is the single contract for how Codero launches, tracks, supervises,
exposes, and finalizes agent work when agents run in detached terminal sessions.

It replaces the vague split between "agent UI", "session wrapper", and "operator
surface" with one clear model:

- Codero's product UI is the web dashboard plus CLI commands.
- Detached terminal sessions are the runtime for agents, not the product UI.
- `tmux` is the normative backend today.
- `screen` may be supported later only through the same backend adapter contract.

This contract consolidates and clarifies behavior currently spread across:

- `docs/contracts/session-lifecycle-contract.md`
- `docs/evidence/agent-v3-interactions.md`
- `docs/evidence/task-layer-v2-architecture.md`
- `docs/dashboard-architecture.md`
- `docs/agent-setup.md`

## Product Stance

Codero does not ship or depend on an operator-facing TUI.

The dashboard at `/dashboard` is the sole interactive Codero UI for operators.
CLI commands remain supported for scripting, diagnostics, and local workflows.
Any `tmux` or `screen` UI used around Codero is an execution aid, not the system
of record.

This matters because several strong open-source references in this space include
their own terminal UI. Codero borrows their runtime and information patterns, but
maps them onto the existing dashboard instead of adopting a second product surface.

## Primary Borrow Sources

Codero should borrow behavior from the following repositories.

| Source | License | Borrow | Do Not Borrow |
|--------|---------|--------|---------------|
| [Ataraxy-Labs/opensessions](https://github.com/Ataraxy-Labs/opensessions) | MIT | Session list/detail mental model, lightweight status/progress/log signaling, `tmux`-native observer pattern | Sidebar UI, Bun runtime, local HTTP server as Codero source of truth |
| [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad) | AGPL-3.0 | One-agent-one-`tmux` session, one-worktree-one-branch isolation, attach/resume/kill lifecycle, profile-based launch commands | Its TUI, direct product UX, or any AGPL code import |
| [coplane/par](https://github.com/coplane/par) | MIT | Globally unique context labels, durable worktree naming, unified view of many sessions and repos | Its control-center terminal UI as a Codero UI surface |
| [wehnsdaefflae/terminal-control-mcp](https://github.com/wehnsdaefflae/terminal-control-mcp) | MIT | Raw output capture, dual user-plus-agent session access, history isolation, audit-minded runtime boundaries | Browser terminal as the default Codero operator interface |

### Reuse Boundary

For AGPL projects such as Claude Squad, Codero may borrow architecture and
behavioral patterns only. This contract does not authorize copying AGPL code into
Codero.

## Dashboard Alignment

The external projects above mostly present their information through terminal UIs.
Codero must express the same operational concepts through its dashboard tabs and
APIs instead.

| External concept | Borrowed from | Codero dashboard home | Canonical API / state |
|------------------|---------------|------------------------|-----------------------|
| Global control center across many live contexts | `par control-center`, `opensessions` sidebar | `Overview` | `/api/v1/dashboard/overview`, `/events`, `/health`, `/gate-health` |
| Session list with attention-first sorting | `opensessions` session list, Claude Squad session menu | `Sessions` | `/api/v1/dashboard/sessions`, inferred status fields |
| Session detail, repo breadcrumb, cwd, branch, last activity | `opensessions` detail panel | `Sessions` drill-down | `/api/v1/dashboard/sessions/{id}` |
| Live tail / recent terminal output | `opensessions` logs, `terminal-control-mcp` capture modes | `Sessions` detail tail panel | `/api/v1/dashboard/sessions/{id}/tail`, local tail files |
| Open agent roster, supported profiles, tracked binaries | Claude Squad profiles, `opensessions` live agent detection | `Agents` | `/api/v1/dashboard/agents`, `/tracking-config` |
| Queue of work waiting to be claimed | `par` global labels, Claude Squad new-session workflow | `Tasks` | `/api/v1/dashboard/tasks`, `/queue`, `agent_assignments` |
| Repo/worktree placement and branch ownership | `par` worktrees/workspaces | `Repos` | `/api/v1/dashboard/repos`, `branch_states` |
| Post-submit flow: waiting, gating, PR, monitoring | Claude Squad commit/push/resume mental model | `Pipeline` | `/api/v1/dashboard/pipeline`, `agent_assignments` |
| Blocked state, CI failures, compliance, merge readiness | runtime feedback patterns across all four repos | `Gate` | `/api/v1/dashboard/gate-checks`, `/compliance`, feedback cache |
| Runtime and process settings | `par` config, Claude Squad profile config, `terminal-control-mcp` security/session config | `Settings` | `/api/v1/dashboard/settings`, `/tracking-config` |
| Fleet health metrics | control-plane operation, not external UI | `Scorecard` | `/api/v1/dashboard/scorecard` |

### Alignment Rule

Codero does not need to reproduce an external terminal layout. It only needs to
surface the same decisions:

- what is running
- where it is running
- what repo and branch it owns
- whether it is making progress
- what needs operator attention
- how to reattach locally if manual intervention is needed

## Canonical Roles

### 1. Control Plane

The Codero control plane is the durable source of truth for:

- sessions
- assignments
- branch ownership
- delivery pipeline state
- feedback
- compliance
- operator-facing status

SQLite is authoritative. Runtime metadata from `tmux`, tails, hooks, or shim
scripts is auxiliary and reconstructable.

### 2. Launcher

The launcher is the Codero-owned wrapper around the real agent binary.

Current Codero launch surfaces:

- `codero agent launch`
- `codero agent run`
- generated shims from `codero agent hooks --install`

The launcher is responsible for:

- resolving agent identity and profile
- creating or joining the runtime session
- registering the Codero session
- owning the heartbeat secret
- sending heartbeats
- capturing output tail
- finalizing on exit

The launcher is not the worker. It is the supervisor.

### 3. Agent Process

The agent process is the real coding worker running inside the detached session.

The agent may:

- read task and assignment context
- edit files in its assigned worktree
- emit assignment updates
- submit work for delivery
- read feedback

The agent may not:

- own liveness
- own session registration
- own merge decisions
- own branch handoff
- own recovery logic

### 4. Runtime Adapter

The runtime adapter is the backend for detached terminal sessions.

`tmux` is the normative adapter today. A future `screen` adapter is acceptable
only if it satisfies the same contract:

- create detached session
- test whether session exists
- send command to session
- capture recent output
- kill session
- list Codero-managed sessions
- provide a local attach command

The dashboard and control plane contract must not change when adapters change.

## Canonical Runtime Objects

### Agent Profile

A configured launch target for a binary or shim, for example `claude`, `codex`,
or `aider`.

Fields:

- `agent_id`
- launch command
- tracking enabled or disabled
- optional environment variables
- installed or missing status

### Session

One live runtime process supervised by Codero.

Fields:

- `session_id`
- `agent_id`
- `mode`
- `tmux_session_name`
- `started_at`
- `last_seen_at`
- inferred operator status

### Assignment

The durable connection between a live session and a task, repo, branch, and
worktree.

Fields:

- `assignment_id`
- `task_id`
- `session_id`
- `repo`
- `branch`
- `worktree`
- lifecycle `state`
- versioned `substatus`

### Tail

A bounded, sanitized, owner-only rolling output record for the session.

### Feedback Bundle

The control-plane result written after submit, including gate results, CI
results, review findings, and suggested next action.

## Core Invariants

1. A live `session_id` maps to exactly one running agent process.
2. Each live session maps to at most one live assignment.
3. Any task maps to at most one live assignment.
4. Each branch maps to at most one live owner session.
5. The launcher owns the heartbeat secret.
6. The agent never owns merge, handoff, or stale-session recovery.
7. SQLite is the source of truth; detached terminal sessions are runtime evidence.
8. Dashboard APIs must not expose secrets, raw prompts, or worktree contents.

## Naming and Identity

Borrowing from `par` and Codero's existing `internal/tmux` package, terminal
session names must be deterministic and easy to inspect.

Current canonical `tmux` naming:

- `codero-<agent_id>-<session_id_prefix>`

Examples:

- `codero-claude-a1b2c3d4`
- `codero-codex-09f7e221`

The human-friendly identity shown in the dashboard may be richer than the raw
runtime session name, but the runtime name must remain stable and machine-usable.

## Supported Launch Modes

### 1. Managed Detached Session

Preferred for production-style or long-running agents.

Surface:

- `codero agent launch --agent <id> --repo <path> --branch <branch> -- <cmd...>`

Borrowed primarily from Claude Squad:

- one detached terminal session per agent
- one worktree per branch
- explicit attach or resume lifecycle

### 2. Wrapped Foreground Process

Preferred for local manual runs, shimmed binaries, and gradual adoption.

Surface:

- `codero agent run --agent-id <id> -- <binary> <args...>`

This may run inside `tmux` or in a plain shell. Codero still tracks the process
through session registration and heartbeat.

### 3. Shimmed Existing Binary

Preferred for tools like Claude Code or Codex CLI that should continue to feel
native to the user.

Surface:

- `codero agent hooks --install`

Borrowed from Claude Squad profile launches and `opensessions` watcher-style
integration:

- keep the user's normal binary name
- insert Codero tracking without changing the agent's core UX

## Session Lifecycle

### Bootstrap

The launcher performs this sequence:

1. Resolve `agent_id`, mode, repo, branch, worktree, and launch command.
2. Generate a new `session_id`.
3. Compute the runtime session name.
4. Create the detached runtime session when using `codero agent launch`.
5. Register the Codero session and receive a heartbeat secret.
6. Write `.codero/SESSION.md` and `.codero/AGENT.md` into the worktree.
7. Export `CODERO_SESSION_ID`, `CODERO_AGENT_ID`, and `CODERO_DAEMON_ADDR`.
8. Start the real agent command inside the runtime session.
9. Begin heartbeat and tail capture.
10. Finalize when the child process exits or the runtime session disappears.

### Registration

Registration creates or refreshes durable session state with:

- `session_id`
- `agent_id`
- `mode`
- `tmux_session_name`
- `started_at`
- `last_seen_at`
- heartbeat secret

Single-use session IDs must not be reused after they have ended.

### Heartbeat

Heartbeat is launcher-owned.

Normative behavior:

- target interval: every 30 seconds
- secret required on every heartbeat
- progress is distinct from liveness
- the child agent process is not expected to call heartbeat APIs directly

### Progress Detection

Borrowing from `opensessions` status updates and `terminal-control-mcp` raw
stream capture, Codero should infer progress from observed runtime activity
instead of trusting a generic "still running" signal.

Current behavior:

- child output counts as activity
- one silent interval is enough to stop marking progress
- progress and liveness are stored separately

### Inferred Operator Status

The dashboard-facing status is separate from assignment state:

- `working`
- `waiting_for_input`
- `idle`
- `unknown`

Precedence:

1. `waiting_for_input`
2. `working`
3. `idle`
4. `unknown`

This status exists for triage and sorting, not for business logic.

### Tail and Runtime Capture

Borrowing from `terminal-control-mcp` and `opensessions`, Codero should always
preserve a compact operator-readable tail.

Normative behavior:

- tail is plain text, not raw ANSI dump
- tail is bounded in size
- tail files are owner-only on disk
- dashboard exposes read-only tail access
- Codero does not expose a browser terminal in v1

## Agent Interaction Contract

The agent contract must stay intentionally small.

### Allowed agent-facing commands

1. `codero session attach`
   - attach repo, branch, worktree, and optional task context to a session
2. `codero task accept`
   - atomically claim a task for the live session
3. `codero task emit`
   - emit a versioned assignment substatus update
4. `codero task submit`
   - hand the assignment into Codero's delivery pipeline
5. feedback read surfaces
   - `GetFeedback`
   - generated feedback artifacts such as `FEEDBACK.md`

### Non-agent commands

The agent does not call:

- session registration
- heartbeat
- merge actions
- direct pipeline stage mutations
- branch handoff APIs

## Task Claiming and Assignment Ownership

### Claim rules

Borrowing from runner systems and Codero's current task layer:

- `task_id` may have only one live assignment
- a repeated claim by the same session is idempotent
- a rival live session receives conflict
- a session may hold only one live assignment
- a new accepted task supersedes the session's previous live assignment

### Supersede rules

When a new task supersedes an older live assignment for the same session, the old
assignment must end as:

- state: `superseded`
- substatus: `terminal_waiting_for_next_task`

### Branch ownership

`codero session attach` and assignment acceptance update durable branch ownership.
At most one live owner exists per branch.

### Handoff restriction

If Codero records `successor_session_id` on the most recently ended assignment,
only that successor may claim the next assignment for the task.

Handoff is a control-plane decision, not an agent decision.

## Assignment State Model

Codero uses two layers:

- lifecycle `state`
- narrower `substatus`

### Active substatuses

- `in_progress`
- `needs_revision`
- `waiting_for_ci`
- `waiting_for_merge_approval`

### Blocked substatuses

- `blocked_credential_failure`
- `blocked_merge_conflict`
- `blocked_external_dependency`
- `blocked_ci_failure`
- `blocked_policy`

### Agent-emittable terminal substatuses

- `terminal_finished`
- `terminal_waiting_for_comments`
- `terminal_cancelled`

### System-owned terminal substatuses

- `terminal_waiting_for_next_task`
- `terminal_lost`
- `terminal_stuck_abandoned`

Agents must not emit system-owned terminal substatuses.

## Submit and Delivery Boundary

`codero task submit` is the primary agent signal that local work is ready.

Submit means:

- the agent believes the branch is ready for Codero review flow
- Codero takes over push, CI, review, feedback assembly, and merge evaluation
- the assignment transitions into the delivery pipeline

This split borrows the "worker does work, control plane handles delivery" idea
from Claude Squad's commit/push lifecycle, but makes Codero's pipeline explicit
and durable instead of hiding it behind a terminal UI.

## Attach and Manual Intervention

Borrowing from Claude Squad attach and `terminal-control-mcp` dual access:

- the operator may reattach locally to a live runtime session
- the runtime session name must be stored in durable session state
- the dashboard should expose enough metadata to make local attach trivial

V1 rule:

- Codero exposes metadata and read-only tails in the dashboard
- manual input happens through local `tmux attach`, not a web terminal
- state changes caused by manual intervention still flow back through Codero APIs

## Recovery and Failure Handling

### Daemon unavailable at launch

For `codero agent run`, if the daemon is unavailable, the wrapped binary may run
without tracking. This is graceful degradation, not tracked success.

### Invalid heartbeat secret

- heartbeat is rejected
- session state remains unchanged

### Version conflict on emit

- `task emit` rejects stale `assignment_version`
- caller must reread durable assignment state before retrying

### Ended assignment

- once `ended_at` is set, further emits are rejected

### Lost and stuck paths

The control plane distinguishes:

- lost: liveness failed
- stuck abandoned: liveness exists but useful progress stopped for too long

Agents do not decide which recovery path applies.

## Security Boundaries

Borrowing from `terminal-control-mcp` and general runner systems:

1. Registration credentials and heartbeat secrets are launcher-only.
2. Session tails may contain sensitive data and must stay owner-only on disk.
3. Runtime shells should use isolated history files where practical.
4. The dashboard must not become a general-purpose remote shell.
5. Operator APIs may expose metadata, status, and bounded output, but not raw
   worktree content or secrets.

## Implementation Contract

This document is aligned to the following current Codero surfaces:

- `cmd/codero/agent_launch.go`
- `cmd/codero/agent_run.go`
- `cmd/codero/task.go`
- `cmd/codero/agent_hooks.go`
- `internal/tmux/tmux.go`
- dashboard APIs under `/api/v1/dashboard/`

If implementation changes invalidate this contract, the contract must be updated
before the behavior is treated as canonical.

## Compatibility Promise

In v1.1 of this contract, Codero preserves:

- dashboard as the sole interactive Codero UI
- detached `tmux` session runtime as the normative backend
- launcher-owned registration and heartbeat
- one-session-one-worker semantics
- one-task-one-live-assignment semantics
- one-branch-one-live-owner semantics
- `task submit` as the primary handoff into delivery
- control-plane-owned recovery, handoff, and merge logic

Changes to these rules require:

- a contract update
- migration notes
- rollback notes when behavior is user-visible
- regression coverage for the changed lifecycle path
