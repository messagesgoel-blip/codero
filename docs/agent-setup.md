# Agent Setup Guide

This guide covers setting up AI agent tracking for Codero. Supported agents: Claude Code, Aider, Cursor, and generic CLI wrappers.

---

## Quick Start

```bash
# Install codero CLI
go install github.com/codero/codero/cmd/codero@latest

# Run one-time setup: creates shims and writes ~/.codero/config.yaml
codero setup

# Add shim directory to PATH (add to ~/.bashrc or ~/.zshrc)
export PATH="$HOME/.codero/bin:$PATH"

# Verify installation
codero agent list --json
```

---

## Claude Code Setup

### 1. Install Shim

Run the one-time setup command to create all supported agent shims:

```bash
codero setup
```

This creates a shim at `~/.codero/bin/claude` (and shims for other supported agents) that wraps the real binary.

> **Note:** `codero agent hooks --install` installs Claude Code *heartbeat hooks* into `~/.claude/settings.json`. It does **not** create shims. These are two distinct operations — `codero setup` for shims, `codero agent hooks --install` for heartbeat hooks.

### 2. Install Claude Code Heartbeat Hooks (Claude Code only)

For Claude Code's built-in hook system (PreToolUse / PostToolUse / Notification), run:

```bash
codero agent hooks --install
```

This writes hook entries into `~/.claude/settings.json` so Claude Code calls back to the daemon on tool use events.

### 3. Configure Claude Code to Use Shim

Add to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.):

```bash
# Use codero-managed Claude Code
export PATH="$HOME/.codero/bin:$PATH"
```

Or use the full path in your workflow:
```bash
~/.codero/bin/claude --repo-path /path/to/repo
```

### 4. Verify Heartbeat Flow

Start a session and verify heartbeats are recorded:

```bash
# Start a tracked session
~/.codero/bin/claude --repo-path /srv/storage/repo/codero/.worktrees/main

# In another terminal, check sessions
codero agent list
```

Expected output:
```json
[
  {
    "session_id": "...",
    "agent_id": "claude",
    "inferred_status": "working",
    "elapsed_sec": 120,
    "last_seen_at": "2026-03-30T12:00:00Z",
    "installed": true
  }
]
```

### 5. Dashboard Verification

Open the dashboard at `http://127.0.0.1:8110/dashboard`:
- **Agents tab**: Shows Claude Code as "installed"
- **Sessions tab**: Shows active session with status
- Heartbeat updates every 2 minutes (or on tool use)

---

## Aider Setup

### 1. Install Shim

```bash
codero setup --agent-id aider --binary $(which aider)
```

### 2. Configure Shell

```bash
export PATH="$HOME/.codero/bin:$PATH"
```

### 3. Verify

```bash
codero agent list --json | grep aider
```

---

## Cursor Setup

Cursor requires manual hook installation since it doesn't have a CLI mode.

### 1. Create Wrapper Script

Create `~/.codero/bin/cursor`:

```bash
#!/usr/bin/env bash
# Codero shim for Cursor
exec codero agent run --agent-id cursor -- "cursor" "$@"
```

### 2. Make Executable

```bash
chmod +x ~/.codero/bin/cursor
```

### 3. Register in Config

Edit `~/.codero/config.yaml`:

```yaml
version: 1
daemon_addr: 127.0.0.1:8110
wrappers:
  cursor:
    real_binary: /usr/bin/cursor
    installed_at: 2026-03-30T12:00:00Z
```

---

## Generic CLI Agent Setup

For any CLI-based agent:

### 1. Create Shim

```bash
codero setup --agent-id myagent --binary /path/to/agent
```

### 2. Verify Shim

Check the generated shim:

```bash
cat ~/.codero/bin/myagent
```

Expected content:
```bash
#!/usr/bin/env bash
# Codero shim for myagent — do not edit (managed by codero setup)
exec codero agent run --agent-id myagent -- "/path/to/agent" "$@"
```

---

## Hook Types

Codero uses three lifecycle hooks for status inference:

| Hook | Trigger | Status |
|------|---------|--------|
| `PreToolUse` | Before tool execution | `working` |
| `PostToolUse` | After tool execution | `working` |
| `Notification` | User notification shown | `waiting_for_input` |

Generate hooks for your agent:

```bash
codero agent hooks --print
```

This outputs JSON configuration for Claude Code hooks.

---

## Troubleshooting

### Agent Not Showing as Installed

```bash
# Check shim exists
ls -la ~/.codero/bin/

# Check real binary path
cat ~/.codero/bin/claude

# Verify binary exists
ls -la /srv/storage/shared/tools/bin/claude

# Reinstall shim
codero setup --force
```

### No Heartbeats Received

1. Check daemon is running:
   ```bash
   curl http://127.0.0.1:8110/health
   ```

2. Verify session registration:
   ```bash
   codero agent list
   ```

3. Check hooks are configured:
   ```bash
   codero agent hooks --print
   ```

4. Verify agent is using shim:
   ```bash
   which claude  # Should show ~/.codero/bin/claude
   ```

### Dashboard Shows No Agents

The dashboard merges DB roster with discovered shims. If no agents appear:

```bash
# Check tracking config API
curl http://127.0.0.1:8110/api/v1/dashboard/tracking-config

# Verify config file
cat ~/.codero/config.yaml
```

---

## Verification Checklist

Use this checklist to verify P1-B tasks:

- [ ] **P1-B-001**: `codero agent list --json` returns ≥1 agent with `installed: true`
- [ ] **P1-B-002**: Running a test session creates heartbeat entries
- [ ] **P1-B-003**: This document exists at `docs/agent-setup.md`

### Automated Verification

```bash
#!/bin/bash
# verify-agent-setup.sh

echo "=== P1-B-001: Agent shims installed ==="
INSTALLED=$(codero agent list --json 2>/dev/null | grep -c '"installed": true')
if [ "$INSTALLED" -gt 0 ]; then
    echo "✓ Found $INSTALLED installed agent(s)"
else
    echo "✗ No installed agents found"
    exit 1
fi

echo ""
echo "=== P1-B-002: Heartbeat flow ==="
SESSIONS=$(curl -s http://127.0.0.1:8110/api/v1/dashboard/sessions | jq '.sessions | length')
if [ "$SESSIONS" -gt 0 ]; then
    echo "✓ Found $SESSIONS session(s) in dashboard"
else
    echo "⚠ No sessions found (run a test session)"
fi

echo ""
echo "=== Dashboard Agents API ==="
curl -s http://127.0.0.1:8110/api/v1/dashboard/agents | jq '.agents | length'
echo "agents discovered"
```

---

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CODERO_USER_CONFIG_DIR` | Config directory | `~/.codero` |
| `CODERO_DAEMON_ADDR` | Daemon address | `127.0.0.1:8110` |
| `CODERO_AGENT_ID` | Agent ID for current session | (from shim) |

---

## See Also

- `AGENTS.md` — Repository-specific agent policy
- `docs/roadmap.md` — P1-B task definitions
- `docs/runtime/repo-onboarding.md` — Shared tooling and OpenClaw baseline onboarding
- `codero agent --help` — CLI reference
