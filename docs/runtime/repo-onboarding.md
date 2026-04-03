# Repo Onboarding: Shared Tooling and OpenClaw Baseline

Status: active
Task: TOOL-005
Date: 2026-04-02

## Purpose

This document is the onboarding reference for any repo adopting the shared
tooling and OpenClaw baseline established in Codero's Wave 0 (`TOOL-001`
through `TOOL-004`). It answers the question: "What does a repo need to do to
run on the shared tooling baseline, and what does OpenClaw look like in that
context?"

**This is a documentation artifact.** It does not change shared tooling
behavior, does not enforce anything globally, and does not claim that non-primary
repos have already migrated.

Use this document as the starting checklist when a new repo wants to:

- Use shared tool binaries instead of per-repo installs
- Run an OpenClaw adapter with the Codero privilege and plugin baseline
- Wire the approved PTY bridge for session delivery
- Know what exceptions are allowed and how to register them

---

## Scope and Non-Scope

**In scope:**

- Shared tool binary paths and how to resolve them
- Shared env bootstrap expectations
- OpenClaw config shape (gateway, auth, workspace, plugins, provider)
- Approved wrappers under `~/.codero/bin`
- PTY bridge usage via `agent-tmux-bridge`
- Exception handling when a repo cannot meet the baseline
- A dry-run validation checklist against a staging or non-primary repo

**Not in scope:**

- Codero daemon lifecycle, queue, or state machine
- Shared tooling global enforcement (no change to `/srv/storage/shared/tools/`)
- Forcing adoption on non-primary repos without an explicit setup task
- Anything that is repo-specific policy drift by default

---

## Prerequisites

Before onboarding, verify:

1. The shared tooling root exists:
   - `/srv/storage/shared/tools/bin/` (shared tool binaries)
   - `/srv/storage/shared/agent-toolkit/bin/` (shared toolkit scripts)
   - `/srv/shared/agent-env.sh` (env bootstrap)

2. The shared memory is readable:
   - `/srv/storage/shared/memory/MEMORY.md`
   - `/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md`

3. A Codero daemon is reachable on `127.0.0.1:8110` if session registration
   is required.

4. `jq` is available in the PATH (required by the plugin validation script).

