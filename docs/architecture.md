# Architecture Baseline

## Intent

Codero coordinates code review work across branches and repositories with explicit
state transitions, durable persistence, and observable operations.

## Boundaries

- Durable state: canonical source of truth (initially local DB)
- Coordination layer: ephemeral queue/lease/heartbeat primitives
- Interface layer: CLI + API contracts + web dashboard
- Operator layer: dashboard, CLI status commands, alerts, runbooks

## Initial Runtime Model

- Single daemon process
- Single operator deployment
- Contract-first module intake from prior systems

## Constraint

No capability is imported without contract definition and parity tests.

## Package Layout (P1-S1-01)

```
cmd/codero/         — binary entrypoint: daemon, status, version subcommands
internal/
  config/           — env-var config loader (file-based config: P1-S1-02)
  daemon/           — PID file, signal handling, Redis health-check goroutine
  redis/            — ONLY permitted Redis entrypoint; key builder, script registry, client wrapper
  state/            — branch lifecycle state machine (P1-S1-03)
  scheduler/        — WFQ dispatch loop and lease issuance (P1-S3)
  delivery/         — append-only inbox.log and seq-number assignment (P1-S4)
  delivery_pipeline/ — submit-to-merge FSM orchestrator (MIG-037)
  session/          — session lifecycle management (MIG-038)
  webhook/          — GitHub webhook receiver and dedup (P1-S5)
  precommit/        — two-loop local review gate (P1-S5.5)
  version/          — build-time version constant
scripts/
  service/          — systemd unit and launchd plist
  review/           — two-pass pre-commit review gate
```

## Delivery Pipeline (MIG-037)

The delivery pipeline orchestrates the submit-to-merge flow:

```
idle → staging → gating → committing → pushing → pr_management → monitoring → feedback_delivery → merge_evaluation → merging → post_merge → idle
```

Key behaviors:
- **Lock file**: `delivery.lock` prevents concurrent submits (returns `ErrPipelineBusy` / HTTP 409)
- **Gate check**: Must pass before commit
- **Feedback**: Written to `FEEDBACK.md` and `feedback/current.json` on gate/push failure
- **Recovery**: Lock always released, state always returns to idle

Contract: `docs/contracts/delivery-pipeline-contract.md`

## Session Lifecycle (MIG-038)

Sessions track agent engagement:

1. **Register** — Create session with session_id, agent_id, mode
2. **Heartbeat** — Update last_seen_at, validate secret
3. **AttachAssignment** — Link to repo/branch (requires branch_state row)
4. **Finalize** — Archive session with result

Key behaviors:
- **Tmux support**: `RegisterWithTmux` stores tmux session name for reattachment
- **PTY bot delivery**: external bot shells injecting messages into live agent
  sessions must follow `docs/contracts/bot-pty-delivery-contract.md`
- **Idempotency**: Re-register updates existing session
- **Lazy assignment**: Branch state must exist before attach

Contract: `docs/contracts/session-lifecycle-contract.md`

## Redis Command Policy

**All Redis commands must go through `internal/redis`.**
No package outside `internal/redis` may construct raw Redis key strings or
create a `*redis.Client` directly. Use `redislib.New()` for clients and
`redislib.BuildKey()` for all key construction.

Key format: `codero:<repo>:<type>:<id>`
- `repo`  — GitHub owner/repo slug (e.g. `acme/api`)
- `type`  — coordination primitive (e.g. `lease`, `queue`, `heartbeat`)
- `id`    — branch name, event ID, or other identifier

Lua scripts for atomic operations are registered via `ScriptRegistry.Load()`
on daemon startup and referenced by name constant (never by inline string).
