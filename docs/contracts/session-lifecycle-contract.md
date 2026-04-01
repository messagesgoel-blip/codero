# Session Lifecycle Contract

**Version:** 1.0
**Last Updated:** 2026-03-26
**MIG Reference:** MIG-038

## Overview

Sessions represent an agent's active engagement with Codero. The session lifecycle manages registration, heartbeat, assignment attachment, and archival.

## Session States

| State | Description |
|-------|-------------|
| active | Session is currently running |
| idle | Session registered but no active work |
| archived | Session has been finalized and archived |

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

**Idempotency**: Re-registering with the same session_id updates the existing session (mode change, new secret).

### RegisterWithTmux

Extended registration for tmux-attached sessions.

```go
secret, err := store.RegisterWithTmux(ctx, sessionID, agentID, mode, tmuxSessionName)
```

- Stores `tmux_session_name` for later retrieval
- Used for session reattachment after coordinator restart
- Provides the continuity anchor used by external bot-shell PTY delivery; see
  `docs/contracts/bot-pty-delivery-contract.md`

### Heartbeat

Updates `last_seen_at` timestamp and validates session ownership.

```go
err := store.Heartbeat(ctx, sessionID, secret, isClosing)
```

- Validates `secret` matches the session
- Updates `last_seen_at` to current time
- `isClosing=true` signals graceful shutdown

**Errors**:
- `ErrSessionNotFound` — Session does not exist
- `ErrInvalidSecret` — Secret does not match

### AttachAssignment

Links a session to a repo/branch assignment.

```go
err := store.AttachAssignment(ctx, sessionID, agentID, repo, branch, worktree, mode, taskID, parentSessionID)
```

**Preconditions**:
- Session must exist
- Branch state row must exist (returns `ErrBranchNotFound` otherwise)

**Postconditions**:
- Creates `agent_assignments` row
- Updates `branch_states.owner_session_id` and `branch_states.owner_agent`

### Finalize

Gracefully closes a session and optionally creates an archive.

```go
err := store.Finalize(ctx, sessionID, result, mergeSHA)
```

- `result` — One of: `merged`, `abandoned`, `failed`, `cancelled`
- `mergeSHA` — Required when `result=merged`

**Preconditions**:
- Assignment must have passing gate (RULE-001)

## Database Schema

### agent_sessions

```sql
CREATE TABLE agent_sessions (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    mode TEXT NOT NULL,
    started_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    tmux_session_name TEXT,
    heartbeat_secret TEXT NOT NULL
);
```

### agent_assignments

```sql
CREATE TABLE agent_assignments (
    assignment_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    repo TEXT NOT NULL,
    branch TEXT NOT NULL,
    worktree TEXT NOT NULL,
    task_id TEXT,
    parent_session_id TEXT,
    delivery_state TEXT DEFAULT 'idle',
    last_gate_result TEXT,
    last_commit_sha TEXT,
    revision_count INTEGER DEFAULT 0
);
```

### session_archives

```sql
CREATE TABLE session_archives (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    repo TEXT NOT NULL,
    branch TEXT NOT NULL,
    task_id TEXT,
    result TEXT NOT NULL,
    merge_sha TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL
);
```

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
