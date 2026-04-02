# SES-001: Session Lifecycle Parity Implementation Plan

**Status:** ACCEPTED

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the session lifecycle contract document, contract tests, and CLI commands into full parity with the actual implementation — fixing stale schema docs, wrong function signatures, missing test coverage for `inferred_status`, `HeartbeatWithStatus`, `Finalize` with `Completion`, and the `AttachAssignment` `substatus` parameter.

**Architecture:** All changes are documentation and test additions only — no implementation code changes. The store, state layer, migrations, and CLI are already correct; only the contract document and test file need updating. One new test file covers the gaps identified in the parity analysis (inferred_status lifecycle, Finalize with Completion struct, HeartbeatWithStatus, and the `substatus` param on AttachAssignment). The contract document is rewritten in-place to reflect actual schemas and signatures.

**Tech Stack:** Go 1.25, SQLite (mattn/go-sqlite3), `internal/session`, `internal/state`, `tests/contract`

---

## Baseline

All 8 existing `TestMIG038_*` tests pass. The build is clean (`go build -buildvcs=false ./...`). Pre-existing failures in `internal/dashboard` are unrelated to this slice and must not be touched.

Run full test suite before starting and keep a mental baseline:
```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
go test ./tests/contract/... -v -run TestMIG038
```

---

## Files

| Action | Path | What changes |
|--------|------|--------------|
| Modify | `docs/contracts/session-lifecycle-contract.md` | Rewrite to match actual schemas and signatures |
| Modify | `tests/contract/session_lifecycle_contract_test.go` | Add 5 new `TestMIG038_*` tests covering gaps |

No new files. No implementation files touched.

---

## Task 1: Fix the contract document

**File:** `docs/contracts/session-lifecycle-contract.md`

Eleven concrete errors to fix, one at a time.

- [ ] **Step 1.1: Fix `agent_sessions` schema block**

Replace the stale 7-column DDL with the actual 13-column schema (from migrations 000005, 006, 014, 016, 021, 024). Change:

```sql
-- OLD (wrong)
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

To:

```sql
-- NEW (correct)
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

- [ ] **Step 1.2: Fix `agent_assignments` schema block**

Replace the stale 12-column DDL (which lists `parent_session_id`, `delivery_state`, `last_gate_result`, `last_commit_sha`, `revision_count`) with the actual columns:

```sql
-- NEW (correct)
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

Note: `parent_session_id`, `last_gate_result`, `last_commit_sha`, and `revision_count` do not exist in the actual schema. Remove them.

- [ ] **Step 1.3: Fix `session_archives` schema block**

Replace the stale schema (which has `session_id TEXT PRIMARY KEY` and `finished_at`) with the actual append-only schema (from migrations 000015, 000019):

```sql
-- NEW (correct)
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

Key differences: `archive_id` is the PK (not `session_id`), multiple rows per session are allowed (append-only since migration 000019), `repo`/`branch`/`task_id` are nullable, `finished_at` is renamed to `ended_at`.

- [ ] **Step 1.4: Fix the `Heartbeat` signature and parameter name**

The contract doc says `isClosing bool` which implies graceful shutdown semantics. The actual parameter is `markProgress bool` with progress-tracking semantics. Update the signature and description:

```text
-- OLD
err := store.Heartbeat(ctx, sessionID, secret, isClosing)
- `isClosing=true` signals graceful shutdown

-- NEW
err := store.Heartbeat(ctx, sessionID, secret, markProgress)
- `markProgress=true` refreshes last_progress_at and last_io_at (60-minute compliance rule)
```

Also correct the error var name: actual code returns `state.ErrInvalidHeartbeatSecret` (wrapped as `ErrInvalidHeartbeatSecret`), not `ErrInvalidSecret`.

- [ ] **Step 1.5: Add HeartbeatWithStatus to the contract**

This method exists in the store but is entirely absent from the contract. Add a new subsection after `### Heartbeat`:

```markdown
### HeartbeatWithStatus

Extended heartbeat that also sets `inferred_status`.

\`\`\`go
err := store.HeartbeatWithStatus(ctx, sessionID, secret, markProgress, inferredStatus)
\`\`\`

- `inferredStatus` — Canonical values: `working`, `waiting_for_input`, `idle`, `unknown`
- Aliases accepted: `pretooluse`, `posttooluse` → `working`; `waiting`, `notification` → `waiting_for_input`
- Updates `inferred_status` only if new value has equal or higher precedence than current
- CLI flag: `--status` on `codero session heartbeat`
```

