# Architecture Baseline

## Intent

Codero coordinates code review work across branches and repositories with explicit
state transitions, durable persistence, and observable operations.

## Boundaries

- Durable state: canonical source of truth (initially local DB)
- Coordination layer: ephemeral queue/lease/heartbeat primitives
- Interface layer: CLI + API contracts
- Operator layer: status surfaces, alerts, runbooks

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
  webhook/          — GitHub webhook receiver and dedup (P1-S5)
  precommit/        — two-loop local review gate (P1-S5.5)
  tui/              — Bubble Tea three-pane terminal UI (P1-S6)
  version/          — build-time version constant
scripts/
  service/          — systemd unit and launchd plist
  review/           — two-pass pre-commit review gate
```

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
