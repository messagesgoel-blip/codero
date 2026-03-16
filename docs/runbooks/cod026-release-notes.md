# Release Notes — COD-026: TUI Command Polish and Web Port Hardening

**Branch:** `feat/COD-026-tui-commands-and-web-port-hardening`
**Target:** v1.1.x readiness

---

## New Commands

### `codero tui` — Canonical Interactive Operator Shell

Replaces the `gate-status --watch` workaround as the recommended TUI entry point.

```bash
codero tui                              # full 3-pane TUI (gate / queue / events)
codero tui --view gate --interval 3     # gate view, 3s refresh
codero tui --theme dracula              # dark variant
codero tui --no-alt-screen              # tmux / non-alt-screen terminals
```

Flags: `--repo-path/-r`, `--interval`, `--theme` (dark/light/system/dracula/vscode),
`--view` (gate/queue/events/output/findings), `--no-alt-screen`

Requires an interactive TTY. In CI or piped contexts the command exits immediately
with an informative error instead of corrupting terminal output.

### `codero dashboard` — Dashboard URL and Health Check

```bash
codero dashboard                        # print effective dashboard URL and endpoints
codero dashboard --check                # validate /dashboard/, overview API, /gate (exit 1 on failure)
codero dashboard --open                 # open in default browser (interactive local envs only)
codero dashboard --port 9090            # override port for check
```

The `--check` sub-command is suitable for integration in deploy pipelines as a
post-deploy smoke test.

### `codero ports` — Network Binding Diagnostics

```bash
codero ports
```

Prints all configured network addresses with URLs for running codero services.
Detects and warns on `observability_port` / `webhook.port` conflicts.

---

## Command Improvements

### `gate-status` — New Flags

| Flag | Behaviour |
|------|-----------|
| `--json` | Emit gate status as compact JSON (no TUI, no prompt, scriptable) |
| `--no-prompt` | Disable interactive action prompt even in a TTY |

In non-interactive contexts (pipe, CI, `--no-prompt`): exits 1 on FAIL, 0 on PASS/PENDING.
The old ad-hoc TTY check (`os.Stdin.Stat()`) is replaced by the new centralised
`tui.IsInteractiveTTY()` helper, which checks both stdin and stdout.

---

## Web Port / Routing Hardening

### New Config Fields

```yaml
# codero.yaml additions
observability_host: ""               # bind address; default "" = all interfaces
dashboard_base_path: /dashboard      # URL prefix for SPA; default /dashboard
dashboard_public_base_url: ""        # override external URL (reverse-proxy use)
```

Environment variable equivalents:
```text
CODERO_OBSERVABILITY_HOST
CODERO_DASHBOARD_BASE_PATH
CODERO_DASHBOARD_PUBLIC_BASE_URL
```

### Reverse Proxy Support

The dashboard SPA and all `/api/v1/dashboard/*` API routes are now served under
the configured `dashboard_base_path`. No hardcoded paths remain in the static
HTML. Example nginx config:

```nginx
location /codero/ {
    proxy_pass http://127.0.0.1:8080/;
}
```

With `dashboard_base_path: /codero/dashboard` and `dashboard_public_base_url: https://ops.example.com/codero`.

---

## Internal Changes

- `internal/tui/tty.go` — new `IsInteractiveTTY()` helper using `os.ModeCharDevice` check
- `internal/tui/app.go` — `Config.InitialTab` field for view selection
- `internal/tui/theme.go` — `Theme.Name` string field for testability
- `internal/daemon/observability.go` — `NewObservabilityServer` accepts `host` and `dashboardBasePath`

---

## Tests Added

| Package | Tests |
|---------|-------|
| `cmd/codero` | `printGateStatusJSON` (pass + fail), theme resolution, tab resolution, `runDashboardCheck` (healthy / down / partial), `portsCmd` output and conflict warning |
| `internal/config` | New COD-026 config fields, `DashboardBasePath` validation, env overrides |
| `internal/daemon` | Observability server base-path routing (default + custom), bind address, old-path 404 under custom path |
| `internal/tui` | `IsInteractiveTTY` returns false in test (non-TTY) context |

---

## Rollback

```bash
git revert <merge-commit-sha>
```

This removes all new command files (`tui_cmd.go`, `port_dashboard_cmds.go`),
reverts `tui_commands.go` to pre-COD-026 (removing `--json`/`--no-prompt`),
and reverts config/observability server to the COD-025 state.

No database migration is required and none is introduced.
