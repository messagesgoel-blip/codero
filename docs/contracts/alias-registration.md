# Alias Registration Contract

**Version:** 1.0
**Task:** SET-001
**Last Updated:** 2026-04-02
**Status:** active

## Purpose

This contract defines the one-time alias registration flow that installs
Codero-managed shims for supported agent families. It replaces the
previously implicit and inconsistent guidance spread across `docs/agent-setup.md`
and `docs/contracts/agent-handling-contract.md`.

**Goal:** Every managed agent launch passes through a Codero shim so sessions
are automatically tracked, heartbeated, and registered — with zero operator
ceremony after first-time setup.

---

## Canonical Setup Command

The one-time alias registration command is:

```bash
codero setup
```

**`codero agent hooks --install` is NOT the shim installer.** That command
installs Claude Code heartbeat hooks into `~/.claude/settings.json`. It is a
separate, Claude-Code-specific step (see [Claude Code Heartbeat Hooks](#claude-code-heartbeat-hooks)).

---

## What `codero setup` Does

`codero setup` is idempotent and runs four steps:

| Step | Action | Required for shims |
|------|--------|--------------------|
| 1 | Check Docker (advisory) | No |
| 2 | Check daemon reachability (advisory) | No |
| 3 | Write `~/.codero/config.yaml` | Yes |
| 4 | Install shims for discovered agent binaries | Yes |

Steps 1 and 2 are advisory only. Docker not running or the daemon being
unreachable does not block shim installation. The operator sees a warning and
setup continues.

Steps 3 and 4 are the alias registration core. They fail only if the
filesystem is unwritable.

---

## Shim Directory

Shims are installed at:

```
~/.codero/bin/<agent-kind>
```

Examples:

```
~/.codero/bin/claude
~/.codero/bin/codex
~/.codero/bin/opencode
~/.codero/bin/copilot
~/.codero/bin/gemini
```

This directory **must appear before the real binary directories** in `$PATH`.
If it does not, `codero setup` prints the required export:

```bash
export PATH="$HOME/.codero/bin:$PATH"
```

Add this to `~/.bashrc`, `~/.zshrc`, or the managed agent profile before the
first use.

---

## Shim Template

Each shim is a bash script with this exact content:

```bash
#!/usr/bin/env bash
# Codero shim for <profile-id> — do not edit (managed by codero setup)
exec codero agent run --agent-id <profile-id> -- "/real/path/to/binary" "$@"
```

The `exec` ensures the shim has no trailing process overhead. The shim is
executable (`chmod 0755`) and is **managed by `codero setup`** — do not edit
by hand.

---

## Config File

`codero setup` writes or updates `~/.codero/config.yaml`. The schema for the
alias registration fields is:

```yaml
version: 1
daemon_addr: 127.0.0.1:8110
setup_at: 2026-04-02T00:00:00Z
wrappers:
  claude:
    agent_kind: claude
    real_binary: /usr/local/bin/claude
    installed_at: 2026-04-02T00:00:00Z
  opencode:
    agent_kind: opencode
    real_binary: /usr/local/bin/opencode
    installed_at: 2026-04-02T00:00:00Z
```

The map key is the durable profile ID used as `agent_id` in session tracking
and `codero agent run --agent-id`. Profile IDs must match the shim filename
under `~/.codero/bin/`.

---

## Supported Agent Kinds

`codero setup` discovers and registers shims for these five agent families:

| Agent kind | Binary name |
|------------|-------------|
| `claude` | `claude` |
| `codex` | `codex` |
| `opencode` | `opencode` |
| `copilot` | `copilot` |
| `gemini` | `gemini` |

Only binaries found in `$PATH` (excluding the shim dir itself) are registered.
Missing binaries are silently skipped — they do not fail setup.

---

## Idempotency Guarantees

`codero setup` is safe to rerun at any time:

| Situation | Behavior |
|-----------|----------|
| Shim exists, same binary | `unchanged` — no write, no mtime change |
| Shim exists, different binary | `updated` — shim and config entry rewritten |
| Shim does not exist | `created` — shim and config entry created |
| `--force` flag | Always rewrites, even if binary is unchanged |

The config file (`~/.codero/config.yaml`) follows the same rules: unchanged if
`daemon_addr` matches, updated if it changed, or created on first run.

---

## Manual Registration

To register an agent that `codero setup` cannot auto-discover (for example,
because it is not on `$PATH` by default), run:

```bash
codero agent run --agent-id <profile-id> -- /full/path/to/binary
```

This does not create a shim. For a persistent shim, create it manually and
register the wrapper config in `~/.codero/config.yaml`:

```bash
# Create shim manually
mkdir -p ~/.codero/bin
cat > ~/.codero/bin/myagent << 'EOF'
#!/usr/bin/env bash
# Codero shim for myagent — do not edit (managed by codero setup)
exec codero agent run --agent-id myagent -- "/full/path/to/binary" "$@"
EOF
chmod 0755 ~/.codero/bin/myagent
```

Then add the wrapper entry to `~/.codero/config.yaml` under `wrappers:`:

```yaml
wrappers:
  myagent:
    agent_kind: ""          # leave blank for unsupported kinds
    real_binary: /full/path/to/binary
    installed_at: 2026-04-02T00:00:00Z
```

---

## Claude Code Heartbeat Hooks

`codero agent hooks --install` is a **separate, Claude-Code-specific command**
that installs lifecycle hooks into `~/.claude/settings.json`:

```bash
codero agent hooks --install
```

These hooks call `codero session heartbeat` on tool use and notifications,
enabling real-time status inference (`working` / `waiting_for_input`) in the
dashboard.

This is not a shim installer. Run it **in addition to** `codero setup` when
using Claude Code:

| Command | What it does |
|---------|--------------|
| `codero setup` | Creates `~/.codero/bin/claude` shim (alias registration) |
| `codero agent hooks --install` | Installs heartbeat hooks into `~/.claude/settings.json` |

Both are needed for full Claude Code tracking.

---

## Verification

After running `codero setup`:

```bash
# 1. Verify shim dir exists and has content
ls -la ~/.codero/bin/

# 2. Verify at least one shim is installed
codero agent list --json

# 3. Verify ~/.codero/bin is first in PATH
which claude    # should show ~/.codero/bin/claude

# 4. Verify daemon reachability (if daemon is running)
curl -s http://127.0.0.1:8110/health
```

Expected `codero agent list --json` output for an installed agent:

```json
[
  {
    "agent_id": "claude",
    "agent_kind": "claude",
    "shim_name": "claude",
    "real_binary": "/usr/local/bin/claude",
    "installed": true,
    "disabled": false
  }
]
```

`"installed": true` means the real binary exists at the recorded path.

---

## Environment Variables

| Variable | Effect | Default |
|----------|--------|---------|
| `CODERO_USER_CONFIG_DIR` | Override config and shim root (`~/.codero`) | `~/.codero` |
| `CODERO_TRACKING=0` | Disable tracking for any agent launch | (unset) |
| `CODERO_DAEMON_ADDR` | Override daemon address for `codero agent run` | `127.0.0.1:8110` |

---

## Failure Modes

| Failure | Cause | Resolution |
|---------|-------|------------|
| `shim dir: create shim dir: …` | Filesystem unwritable | Check permissions on `~/.codero/` |
| `write shim: …` | Shim write failed | Check disk space and permissions |
| `save config: …` | Config write failed | Check permissions on `~/.codero/config.yaml` |
| `load config: parse: …` | Corrupt YAML | Delete or repair `~/.codero/config.yaml`, rerun |
| Shim missing from PATH | `~/.codero/bin` not in `$PATH` | Add `export PATH="$HOME/.codero/bin:$PATH"` |
| `which claude` shows wrong path | Shim dir not first in PATH | Ensure shim dir precedes other binary dirs |

Docker not running and daemon not reachable are warnings, not failures.

---

## Implementation Reference

The alias registration implementation lives in:

| File | Responsibility |
|------|---------------|
| `cmd/codero/setup_cmd.go` | `codero setup` entrypoint and shim installation |
| `internal/config/user_config.go` | `UserConfig`, `WrapperConfig`, `DiscoverAgents()` |
| `internal/config/agent_registry.go` | Durable `AgentRegistry` and `RefreshAgentRegistry()` |
| `cmd/codero/setup_cmd_test.go` | Unit tests for `installShim` and idempotency |

---

## Related Documents

- `docs/contracts/agent-handling-contract.md` — session launch, shim runtime contract
- `docs/agent-setup.md` — user-facing quickstart and per-agent setup guides
- `docs/runtime/codero-tooling-baseline.md` — shared tooling baseline (Codero-local)
- SET-002: prehook installation setup (next task after this one)