- [ ] **Step 1.6: Fix the `Finalize` signature**

The contract shows `store.Finalize(ctx, sessionID, result, mergeSHA)`. The actual signature takes `agentID` and a `Completion` struct:

```text
-- OLD
err := store.Finalize(ctx, sessionID, result, mergeSHA)
- `result` — One of: merged, abandoned, failed, cancelled
- `mergeSHA` — Required when result=merged

-- NEW
err := store.Finalize(ctx, sessionID, agentID, completion)
```

Replace the Finalize section content with:

```markdown
### Finalize

Gracefully closes a session and atomically writes a `session_archives` row.

\`\`\`go
err := store.Finalize(ctx, sessionID, agentID, session.Completion{
    TaskID:     taskID,
    Status:     status,
    Substatus:  substatus,
    Summary:    summary,
    Tests:      []string{},
    FinishedAt: time.Now(),
})
\`\`\`

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
```

- [ ] **Step 1.7: Fix the `AttachAssignment` signature**

The contract shows `parentSessionID` as the last parameter. The actual last parameter is `substatus string`:

```text
-- OLD
err := store.AttachAssignment(ctx, sessionID, agentID, repo, branch, worktree, mode, taskID, parentSessionID)

-- NEW
err := store.AttachAssignment(ctx, sessionID, agentID, repo, branch, worktree, mode, taskID, substatus)
```

Update the description: `substatus` is the initial `assignment_substatus` value for the new row (e.g. `in_progress`). Remove any mention of `parentSessionID` — that column does not exist.

- [ ] **Step 1.8: Add `inferred_status` to the Session States table**

The states table only has `active`, `idle`, `archived`. Add an `inferred_status` row clarifying it is a separate dimension:

```markdown
## inferred_status Values

| Value | Description |
|-------|-------------|
| unknown | Default; no status reported yet |
| working | Agent is actively executing a tool call |
| waiting_for_input | Agent is blocked waiting for human or external input |
| idle | Agent has no active work |
```

Also add a note to the Session States table that `inferred_status` is an orthogonal column on `agent_sessions` updated by heartbeat, not a session lifecycle state.

- [ ] **Step 1.9: Fix the "idempotency" note for Register**

Contract says "Re-registering generates a new secret." Actual behavior: the existing secret is preserved on re-register (the `heartbeat_secret` is kept via `COALESCE(NULLIF(agent_sessions.heartbeat_secret, ''), excluded.heartbeat_secret)`). Fix:

```text
-- OLD
**Idempotency**: Re-registering with the same session_id updates the existing session (mode change, new secret).

-- NEW
**Idempotency**: Re-registering with the same session_id updates `agent_id`, `mode`, `last_seen_at`, and optionally `tmux_session_name`. The `heartbeat_secret` is preserved — the same secret is returned.
```

- [ ] **Step 1.10: Correct the `Confirm` postcondition**

The contract implies `Confirm` writes a `confirmed_at` timestamp. It does not — it is a pure read-validate call. Fix the description:

```text
-- OLD (implied write)
Confirm verifies that Codero has the same live session identity...

-- NEW (explicit read-only)
Confirm verifies that a live session exists and its registered agent_id matches. It is read-only — no columns are written.

**Errors**:
- `ErrSessionNotFound` — Session does not exist or already ended
- `ErrSessionMismatch` — agentID does not match registered agent
```

- [ ] **Step 1.11: Update the version/date header and contract tests table**

Update header:
```text
Version: 1.1
Last Updated: 2026-04-02
```

Add the 5 new tests to the Contract Tests table (from Task 2):
```text
| TestMIG038_HeartbeatWithStatus_UpdatesInferredStatus | HeartbeatWithStatus persists inferred_status |
| TestMIG038_Heartbeat_StatusAliasNormalization | Status aliases map to canonical values |
| TestMIG038_Finalize_WritesArchiveRow | Finalize atomically creates session_archives row |
| TestMIG038_Finalize_AgentMismatch | Finalize rejects wrong agentID |
| TestMIG038_AttachAssignment_SubstatusStored | AttachAssignment stores substatus on the assignment row |
```

- [ ] **Step 1.12: Run contract tests to confirm no regressions**

