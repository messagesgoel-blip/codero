# agtx ↔ Codero Integration Roadmap

Status: deferred — pending codero Phase 1 exit gate
Owner: sanjay
Created: 2026-03-29
Reference: https://github.com/fynnfluegge/agtx

---

## Vision

agtx handles **planning and orchestration** — task kanban, worktree provisioning,
per-phase agent assignment, and autonomous phase progression.

Codero handles **review and merge compliance** — agent session monitoring,
pre-push review gate, CodeRabbit + LiteLLM AI review, CI compliance, and merge.

The two systems are complementary and non-overlapping. The integration boundary
is the `git push`: agtx owns everything before it, codero owns everything after.

---

## Division of Responsibility

| Concern | Owner |
|---|---|
| Task creation and kanban | agtx |
| Worktree provisioning per task | agtx |
| Agent assignment per phase (research / plan / implement / review) | agtx |
| Autonomous phase progression | agtx orchestrator |
| Pre-push review gate (tests + AI diff review) | codero (hook installed by agtx plugin) |
| Fix iteration loop (push rejected → agent fixes → push again) | agtx session (Running state) |
| Post-push finish loop (CodeRabbit PR review + CI + merge) | codero-finish.sh |
| Task close after confirmed merge | codero → agtx MCP callback |

---

## Full Workflow

```text
agtx kanban
  Backlog → Planning → Running ──────────────────────────────┐
                          │                                   │
                   agent pushes                        (fix, push again)
                          │                                   │
                   [pre-push hook]                            │
                          ├── FAIL: findings printed ─────────┘
                          │         agent reads terminal,
                          │         stays in Running
                          │
                          └── PASS: push goes through
                                    │
                               [post-push hook]
                                    │
                             card → Review
                                    │
                              codero-finish.sh
                              (CodeRabbit PR review
                               + CI watch + merge)
                                    │
                              merge confirmed
                                    │
                          [agtx MCP callback]
                                    │
                             card → Done ✓
```

---

## Card State Semantics

| agtx state | What it means |
|---|---|
| Backlog | Task defined, not started |
| Planning | Agent planning approach |
| Running | Agent implementing + push/fix loop (stays here until pre-push passes) |
| Review | Push succeeded, codero running finish loop |
| Done | Merge confirmed by codero, card closes |

The card stays in **Running** for all fix iterations. Moving back to Planning
implies replanning (user-initiated via `r` key), not review-gate rework.

---

## Integration Components

### 1. agtx Plugin for Codero Workflow

A `plugin.toml` at `.agtx/plugins/codero/plugin.toml` in each managed repo.

**Responsibilities:**
- `init_script`: sets up each worktree on creation
  - Copies `.env`, `.env.local` from project root
  - Copies agent config dirs (`.claude/`, `.codex/`, `.gemini/`)
  - Installs pre-push hook into `.git/hooks/pre-push`
  - Installs post-push hook into `.git/hooks/post-push`
  - Sets `CODERO_REPO_PATH` to worktree root
- Phase commands: `/codero:plan`, `/codero:implement`, `/codero:review`
- Artifacts: `.codero/plan.md` (planning), `.codero/finish-feedback.md` (review)
- `copy_back`: syncs research/plan artifacts across worktrees if needed

**Phase → agent mapping (global `~/.config/agtx/config.toml`):**
```toml
default_agent = "claude"

[agents]
research = "gemini"
planning = "claude"
running  = "claude"
review   = "codex"
```

---

### 2. Pre-Push Hook (agtx Worktree Context)

Installed into `.git/hooks/pre-push` of each worktree by the plugin `init_script`.

**What it runs:**
1. `go test ./...` — existing test gate, unchanged
2. `two-pass-review.sh` — adapted for pre-push context

**Adaptation needed for `two-pass-review.sh`:**

The current script uses `--type uncommitted` which reviews staged/working-dir
changes. In a pre-push context the changes are committed. Required changes:

