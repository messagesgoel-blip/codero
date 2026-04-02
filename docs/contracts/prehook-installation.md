# Prehook Installation Contract

**Version:** 1.0
**Task:** SET-002
**Last Updated:** 2026-04-02
**Status:** active

## Purpose

This contract defines the one-time prehook installation flow that installs
Codero-managed lifecycle hooks into Claude Code's settings file. It formalizes
`codero agent hooks --install` as a durable, idempotent operation — analogous
to how `docs/contracts/alias-registration.md` (SET-001) formalized shim
installation.

**Goal:** Claude Code agents fire heartbeat callbacks on every tool use and
notification, so the Codero dashboard can infer real-time agent status
(`working` / `waiting_for_input`) without polling.

---

## Canonical Install Command

The one-time prehook installation command is:

```bash
codero agent hooks --install
```

**This is NOT the shim installer.** `codero setup` installs shims. These are
two distinct operations — both are needed for full Claude Code tracking:

| Command | What it does |
|---------|--------------|
| `codero setup` | Creates `~/.codero/bin/claude` shim (alias registration) |
| `codero agent hooks --install` | Installs heartbeat hooks into `~/.claude/settings.json` |

---

## Agent Family Scope

Prehook installation currently targets **Claude Code only**. The hooks are
written into `~/.claude/settings.json`, which is Claude Code's native settings
file. Other agent families (`codex`, `opencode`, `copilot`, `gemini`) do not
have an equivalent hook system and are not addressed by this command.

---

## Target File

```
~/.claude/settings.json
```

The file is created if it does not exist. The parent directory (`~/.claude/`)
is also created if missing. Non-hook keys already present in the file are
preserved — the command merges only the `hooks` key.

---

## Hook Types

Three hook types are installed:

| Hook type | Trigger | Heartbeat status |
|-----------|---------|-----------------|
| `PreToolUse` | Before any Claude Code tool executes | `working` |
| `PostToolUse` | After any Claude Code tool executes | `working` |
| `Notification` | When Claude Code raises a user notification | `waiting_for_input` |

Each hook calls `codero session heartbeat` with the appropriate `--status`
flag. The generated JSON structure is:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "codero session heartbeat --status=working --progress"}
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "codero session heartbeat --status=working --progress"}
        ]
      }
    ],
    "Notification": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "codero session heartbeat --status=waiting_for_input"}
        ]
      }
    ]
  }
}
```

Use `codero agent hooks --print` to print this JSON to stdout without writing
any file.

---

## Idempotency Guarantees

`codero agent hooks --install` is safe to rerun at any time:

| Situation | Behavior |
|-----------|----------|
| Settings file does not exist | `created` — file and parent dir created |
| Settings file exists, hooks already identical | `unchanged` — no write, no mtime change |
| Settings file exists, hooks differ | `updated` — hooks section rewritten, other keys preserved |
| `--force` flag provided | Always rewrites, even if hooks are identical → `updated` |

The status (`created` / `updated` / `unchanged`) is printed to stderr:

```
Hooks created to /home/user/.claude/settings.json
Hooks updated to /home/user/.claude/settings.json
Hooks unchanged to /home/user/.claude/settings.json
```

---

## `--force` Flag

Pass `--force` to unconditionally reinstall the hooks regardless of current
state:

```bash
codero agent hooks --install --force
```

Use this after a Claude Code upgrade that may have reset `settings.json`, or
when debugging hook delivery issues.

---

## Config Recording

After a `created` or `updated` install, the installation is recorded in
`~/.codero/config.yaml` under the `hooks` key:

```yaml
hooks:
  claude:
    settings_path: /home/user/.claude/settings.json
    installed_at: 2026-04-02T00:00:00Z
```

The map key is the agent family (`claude`). `settings_path` is the absolute
path of the settings file that was written. `installed_at` is the UTC timestamp
of the most recent `created` or `updated` install.

`unchanged` installs do not update this record.

---

## Verification

After running `codero agent hooks --install`:

```bash
# 1. Check the hooks key exists in Claude Code settings
cat ~/.claude/settings.json | python3 -m json.tool | grep -A5 '"hooks"'

# 2. Verify installation was recorded in Codero config
cat ~/.codero/config.yaml | grep -A3 'hooks:'

# 3. Print the canonical hook JSON for comparison
codero agent hooks --print

# 4. Verify repo hook/tool wiring end to end
(cd /srv/storage/repo/codero && /srv/storage/shared/agent-toolkit/bin/install-hooks --verify)
```

Expected `~/.codero/config.yaml` excerpt after install:

```yaml
hooks:
  claude:
    settings_path: /home/user/.claude/settings.json
    installed_at: 2026-04-02T00:00:00Z
```

---

## How This Differs from `codero setup`

| Dimension | `codero setup` | `codero agent hooks --install` |
|-----------|---------------|-------------------------------|
| What it installs | Shims at `~/.codero/bin/` | Hooks in `~/.claude/settings.json` |
| Config recorded under | `wrappers.<agent-id>` | `hooks.claude` |
| Agent families | All 5 supported families | Claude Code only |
| Required for session tracking | Yes — shim routes launches through Codero | Yes — hooks fire heartbeats during sessions |
| Safe to rerun | Yes (idempotent) | Yes (idempotent) |
| Force flag | `--force` | `--force` |

Both commands are independent and must both be run for full Claude Code
tracking.

---

## Environment Variables

| Variable | Effect | Default |
|----------|--------|---------|
| `HOME` | Determines `~/.claude/settings.json` target path | OS home directory |
| `CODERO_USER_CONFIG_DIR` | Overrides `~/.codero/` root for config recording | `~/.codero` |

---

## Failure Modes

| Failure | Cause | Resolution |
|---------|-------|------------|
| `parse existing settings at …` | `~/.claude/settings.json` contains invalid JSON | Fix or delete the file, then rerun |
| `read settings: …` | Settings file exists but is unreadable | Check permissions on `~/.claude/settings.json` |
| `ensure settings dir: …` | `~/.claude/` directory could not be created | Check permissions on `~` |
| `write settings: …` | Settings file could not be written | Check disk space and file permissions |
| `load user config: …` | `~/.codero/config.yaml` is corrupt | Delete or repair `~/.codero/config.yaml`, rerun |
| `save user config: …` | Config write failed | Check permissions on `~/.codero/` |

---

## Implementation Reference

| File | Responsibility |
|------|---------------|
| `cmd/codero/agent_hooks.go` | `codero agent hooks` entrypoint, `installClaudeHooks`, `generateClaudeHooks` |
| `cmd/codero/agent_hooks_test.go` | Unit tests for `installClaudeHooks` and `generateClaudeHooks` |
| `internal/config/user_config.go` | `UserConfig`, `HooksConfig`, `LoadUserConfig()`, `Save()` |

---

## Related Documents

- `docs/contracts/alias-registration.md` — SET-001: shim installation contract
- `docs/agent-setup.md` — user-facing quickstart and per-agent setup guides
- `docs/contracts/agent-handling-contract.md` — session launch and shim runtime contract
- SES-001 and beyond: session runtime improvements