```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
go test ./tests/contract/... -v -run TestMIG038
```

Expected: all 8 existing tests still PASS (no implementation changed).

- [ ] **Step 1.13: Commit the contract doc update**

```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
gofmt -w .   # no Go files changed, but run for hygiene
git add docs/contracts/session-lifecycle-contract.md
git commit -m "docs(ses-001): update session lifecycle contract to match actual implementation

Fix schema blocks for agent_sessions (13 cols), agent_assignments (remove
parent_session_id/revision_count), and session_archives (archive_id PK,
nullable fields, ended_at not finished_at). Fix Finalize/Heartbeat/
AttachAssignment signatures. Add HeartbeatWithStatus and inferred_status
sections. Mark Confirm as read-only. Update idempotency note for Register."
```

---

## Task 2: Add missing contract tests

**File:** `tests/contract/session_lifecycle_contract_test.go`

Add 5 new `TestMIG038_*` functions at the bottom of the existing file. Each test exercises a behavior that was documented incorrectly or missing from the contract.

- [ ] **Step 2.1: Write the test for `HeartbeatWithStatus` — verify `inferred_status` is stored**

Add this test to `tests/contract/session_lifecycle_contract_test.go`:

```go
// TestMIG038_HeartbeatWithStatus_UpdatesInferredStatus tests that HeartbeatWithStatus
// persists inferred_status to the agent_sessions row.
func TestMIG038_HeartbeatWithStatus_UpdatesInferredStatus(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-inferred-status-1"
		agentID   = "agent-inferred"
	)

	secret, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Initial inferred_status should be 'unknown'
	var initialStatus string
	if err := db.Unwrap().QueryRow(
		`SELECT inferred_status FROM agent_sessions WHERE session_id = ?`, sessionID,
	).Scan(&initialStatus); err != nil {
		t.Fatalf("query initial inferred_status: %v", err)
	}
	if initialStatus != "unknown" {
		t.Errorf("initial inferred_status = %q, want unknown", initialStatus)
	}

	err = store.HeartbeatWithStatus(ctx, sessionID, secret, false, "working")
	if err != nil {
		t.Fatalf("HeartbeatWithStatus: %v", err)
	}

	var storedStatus string
	if err := db.Unwrap().QueryRow(
		`SELECT inferred_status FROM agent_sessions WHERE session_id = ?`, sessionID,
	).Scan(&storedStatus); err != nil {
		t.Fatalf("query inferred_status after heartbeat: %v", err)
	}
	if storedStatus != "working" {
		t.Errorf("inferred_status = %q, want working", storedStatus)
	}
}
```

- [ ] **Step 2.2: Write the test for status alias normalization**

```go
// TestMIG038_Heartbeat_StatusAliasNormalization tests that status aliases are
// normalized to canonical values (pretooluse → working, waiting → waiting_for_input).
func TestMIG038_Heartbeat_StatusAliasNormalization(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	cases := []struct {
		alias    string
		expected string
	}{
		{"pretooluse", "working"},
		{"posttooluse", "working"},
		{"waiting", "waiting_for_input"},
		{"notification", "waiting_for_input"},
		{"idle", "idle"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.alias, func(t *testing.T) {
			sessionID := "sess-alias-" + tc.alias
			secret, err := store.Register(ctx, sessionID, "agent-alias", "coding")
			if err != nil {
				t.Fatalf("Register: %v", err)
			}
			if err := store.HeartbeatWithStatus(ctx, sessionID, secret, false, tc.alias); err != nil {
				t.Fatalf("HeartbeatWithStatus(%q): %v", tc.alias, err)
			}
			var stored string
			if err := db.Unwrap().QueryRow(
				`SELECT inferred_status FROM agent_sessions WHERE session_id = ?`, sessionID,
			).Scan(&stored); err != nil {
				t.Fatalf("query inferred_status: %v", err)
			}
			if stored != tc.expected {
				t.Errorf("alias %q: inferred_status = %q, want %q", tc.alias, stored, tc.expected)
			}
		})
	}
}
```

- [ ] **Step 2.3: Write the test for `Finalize` writing the archive row**

