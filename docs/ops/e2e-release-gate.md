# E2E Release Gate

**Added in COD-036.** Run the E2E gate as a mandatory pre-promotion check
before tagging any test release. A clean pass is required before proceeding to
Phase 2 of the [release checklist](../runbooks/release-checklist-template.md).

---

## When to run

- Before tagging a test release from `main`.
- After merging any change to `cmd/codero/`, `internal/daemon/`, or
  `internal/config/`.
- As part of CI gate validation (see Phase 1.E2E in the release checklist).

---

## Commands

All commands run from the repo root (`/srv/storage/repo/codero`).

```bash
# E2E gate — standard pass (required)
go test -tags=e2e -count=1 ./tests/e2e/ -v -timeout 60s

# E2E gate — race detector pass (required before tagging)
go test -tags=e2e -race -count=1 ./tests/e2e/ -v -timeout 60s

# Full unit suite — must remain green alongside E2E
go test -count=1 ./...
go test -race ./...
```

---

## Test inventory

| Test function | Sub-tests | What it proves |
|---|---|---|
| `TestVersion_NoEnv_E2E` | — | Fast-mode: `codero version` exits 0 with no env and no services |
| `TestStatusStalePID_E2E` | — | Stale PID detection: dead PID surfaces clear error, never "running" |
| `TestDaemonLifecycle_E2E` | `status_before_start` | `codero status` reports "not running" before daemon starts |
| | `status_while_running` | `codero status` reports "running" + "redis: ok" while daemon is up |
| | `redis_commands_received` | Daemon issued at least one Redis command to miniredis |
| | `db_initialized` | SQLite DB file created and non-empty (migrations ran) |
| | `observability_health` | `GET /health` returns HTTP 200 while daemon is up |
| | `observability_ready` | `GET /ready` returns HTTP 200 (Redis reachable) |
| | `pid_file_removed` | PID file deleted on clean SIGTERM shutdown |
| | `status_after_shutdown` | `codero status` reports "not running" after shutdown |
| `TestDaemonRestart_E2E` | `run1_ready` | First daemon run reaches ready state |
| | `run2_ready` | Second daemon start after clean stop also reaches ready state |
| | `run2_pid_removed` | Second shutdown removes PID file cleanly |

---

## Environment contract

The E2E tests are fully self-contained:

- **Redis**: [miniredis](https://github.com/alicebob/miniredis) runs in-process.
  No external Redis required.
- **GitHub token**: A placeholder token (`ghp-e2e-test-placeholder`) is
  injected automatically. `CODERO_SKIP_GITHUB_SCOPE_CHECK=true` is set to
  bypass the live GitHub API scope-check. No real GitHub API calls are made.
- **Observability port**: A free ephemeral port on `127.0.0.1` is allocated per
  test run to prevent port conflicts.
- **Build tag**: The `e2e` build tag is required. Without it, these tests do
  not compile or run, so they cannot interfere with unit test runs.

---

## Expected outcomes

| Outcome | Meaning |
|---|---|
| All `PASS` | E2E gate clear — proceed with release |
| Any `FAIL` | Release blocker — fix defect before promoting |
| Build failure | Release blocker — `cmd/codero` does not compile |
| `SKIP` (tag absent) | Expected when running `go test ./...` without `-tags=e2e` |

---

## Pass/fail semantics per test

- **`TestVersion_NoEnv_E2E`**: FAIL if `codero version` exits non-zero or
  produces no output. Indicates a broken CLI entry point.
- **`TestStatusStalePID_E2E`**: FAIL if stale PID reports exit 0 or does not
  contain "stale" in output. Indicates operators cannot detect dead daemon.
- **`TestDaemonLifecycle_E2E`**: FAIL if any sub-test fails. The most common
  failure modes are:
  - Daemon does not start within 10 s (startup timeout exceeded)
  - PID file not removed after SIGTERM (daemon does not clean up)
  - `/health` or `/ready` unreachable (observability server not binding)
- **`TestDaemonRestart_E2E`**: FAIL if second daemon start fails or does not
  reach ready state. Indicates PID file ownership is not handed off cleanly.

---

## Defects discovered by E2E gate (COD-036)

| Defect | Fix | Commit |
|---|---|---|
| PID file not removed on clean SIGTERM shutdown | Added `defer daemon.RemovePID(cfg.PIDFile)` in `daemonCmd` after successful `WritePID` | COD-036 |