Run the tooling baseline validation first to confirm (see the
[Dry-Run Validation Checklist](#dry-run-validation-checklist) section below).

---

## Shared Tooling Baseline

### Shared Env Bootstrap

All managed launches source the shared env bootstrap:

```bash
/srv/shared/agent-env.sh
```

This file sets:

- `PATH` to include `/srv/storage/shared/tools/bin`
- Shared cache locations (`GOMODCACHE`, `npm_config_cache`, etc.)
- `PIP_REQUIRE_VIRTUALENV=1` to block ad hoc `pip install --user`

**Repos must not install tools locally.** All tool resolution goes through
the shared PATH established by `agent-env.sh`.

Override the bootstrap path with:

```bash
CODERO_SHARED_ENV_BOOTSTRAP=/path/to/agent-env.sh
```

### Shared Tool Binaries

| Path | Mandatory | Purpose |
|------|-----------|---------|
| `/srv/storage/shared/tools/bin/agent-tmux-bridge` | Yes | PTY session transport |
| `/srv/storage/shared/agent-toolkit/bin/gate-heartbeat` | Yes | Review gate entrypoint |
| `/srv/storage/shared/agent-toolkit/bin/codero-finish.sh` | Yes | Autonomous finish-loop |
| `/srv/storage/shared/agent-toolkit/bin/install-hooks` | Optional | Hook installation |
| `/srv/storage/shared/agent-toolkit/bin/ci-watch.sh` | Optional | CI watcher after push |

Do not copy these binaries into a repo. They resolve through the shared PATH.

### Shared Caches

| Cache | Path | Mandatory for Go repos |
|-------|------|----------------------|
| Go module cache | `/srv/shared/go-mod-cache` | Yes |
| pip cache | `/srv/shared/pip-cache` | No |
| npm cache | `/srv/shared/npm-cache` | No |
| semgrep cache | `/srv/shared/semgrep-cache` | No |

Go repos must set `GOMODCACHE=/srv/shared/go-mod-cache` or rely on the env
bootstrap to set it. Failing to use the shared Go module cache causes build
failures in worktrees because `go build ./...` cannot resolve the bare-control
repo structure cleanly without VCS stamping disabled.

Correct build invocation from a worktree:

```bash
go build -buildvcs=false ./...
```

Or via Make:

```bash
make build
```

### Shared Virtual Environments

Shared Python tooling lives in dedicated venvs. Repos must use these instead
of per-agent or per-repo installs:

| Venv | Path | Tools |
|------|------|-------|
| `tooling` | `/srv/storage/shared/tools/venvs/tooling` | ruff, poetry, semgrep, pre-commit |
| `aider` | `/srv/storage/shared/tools/venvs/aider` | aider |
| `pr-agent` | `/srv/storage/shared/tools/venvs/pr-agent` | pr-agent |

Use `PIP_REQUIRE_VIRTUALENV=1` (set by the env bootstrap) to prevent accidental
system-level installs.

---

## OpenClaw Config Shape

Every repo that uses the shared OpenClaw baseline must configure OpenClaw with
the following shape. Deviations require an exception (see
[Exception Handling](#exception-handling)).

### Config File Location

The config file is resolved by:

1. `OPENCLAW_CONFIG_PATH` env var (if set)
2. Default: `$HOME/.openclaw-codero-smoke/openclaw.json`

### Required Config Shape

```json
{
  "meta": {
    "lastTouchedVersion": "<pinned-version>"
  },
  "gateway": {
    "bind": "loopback",
    "port": 18789,
    "auth": {
      "mode": "token"
    }
  },
  "agents": {
    "defaults": {
      "workspace": "$HOME/.openclaw-<repo>-smoke/workspace"
    }
  },
  "models": {
    "providers": {
      "litellm": {
        "baseUrl": "http://localhost:4000"
      }
    }
  },
  "plugins": {
    "entries": {
      "litellm": {
        "enabled": true,
        "config": {}
      },
      "acpx": {
        "enabled": true,
        "config": {}
      }
    }
  }
}
```

### Gateway

| Field | Required value | Reason |
|-------|---------------|--------|
| `gateway.bind` | `"loopback"` | No external network exposure |
| `gateway.auth.mode` | `"token"` | Token-gated access only |

Do not bind to `0.0.0.0` or any non-loopback address on a shared host.

### Workspace

The workspace must be isolated under the OpenClaw state root, not shared
with the Codero persistence directories (`CODERO_DB_PATH`, Redis). Use a
path that includes the repo name to avoid collision with other workspaces:

```text
$HOME/.openclaw-<repo-name>-smoke/workspace
```

### Plugins

**Allowlist only.** Only two plugins are approved for the baseline:

| Plugin | Purpose |
|--------|---------|
| `litellm` | Model routing through local LiteLLM proxy at `localhost:4000` |
| `acpx` | Codero adapter integration (session register, heartbeat, submit) |

All other plugins must be disabled. Do not enable bundled upstream plugins
that are not on this list. For the full rationale, see
`docs/runtime/openclaw-plugin-policy.md`.

### Model Provider

Model requests route through the local LiteLLM proxy:

```text
http://localhost:4000
```

Direct upstream provider credentials must not appear in the OpenClaw config.
LiteLLM holds the API keys and provides the routing abstraction.

### Forbidden Config Fields

The following must never appear in an OpenClaw config file:

| Field | Reason |
|-------|--------|
| `GITHUB_TOKEN` | Codero owns GitHub authority |
| `CODERO_DB_PATH` | Codero owns durable state |
| `CODERO_REDIS_ADDR` or `CODERO_REDIS_PASS` | Codero owns coordination state |
| Any agent-family auth credential | Belongs in the agent runtime shell only |

For the full privilege profile, see `docs/runtime/openclaw-privilege-profile.md`.

---

## Approved Wrappers

Codero uses profile-based wrappers under `~/.codero/bin` as the source of
truth for installed agent profiles. These wrappers are created by the Codero
CLI during initial alias registration (see `SET-001` for the one-time alias
registration task).

Each wrapper:

- Is a shim at `~/.codero/bin/<agent-family>` (e.g., `claude`, `opencode`)
- Calls `codero agent run --agent-id <profile-id> -- <real-binary> "$@"`
- Is managed by the Codero CLI — do not edit by hand
- Is removed when the alias is unregistered (aliases belong to the profile)

**Shell profiles must not hardcode `.bashrc` aliases** as an alternative to
the wrapper mechanism. `.bashrc` aliases are not a supported agent discovery
source.

Verify wrapper installation:

```bash
ls -la ~/.codero/bin/
codero agent list --json
```

---

## Bridge Usage

The shared PTY bridge is the **only approved path** for launching and
communicating with managed agent sessions. Its binary is at:

```text
/srv/storage/shared/tools/bin/agent-tmux-bridge
```

Supported commands:

| Command | Purpose |
|---------|---------|
| `start-profile <family> <session-name>` | Launch a managed session |
| `wait-ready <session-name>` | Block until the agent is ready |
| `deliver <session-name> <message>` | Inject a message into the session |
| `capture <session-name>` | Capture current PTY output |
| `status <session-name>` | Check session busy/idle state |
| `stop <session-name>` | Stop the session cleanly |

**Must not use:**

- Raw `tmux send-keys` (bypasses bridge state tracking)
- Arbitrary process launch outside approved wrappers
- PTY injection outside the `deliver` command

The bridge handles per-family busy-state detection and interrupt wrapping.
For interrupt behavior and proven smoke tokens, see
`/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md`.

---

## Exception Handling

Not every repo will match the baseline exactly. Exceptions are allowed but
must be registered.

### What Counts as an Exception

| Situation | Exception type |
|-----------|---------------|
| Repo uses a custom LiteLLM endpoint | Config exception |
| Repo adds a third plugin beyond the approved two | Plugin exception |
| Repo binds gateway to a non-default port | Config exception |
| Repo cannot use the shared Go module cache | Cache exception |
| Repo needs a tool not in `/srv/storage/shared/tools/bin` | Tool exception |
| Repo uses a non-standard PTY session naming convention | Bridge exception |

### How to Register an Exception

1. **Document the exception** in the repo's `AGENTS.md` under a
   `## Tooling Exceptions` section.

2. **State the reason** — why the baseline cannot be met for this repo.

3. **State the scope** — which check or policy does not apply.

4. **State the owner** — who is responsible for this exception.

5. **Note the review date** — when the exception should be revisited.

Example entry in `AGENTS.md`:

```markdown
## Tooling Exceptions

| Exception | Reason | Scope | Owner | Review date |
|-----------|--------|-------|-------|-------------|
| LiteLLM endpoint `localhost:4001` | Dedicated LiteLLM for this repo | Config | sanjay | 2026-07-01 |
```

### What Is Not an Exception

The following are hard requirements with no exception path:

- Gateway must be loopback-only (security boundary, not configurable)
- `GITHUB_TOKEN` must not appear in OpenClaw config (privilege boundary)
- `CODERO_DB_PATH` and `CODERO_REDIS_*` must not appear in OpenClaw config
- PTY injection must go through the bridge (no raw `tmux send-keys`)

---

## Dry-Run Validation Checklist

Use this checklist when onboarding a new repo or verifying a non-primary
repo against the baseline. Run it before adding the repo to the critical
path or enabling session delivery for it.

This checklist is designed to be run from a staging worktree (a fresh
worktree that is not the primary development surface).

### Phase 1: Shared Tooling

```bash
# 1. Verify shared env bootstrap exists and is readable
test -f "${CODERO_SHARED_ENV_BOOTSTRAP:-/srv/shared/agent-env.sh}" \
  && echo "PASS: env bootstrap exists" \
  || echo "FAIL: env bootstrap missing"

# 2. Verify shared tool bin
test -d /srv/storage/shared/tools/bin \
  && echo "PASS: shared tools bin exists" \
  || echo "FAIL: shared tools bin missing"

# 3. Verify PTY bridge is executable
test -x /srv/storage/shared/tools/bin/agent-tmux-bridge \
  && echo "PASS: PTY bridge executable" \
  || echo "FAIL: PTY bridge not executable"

# 4. Verify gate-heartbeat
test -x /srv/storage/shared/agent-toolkit/bin/gate-heartbeat \
  && echo "PASS: gate-heartbeat executable" \
  || echo "FAIL: gate-heartbeat not executable"

# 5. Verify codero-finish.sh
test -x /srv/storage/shared/agent-toolkit/bin/codero-finish.sh \
  && echo "PASS: codero-finish.sh executable" \
  || echo "FAIL: codero-finish.sh not executable"

# 6. Verify Go module cache (Go repos only)
test -d "${GOMODCACHE:-/srv/shared/go-mod-cache}" \
  && echo "PASS: Go mod cache exists" \
  || echo "FAIL: Go mod cache missing"

# 7. Verify shared memory
test -f /srv/storage/shared/memory/MEMORY.md \
  && echo "PASS: shared memory exists" \
  || echo "FAIL: shared memory missing"
```

Or run the automated version:

```bash
scripts/validate-tooling-baseline.sh
```

### Phase 2: OpenClaw Config

Set `OPENCLAW_CONFIG_PATH` to the target repo's config before running:

```bash
export OPENCLAW_CONFIG_PATH="$HOME/.openclaw-<repo>-smoke/openclaw.json"
```

Then run:

```bash
# Privilege profile checks (gateway, auth, forbidden creds)
scripts/validate-openclaw-privileges.sh

# Plugin allowlist checks
scripts/validate-openclaw-plugins.sh

# Update readiness checks
scripts/validate-openclaw-update-readiness.sh
```

All three Phase 2 validators must complete with zero `FAIL` results before the
repo is considered onboarded for the OpenClaw baseline. Phase 2 validators may
also emit heuristic `WARN` results; those warnings are recorded for attention
and should be reviewed and resolved, but they do not count as failures and do
not block the onboarding pass criteria.

### Phase 3: Wrapper and Session Path

```bash
# Verify Codero wrappers exist for expected agent families
ls ~/.codero/bin/

# Verify at least one wrapper is registered
codero agent list --json | grep '"installed": true' \
  && echo "PASS: at least one wrapper installed" \
  || echo "FAIL: no installed wrappers"

# Verify daemon is reachable
curl -s http://127.0.0.1:8110/health | grep -q '"status":"ok"' \
  && echo "PASS: daemon healthy" \
  || echo "FAIL: daemon not reachable"
```

### Phase 4: Bridge Smoke Test (Optional but Recommended)

Run a low-risk bridge smoke against the staging session:

```bash
# Pick a non-primary session name for the smoke
SESSION="onboarding-smoke-$(date +%s)"
FAMILY="opencode"  # or another approved family

# Start a session
/srv/storage/shared/tools/bin/agent-tmux-bridge start-profile "$FAMILY" "$SESSION"

# Wait for ready
/srv/storage/shared/tools/bin/agent-tmux-bridge wait-ready "$SESSION"

# Deliver a test token
/srv/storage/shared/tools/bin/agent-tmux-bridge deliver "$SESSION" \
  "Onboarding smoke test. Reply with: ONBOARDING_SMOKE_OK"

# Capture and verify
/srv/storage/shared/tools/bin/agent-tmux-bridge capture "$SESSION"

# Stop session
/srv/storage/shared/tools/bin/agent-tmux-bridge stop "$SESSION"
```

The session must receive the delivery and produce a recognizable response.
Failure here indicates a bridge path issue, not an OpenClaw config issue.

### Onboarding Verdict

| Phase | Result | Meaning |
|-------|--------|---------|
| Phase 1 all pass | Shared tooling ready | Proceed to Phase 2 |
| Phase 2 all pass | OpenClaw config compliant | Proceed to Phase 3 |
| Phase 3 all pass | Wrappers and daemon ready | Proceed to Phase 4 |
| Phase 4 pass | Bridge smoke confirmed | Repo is onboarded |
| Any phase failure | Investigate before proceeding | See Troubleshooting |

---

## Troubleshooting

### Shared tool not found

```bash
# Check PATH includes shared tools bin
echo "$PATH" | grep -q /srv/storage/shared/tools/bin \
  && echo "shared tools bin in PATH" \
  || echo "shared tools bin NOT in PATH — source agent-env.sh"

# Manual source if needed
source /srv/shared/agent-env.sh
```

### go build fails in worktree

```bash
# Disable VCS stamping for bare-repo worktrees
go build -buildvcs=false ./...

# Or use make
make build
```

### OpenClaw config validation fails: gateway not loopback

Check the `gateway.bind` field in your config:

```bash
jq '.gateway.bind' "$OPENCLAW_CONFIG_PATH"
```

Must return `"loopback"`. If not, correct the config before running the
privilege validation.

### OpenClaw config validation fails: unapproved plugin

Check which plugins are enabled:

```bash
jq '.plugins.entries | to_entries[] | select(.value.enabled == true) | .key' \
  "$OPENCLAW_CONFIG_PATH"
```

Only `litellm` and `acpx` should appear. Disable any others and re-run
`scripts/validate-openclaw-plugins.sh`.

### Bridge smoke fails: session not ready

The `wait-ready` command timed out. Check that:

1. The `tmux` session was created: `tmux ls | grep "$SESSION"`
2. The agent family is one of the approved families:
   `claude`, `codex`, `opencode`, `copilot`, `gemini`
3. No previous session with the same name is running

### Codero daemon not reachable

```bash
# Check if daemon is running
curl http://127.0.0.1:8110/health

# If not, start it
codero daemon start

# Or check docker
docker ps | grep codero
```

---

## Relationship to Other Runtime Docs

| Doc | What it covers | Relationship to this doc |
|-----|---------------|--------------------------|
| `docs/runtime/codero-tooling-baseline.md` | Codero-specific baseline audit | Source of truth for Codero's own tooling claims |
| `docs/runtime/openclaw-privilege-profile.md` | OpenClaw authority and forbidden ops | Defines what OpenClaw may and may not do |
| `docs/runtime/openclaw-plugin-policy.md` | Plugin allowlist and approval process | Defines which plugins are permitted |
| `docs/runtime/openclaw-update-policy.md` | Update cadence and change-control | Defines when and how to update the baseline |

This document synthesizes those four into an actionable onboarding checklist.
The individual docs remain the source of truth for their respective areas.

---

## Related Documents

- Execution roadmap: `docs/roadmaps/dogfood-execution-roadmap.md`
- Strategic roadmap: `docs/roadmap.md`
- Tooling baseline: `docs/runtime/codero-tooling-baseline.md`
- Privilege profile: `docs/runtime/openclaw-privilege-profile.md`
- Plugin policy: `docs/runtime/openclaw-plugin-policy.md`
- Update policy: `docs/runtime/openclaw-update-policy.md`
- Agent setup: `docs/agent-setup.md`
- Shared memory: `/srv/storage/shared/memory/MEMORY.md`
- OpenClaw PTY notes: `/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md`
- Repo agent policy: `AGENTS.md`