- Determine CodeRabbit CLI flag for committed-but-not-pushed diffs.
  Candidate: `--type local` or passing an explicit diff file against
  `git merge-base origin/<base> HEAD`. Requires testing against CodeRabbit CLI
  docs/help output (`coderabbit review --help`).
- Compute `CODERO_DIFF_FILES_CHANGED` and `CODERO_DIFF_LINES_CHANGED` from
  `git diff origin/<base>..HEAD --stat` rather than from staged files (which is
  how pre-commit sets them today).
- Set `CODERO_REPO_PATH` to the worktree root (already derivable from
  `git rev-parse --show-toplevel` inside the hook).

**Failure behavior:**
- Hook exits 1 → push is rejected
- Findings are printed directly to the agent's tmux pane (stdout/stderr)
- Agent reads terminal output, fixes, and pushes again
- No separate signaling needed — the terminal IS the feedback channel
- agtx card stays in Running throughout all iterations

**Rate limit note:**
The 12 calls/hour CodeRabbit rate limit is shared across all repos and all
agents (`/srv/storage/shared/coderabbit-rate.json`). With multiple agtx
worktrees pushing simultaneously, exhaustion is likely. The LiteLLM fallback
already handles this gracefully.

---

### 3. Post-Push Hook → codero-finish.sh Trigger

Installed into `.git/hooks/post-push` of each worktree by the plugin `init_script`.

**What it does:**
1. Reads the pushed branch name from `git symbolic-ref --short HEAD`
2. Checks if a PR exists for the branch (`gh pr view --json url`)
3. If no PR: creates one (`gh pr create`) with branch name and task ID in body
4. Launches `codero-finish.sh` in background with the commit SHA, branch, PR title
5. Writes PID to `.codero/finish.pid` for monitoring
6. Writes agtx task ID to `.codero/agtx-task-id` for the merge callback

**Task ID source:**
agtx worktrees are created at `.agtx/worktrees/<task-id>/`. The task ID is
derivable from the worktree path:
```bash
TASK_ID=$(basename "$(git rev-parse --show-toplevel)")
```
This assumes the worktree directory name equals the agtx task ID, which is
agtx's default behaviour.

---

### 4. Post-Merge Callback → agtx Card Close

Added to the end of `codero-finish.sh` (runs only on exit code 0 — confirmed merge).

**What it does:**
1. Read task ID from `.codero/agtx-task-id`
2. Check if `agtx` binary is available
3. Spawn `agtx serve` over stdio (JSON-RPC)
4. Call `move_task` tool: `{ "id": "<task-id>", "status": "done" }`
5. Call `get_transition_status` to confirm
6. Log result; non-fatal if agtx is unavailable (merge already happened)

**Failure handling:**
- agtx not installed: log warning, skip — merge is the primary outcome
- Task already Done: no-op
- Task ID not found: log warning, skip
- agtx serve timeout: log warning, skip

---

## Pre-Conditions Before Integration Work Begins

### Codero readiness (Phase 1 exit gate)
- [ ] 14 consecutive days daily use without manual DB repair
- [ ] Zero missed feedback deliveries
- [ ] Zero silent queue stalls
- [ ] Pre-commit enforcement via hook (not policy alone)
- [ ] Recovery drills complete

### agtx readiness
- [ ] agtx installed at `~/.local/bin/agtx`
- [ ] agtx verified working with `void` plugin on at least one repo
- [ ] agtx orchestrator (`--experimental`) tested and stable enough for daily use
- [ ] MCP `move_task` tool tested manually via `agtx serve` stdin/stdout

### Shared toolkit
- [ ] CodeRabbit CLI `--type` flag for committed diffs confirmed
  (`coderabbit review --help` or upstream docs)
- [ ] Rate limit behaviour under concurrent worktree pushes tested

---

## Implementation Phases

### Phase A — agtx Plugin (no codero changes)