```go
// TestMIG038_Finalize_WritesArchiveRow tests that Finalize atomically creates a
// session_archives row with the correct result and agent_id.
func TestMIG038_Finalize_WritesArchiveRow(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-finalize-archive"
		agentID   = "agent-finalize"
	)

	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	completion := session.Completion{
		Status:    "merged",
		Substatus: "terminal_finished",
		Summary:   "task complete",
	}
	if err := store.Finalize(ctx, sessionID, agentID, completion); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Verify session_archives row was written
	archive, err := state.GetSessionArchive(ctx, db, sessionID)
	if err != nil {
		t.Fatalf("GetSessionArchive: %v", err)
	}
	if archive.SessionID != sessionID {
		t.Errorf("archive.SessionID = %q, want %q", archive.SessionID, sessionID)
	}
	if archive.AgentID != agentID {
		t.Errorf("archive.AgentID = %q, want %q", archive.AgentID, agentID)
	}
	if archive.Result != "merged" {
		t.Errorf("archive.Result = %q, want merged", archive.Result)
	}

	// Verify agent_sessions.ended_at is set
	var endedAt *string
	if err := db.Unwrap().QueryRow(
		`SELECT ended_at FROM agent_sessions WHERE session_id = ?`, sessionID,
	).Scan(&endedAt); err != nil {
		t.Fatalf("query ended_at: %v", err)
	}
	if endedAt == nil {
		t.Error("ended_at is NULL after Finalize, want non-null")
	}
}
```

- [ ] **Step 2.4: Write the test for `Finalize` agent mismatch**

```go
// TestMIG038_Finalize_AgentMismatch tests that Finalize rejects a wrong agentID.
func TestMIG038_Finalize_AgentMismatch(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-finalize-mismatch"
		agentID   = "agent-finalize-real"
	)

	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	completion := session.Completion{
		Status: "merged",
	}
	err = store.Finalize(ctx, sessionID, "wrong-agent", completion)
	if err == nil {
		t.Fatal("expected error for wrong agentID, got nil")
	}
	if err != session.ErrSessionMismatch {
		t.Errorf("expected ErrSessionMismatch, got %v", err)
	}
}
```

- [ ] **Step 2.5: Write the test for `AttachAssignment` substatus stored**

```go
// TestMIG038_AttachAssignment_SubstatusStored tests that the substatus parameter
// is persisted to agent_assignments.assignment_substatus.
func TestMIG038_AttachAssignment_SubstatusStored(t *testing.T) {
	db := openSessionContractDB(t)
	ctx := context.Background()

	store := session.NewStore(db)

	const (
		sessionID = "sess-substatus-1"
		agentID   = "agent-substatus"
		repo      = "acme/substatus-repo"
		branch    = "feature/substatus-branch"
		taskID    = "TASK-SUB-001"
		substatus = "in_progress"
	)

	_, err := store.Register(ctx, sessionID, agentID, "coding")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Seed branch state
	if _, err := db.Unwrap().Exec(
		`INSERT INTO branch_states (id, repo, branch, state) VALUES (?, ?, ?, ?)`,
		"branch-substatus", repo, branch, "submitted",
	); err != nil {
		t.Fatalf("seed branch state: %v", err)
	}

	err = store.AttachAssignment(ctx, sessionID, agentID, repo, branch, t.TempDir(), "coding", taskID, substatus)
	if err != nil {
		t.Fatalf("AttachAssignment: %v", err)
	}

	var storedSubstatus string
	if err := db.Unwrap().QueryRow(
		`SELECT assignment_substatus FROM agent_assignments WHERE session_id = ? AND ended_at IS NULL`,
		sessionID,
	).Scan(&storedSubstatus); err != nil {
		t.Fatalf("query assignment_substatus: %v", err)
	}
	if storedSubstatus != substatus {
		t.Errorf("assignment_substatus = %q, want %q", storedSubstatus, substatus)
	}
}
```

- [ ] **Step 2.6: Run all 13 contract tests**

```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
go test ./tests/contract/... -v -run TestMIG038
```

Expected output: all 13 tests PASS. If any fail, read the error, fix the test, re-run.

- [ ] **Step 2.7: Run the full test suite to confirm no regressions**

```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
go test ./... 2>&1 | grep -E "^(ok|FAIL)"
```

Expected: same pass/fail pattern as baseline. `internal/dashboard` may still FAIL (pre-existing, unrelated). All other packages must be `ok`.

- [ ] **Step 2.8: gofmt the test file**

