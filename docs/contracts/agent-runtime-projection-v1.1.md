# Agent Runtime Projection v1.1

This document defines the canonical runtime projection used by the operator dashboard for live agent sessions.

## Goal

All supported agent families project through one runtime model:

- `claude`
- `codex`
- `opencode`
- `kilocode`
- `copilot`
- `gemini`

The dashboard remains the primary operator surface. This layer does not replace the existing session or assignment subsystems. It normalizes them.

## Canonical Runtime Identity

The canonical runtime projection is built from:

- `agent_sessions`
- the current active `agent_assignments` row, if any
- recent `session_activity` counters
- branch metadata from `branch_states`

Each live runtime row should expose:

- session identity
- agent id
- family
- launch mode
- repo
- branch
- worktree
- task / assignment context
- lifecycle state
- activity state
- last heartbeat
- last meaningful activity
- attribution source
- attribution confidence
- attachment state

## Attachment States

Attachment state is separate from lifecycle state.

- `attached`: the runtime has an active tracked assignment with `repo + branch`
- `inferred`: the runtime has usable context, but it is still relying on session metadata or lightweight launch context
- `orphaned`: the runtime is live but has no reliable repo / branch / worktree context

Lightweight wrapper sessions are allowed to start as `inferred` and later promote to `attached` without losing history. Promotion happens by superseding the lightweight assignment with a tracked assignment once a matching `branch_states` row exists.

## Attribution Sources

Repo / branch attribution uses this precedence:

1. explicit heartbeat metadata
2. hook / plugin heartbeat metadata
3. launch context inference
4. tracked assignment state
5. unresolved / orphaned fallback

Canonical source labels:

- `explicit_heartbeat`
- `hook_metadata`
- `launch_context`
- `assignment_state`
- `unresolved`

Canonical confidence labels:

- `high`
- `medium`
- `low`
- `unknown`

## Lifecycle States

Lifecycle states are projection states, not a replacement for the durable branch FSM.

- `registered`: session exists but has not produced meaningful runtime context yet
- `attributed`: session has repo / branch / worktree context but is not fully attached to tracked branch state
- `active`: session is live and fully attached or otherwise actively running
- `blocked`: session is live but currently waiting on a blocking workflow state
- `recovered`: session was re-adopted after daemon restart and has not yet advanced past that recovery point
- `orphaned`: live session without reliable runtime context
- `finalized`: session ended cleanly
- `failed`: session ended via loss / expiry / stuck-abandoned path

The inventory-only `discovered` state remains outside this live runtime projection. `finalizing` is reserved for future explicit finalize-in-flight surfacing.

## Activity States

Activity is normalized independently of agent family:

- `starting`
- `idle`
- `thinking`
- `editing`
- `running_command`
- `waiting_input`
- `blocked`
- `syncing`
- `completed`
- `failed`
- `orphaned`

Signal inputs:

- inferred status from heartbeat / hooks
- recent `session_activity` deltas
- `last_progress_at`
- `last_io_at`
- assignment substatus
- heartbeat freshness

## Home-Launched / External Sessions

Home-launched or externally launched runtimes are first-class:

- registration creates or retains a lightweight session record
- wrapper launches create a lightweight assignment from launch context
- repo / branch from launch or hook metadata are stored on `agent_sessions`
- if a matching tracked branch already exists, Codero promotes the lightweight runtime into a tracked assignment
- if not, the runtime stays `inferred` or `orphaned` and can be promoted later

This keeps session history intact while improving dashboard visibility for non-ideal launch paths.

## Dashboard Semantics

The dashboard distinguishes between two standalone entities:

- `Agents`: durable launch profiles and setup metadata
- `Sessions`: live runtime instances created from those profiles

The Sessions page is the canonical runtime surface and should show explicit badges for:

- family
- launch mode
- lifecycle state
- activity state
- attachment state
- attribution source
- attribution confidence

The Agents page should focus on profile/setup concerns:

- aliases and primary alias
- installed vs missing
- tracked vs disabled
- permission profile
- home strategy / home directory
- duplicate live-instance count and coarse runtime summary

Neither page should pretend tracked certainty when only inference is available.