Deliverables:
- `.agtx/plugins/codero/plugin.toml` with phase commands and artifacts
- `scripts/init_worktree.sh` (called by `init_script`) that:
  - Copies env files
  - Installs pre-push and post-push hook stubs (no-ops initially)
  - Writes `CODERO_REPO_PATH` to `.codero/env`
- Skill files for `/codero:plan`, `/codero:implement`, `/codero:review` phases

Exit gate: agtx creates a worktree, init_script runs without error,
phases cycle manually with hooks as no-ops.

---

### Phase B — Pre-Push Hook

Deliverables:
- Confirm CodeRabbit CLI flag for pre-push diff context
- New script: `hooks/pre-push-codero` in shared agent-toolkit
  - `go test ./...` gate
  - `two-pass-review.sh` with pre-push diff stats
  - Clear exit codes and printed feedback
- `init_worktree.sh` installs `pre-push-codero` as `.git/hooks/pre-push`
- Manual test: agent pushes → hook fires → CodeRabbit reviews committed diff →
  findings printed to terminal → agent fixes → push passes

Exit gate: full fix iteration loop tested end-to-end in a real worktree.
Card stays in Running throughout. Hook exits 0 only when review passes.

---

### Phase C — Post-Push Trigger

Deliverables:
- `hooks/post-push-codero` script installed as `.git/hooks/post-push`
  - Creates PR if none exists
  - Launches `codero-finish.sh` in background
  - Writes `.codero/agtx-task-id`
- Manual test: push passes → post-push fires → `codero-finish.sh` starts →
  CodeRabbit PR review → CI → merge confirmed

Exit gate: merge confirmed via codero after push from an agtx worktree.

---

### Phase D — Post-Merge Callback

Deliverables:
- `codero-finish.sh` exit-0 path: read task ID, call `agtx serve move_task`
- Manual test: merge confirmed → agtx card moves to Done → card disappears
- Error handling for all failure modes (no agtx, no task ID, timeout)

Exit gate: full loop end-to-end — task created in agtx, agents work, push
iterates until pre-push passes, codero merges, card closes.

---

### Phase E — Dogfood and Stabilise

- Use the full workflow for actual codero development tasks
- Identify failure modes not caught in isolated testing
- Tune rate limit behaviour (multiple concurrent worktrees)
- Document operational runbook: how to debug a stuck loop, how to manually
  close a card if the callback fails, how to restart codero-finish.sh

Exit gate: 5 tasks completed end-to-end without manual intervention.

---

## Open Questions

| Question | Notes |
|---|---|
| CodeRabbit CLI flag for committed diffs | Run `coderabbit review --help`; may be `--type local` or explicit diff input |
| agtx MCP stdio protocol details | Test `echo '{"jsonrpc":"2.0",...}' \| agtx serve` locally |
| agtx task ID = worktree dir name? | Verify from agtx source or by creating a test task |
| Multiple agents pushing simultaneously | Rate limit contention; LiteLLM fallback should cover but needs load test |
| codero-finish.sh on non-Go repos | Hook currently no-ops for non-Go; ensure review still runs |
| agtx orchestrator stability | Experimental flag — may need upstream stability before relying on it |

---

## Files Created/Modified (when implementation begins)

| File | Change |
|---|---|
| `.agtx/plugins/codero/plugin.toml` | New — agtx plugin definition |
| `.agtx/plugins/codero/skills/codero-plan/SKILL.md` | New — plan phase skill |
| `.agtx/plugins/codero/skills/codero-implement/SKILL.md` | New — implement phase skill |
| `scripts/init_worktree.sh` | New — worktree init script |
| `/srv/storage/shared/agent-toolkit/hooks/pre-push-codero` | New — adapted pre-push hook |
| `/srv/storage/shared/agent-toolkit/hooks/post-push-codero` | New — post-push trigger |
| `/srv/storage/shared/agent-toolkit/bin/codero-finish.sh` | Modified — add Phase D callback |
