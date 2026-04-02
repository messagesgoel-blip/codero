# Session Lifecycle Contract

**Version:** 1.1
**Last Updated:** 2026-04-02
**MIG Reference:** MIG-038

## Overview

Sessions represent an agent's active engagement with Codero. The session lifecycle manages registration, heartbeat, assignment attachment, and archival.

## Session States

| State | Description |
|-------|-------------|
| active | Session is currently running |
| idle | Session registered but no active work |
| archived | Session has been finalized and archived |

Note: `inferred_status` is an orthogonal column on `agent_sessions` updated via heartbeat — it is not a session lifecycle state. See [inferred_status Values](#inferred_status-values) below.

## Core Operations

### Register

Creates a new session with a unique session ID and agent ID.

```go
secret, err := store.Register(ctx, sessionID, agentID, mode)
```

- `sessionID` — Unique identifier for this session
- `agentID` — Identifier for the agent process
- `mode` — Operating mode: `coding`, `review`, `planning`
- Returns: `secret` — Heartbeat authentication token

**Idempotency**: Re-registering with the same session_id updates `agent_id`, `mode`, `last_seen_at`, and optionally `tmux_session_name`. The `heartbeat_secret` is preserved — the same secret is returned.

### RegisterWithTmux

Extended registration for tmux-attached sessions.

```go
secret, err := store.RegisterWithTmux(ctx, sessionID, agentID, mode, tmuxSessionName)
```

- Stores `tmux_session_name` for later retrieval
- Used for session reattachment after coordinator restart
- Provides the continuity anchor used by external bot-shell PTY delivery; see
  `docs/contracts/bot-pty-delivery-contract.md`

### Confirm

Verifies that a live session exists and its registered agent_id matches. It is read-only — no columns are written.

```go
err := store.Confirm(ctx, sessionID, agentID)
```

**Errors**:
- `ErrSessionNotFound` — Session does not exist or already ended
- `ErrSessionMismatch` — agentID does not match registered agent

### Heartbeat

Updates `last_seen_at` timestamp and validates session ownership.

```go
err := store.Heartbeat(ctx, sessionID, secret, markProgress)
```

- Validates `secret` matches the session
- Updates `last_seen_at` to current time
- `markProgress=true` refreshes `last_progress_at` and `last_io_at` (60-minute compliance rule)

**Errors**:
- `ErrSessionNotFound` — Session does not exist
- `ErrInvalidHeartbeatSecret` — Secret does not match

### HeartbeatWithStatus

Extended heartbeat that also sets `inferred_status`.

```go
err := store.HeartbeatWithStatus(ctx, sessionID, secret, markProgress, inferredStatus)
```

- `inferredStatus` — Canonical values: `working`, `waiting_for_input`, `idle`, `unknown`
- Aliases accepted: `pretooluse`, `posttooluse` → `working`; `waiting`, `notification` → `waiting_for_input`
- Updates `inferred_status` only if new value has equal or higher precedence than current
- CLI flag: `--status` on `codero session heartbeat`

### AttachAssignment

Links a session to a repo/branch assignment.

```go
err := store.AttachAssignment(ctx, sessionID, agentID, repo, branch, worktree, mode, taskID, substatus)
```

- `substatus` — Initial `assignment_substatus` value for the new row (e.g. `in_progress`)

**Preconditions**:
- Session must exist
- Branch state row must exist (returns `ErrBranchNotFound` otherwise)

**Postconditions**:
- Creates `agent_assignments` row
- Updates `branch_states.owner_session_id` and `branch_states.owner_agent`

### Finalize

Gracefully closes a session and atomically writes a `session_archives` row.

```go
err := store.Finalize(ctx, sessionID, agentID, session.Completion{
    TaskID:     taskID,
    Status:     status,
    Substatus:  substatus,
    Summary:    summary,
    Tests:      []string{},
    FinishedAt: time.Now(),
})
```

- `agentID` — Must match the session's registered agent_id
- `completion.Status` — Required. Terminal result string (e.g. `merged`, `failed`, `cancelled`, `ended`)
- `completion.Substatus` — Optional substatus (e.g. `terminal_finished`, `terminal_lost`)
- `completion.FinishedAt` — If zero, defaults to `time.Now().UTC()`

**Errors**:
- `ErrSessionNotFound` — Session does not exist or already ended
- `ErrSessionMismatch` — agentID does not match
- Rule-check errors — RULE-001 (gate must pass), RULE-002 (no silent failure), RULE-003 (branch hold TTL), RULE-004 (heartbeat progress)

**Postconditions**:
- Sets `agent_sessions.ended_at` and `end_reason`
- Closes active assignment with terminal state/substatus
- Clears `branch_states.owner_session_id` and `owner_agent`
- Writes `session_archives` row atomically in the same transaction

## Database Schema

### agent_sessions

```sql
CREATE TABLE agent_sessions (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    ended_at DATETIME,
    end_reason TEXT NOT NULL DEFAULT '',
    last_progress_at DATETIME,
    tmux_session_name TEXT NOT NULL DEFAULT '',
    heartbeat_secret TEXT NOT NULL DEFAULT '',
    last_io_at DATETIME,
    inferred_status TEXT NOT NULL DEFAULT 'unknown',
    inferred_status_updated_at DATETIME
);
```

### agent_assignments

```sql
CREATE TABLE agent_assignments (
    assignment_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES agent_sessions(session_id),
    agent_id TEXT NOT NULL,
    repo TEXT NOT NULL,
    branch TEXT NOT NULL,
    worktree TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'active',
    blocked_reason TEXT NOT NULL DEFAULT '',
    assignment_substatus TEXT NOT NULL DEFAULT '',
    assignment_version INTEGER NOT NULL DEFAULT 1,
    delivery_state TEXT NOT NULL DEFAULT 'idle',
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    end_reason TEXT NOT NULL DEFAULT '',
    superseded_by TEXT
);
```

### session_archives

```sql
CREATE TABLE session_archives (
    archive_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    task_id TEXT,
    repo TEXT,
    branch TEXT,
    result TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL,
    duration_seconds INTEGER DEFAULT 0,
    commit_count INTEGER DEFAULT 0,
    merge_sha TEXT,
    task_source TEXT,
    archived_at TEXT
);
```

Note: `session_archives` is append-only (since migration 000019). Multiple rows per `session_id` are allowed. `archive_id` is the primary key.

## inferred_status Values

| Value | Description |
|-------|-------------|
| unknown | Default; no status reported yet |
| working | Agent is actively executing a tool call |
| waiting_for_input | Agent is blocked waiting for human or external input |
| idle | Agent has no active work |

## Contract Tests

Location: `tests/contract/session_lifecycle_contract_test.go`

| Test | Description |
|------|-------------|
| TestMIG038_TmuxHeartbeat_StoresSessionName | RegisterWithTmux persists tmux session name |
| TestMIG038_SessionArchival_ArchiveRowExists | ArchiveSession creates archive row correctly |
| TestMIG038_LazyAssignment_RequiresBranchState | AttachAssignment fails without branch state |
| TestMIG038_AttachAssignment_UpdatesBranchState | AttachAssignment updates owner info |
| TestMIG038_Heartbeat_UpdatesLastSeen | Heartbeat updates last_seen_at timestamp |
| TestMIG038_Heartbeat_InvalidSecret | Heartbeat rejects invalid secret |
| TestMIG038_Confirm_VerifiesSession | Confirm validates session-agent match |
| TestMIG038_RegisterIdempotent | Re-register updates existing session |
| TestMIG038_HeartbeatWithStatus_UpdatesInferredStatus | HeartbeatWithStatus persists inferred_status |
| TestMIG038_Heartbeat_StatusAliasNormalization | Status aliases map to canonical values |
| TestMIG038_Finalize_WritesArchiveRow | Finalize atomically creates session_archives row |
| TestMIG038_Finalize_AgentMismatch | Finalize rejects wrong agentID |
| TestMIG038_AttachAssignment_SubstatusStored | AttachAssignment stores substatus on the assignment row |

## Invariants

1. **Session Uniqueness**: Each session_id maps to exactly one agent process
2. **Assignment Ownership**: An assignment belongs to exactly one session
3. **Branch Ownership**: A branch can be owned by at most one session at a time
4. **Heartbeat Secret**: The secret is cryptographically random and cannot be guessed
5. **Archive Immutability**: Once archived, session data cannot be modified

## Recovery

### Coordinator Restart

1. Query `agent_sessions` for active sessions
2. Check `last_seen_at` against timeout threshold
3. For tmux sessions, verify tmux process still exists
4. Clean up orphaned sessions via Finalize

### Session Timeout

Default timeout: 5 minutes without heartbeat

- Mark session as `archived` with result `failed`
- Release any owned branches
- Delivery lock cleanup is not performed by `ExpireAgentSession`; that timeout
  handler only archives the session and clears branch ownership.
- Worktree lock removal currently happens in delivery execution cleanup such as
  the deferred `Unlock(worktree)` path in `internal/delivery_pipeline/pipeline.go`
  and in stale-lock cleanup, not in the session timeout handler itself.