```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
gofmt -w tests/contract/session_lifecycle_contract_test.go
```

- [ ] **Step 2.9: Commit the new tests**

```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
git add tests/contract/session_lifecycle_contract_test.go
git commit -m "test(ses-001): add MIG-038 contract tests for inferred_status, Finalize, AttachAssignment substatus

Add TestMIG038_HeartbeatWithStatus_UpdatesInferredStatus,
TestMIG038_Heartbeat_StatusAliasNormalization,
TestMIG038_Finalize_WritesArchiveRow,
TestMIG038_Finalize_AgentMismatch,
TestMIG038_AttachAssignment_SubstatusStored.
Covers the 5 parity gaps identified in SES-001 analysis."
```

---

## Task 3: Finish and open PR

- [ ] **Step 3.1: Verify git log**

```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
git log --oneline -5
```

Expected: two new commits on branch `ses-001` (or your working branch name).

- [ ] **Step 3.2: Push the branch**

```bash
cd /srv/storage/repo/codero/.worktrees/ses-001
git push -u origin ses-001
```

- [ ] **Step 3.3: Open PR and trigger CodeRabbit review**

```bash
gh pr create \
  --title "ses-001: session lifecycle parity — contract doc and tests" \
  --body "$(cat <<'EOF'
## Summary

- Updates `docs/contracts/session-lifecycle-contract.md` to match actual implementation: fixes `agent_sessions` (13 cols), `agent_assignments` (remove phantom columns), `session_archives` (append-only, `archive_id` PK), `Finalize`/`Heartbeat`/`AttachAssignment` signatures, adds `HeartbeatWithStatus` section, `inferred_status` values, marks `Confirm` as read-only, fixes idempotency note for `Register`.
- Adds 5 new `TestMIG038_*` contract tests covering `HeartbeatWithStatus`, status alias normalization, `Finalize` archive row, `Finalize` agent mismatch, and `AttachAssignment` substatus persistence.
- No implementation code changes.

## Tests

All 13 `TestMIG038_*` tests pass. Full suite baseline unchanged.

Closes SES-001.
EOF
)"
```

Then post CodeRabbit review trigger:

```bash
gh pr comment <PR_NUMBER> --body "@coderabbitai review"
```

- [ ] **Step 3.4: Monitor CI and CodeRabbit, address feedback**

Watch CI with:
```bash
/srv/storage/shared/agent-toolkit/bin/ci-watch.sh ses-001 1200 $(git rev-parse HEAD)
```

Address any CodeRabbit comments. When all checks pass and CodeRabbit approves, use `codero-finish.sh` or merge manually per the team workflow.

---

## Checklist Summary

| # | Task | Done |
|---|------|------|
| 1.1 | Fix agent_sessions schema | - [ ] |
| 1.2 | Fix agent_assignments schema | - [ ] |
| 1.3 | Fix session_archives schema | - [ ] |
| 1.4 | Fix Heartbeat signature (markProgress, error var) | - [ ] |
| 1.5 | Add HeartbeatWithStatus section | - [ ] |
| 1.6 | Fix Finalize signature | - [ ] |
| 1.7 | Fix AttachAssignment signature (substatus) | - [ ] |
| 1.8 | Add inferred_status values table | - [ ] |
| 1.9 | Fix Register idempotency note | - [ ] |
| 1.10 | Fix Confirm as read-only | - [ ] |
| 1.11 | Update version/date + test table | - [ ] |
| 1.12 | Run contract tests — 8 pass | - [ ] |
| 1.13 | Commit contract doc | - [ ] |
| 2.1 | Add HeartbeatWithStatus test | - [ ] |
| 2.2 | Add status alias normalization test | - [ ] |
| 2.3 | Add Finalize archive row test | - [ ] |
| 2.4 | Add Finalize agent mismatch test | - [ ] |
| 2.5 | Add AttachAssignment substatus test | - [ ] |
| 2.6 | Run 13 contract tests — all pass | - [ ] |
| 2.7 | Run full suite — no new failures | - [ ] |
| 2.8 | gofmt test file | - [ ] |
| 2.9 | Commit tests | - [ ] |
| 3.1 | Verify git log | - [ ] |
| 3.2 | Push branch | - [ ] |
| 3.3 | Open PR + trigger CodeRabbit | - [ ] |
| 3.4 | Monitor CI + address feedback | - [ ] |
