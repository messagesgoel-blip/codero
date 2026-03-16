# Dashboard Runbook

## Overview

The Codero web dashboard provides a real-time browser view of the review
orchestration system. It is served by the existing codero daemon observability
server (default port 8080).

---

## Running the Dashboard Locally

### Prerequisites

| Tool | Version |
|------|---------|
| Go | 1.22+ |
| Redis | 6+ (or use `docker compose up redis`) |

### 1. Start Redis (if not already running)

```bash
docker compose up -d redis
```

### 2. Configure codero

Create or edit `codero.yaml`:

```yaml
github_token: $CODERO_GITHUB_TOKEN
repos:
  - your-org/your-repo
redis:
  addr: localhost:6379
db_path: /tmp/codero-local.db
observability_port: 8080
log_level: info
```

### 3. Start the daemon

```bash
go run ./cmd/codero daemon --config codero.yaml
```

### 4. Open the dashboard

```
http://localhost:8080/dashboard/
```

The dashboard polls every 5 seconds by default. The live badge in the top-right
corner indicates connection state.

---

## Build

No separate frontend build step is required. The dashboard static files are
embedded in the Go binary at compile time via `//go:embed static` in
`internal/dashboard/static_embed.go`.

```bash
# Build the binary (includes dashboard assets)
go build -o codero ./cmd/codero

# Verify the binary serves the dashboard
./codero daemon --config codero.yaml &
curl -sI http://localhost:8080/dashboard/ | head -5
```

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CODERO_DB_PATH` | (from config) | SQLite DB path; also used to derive settings file directory |
| `CODERO_REPO_PATH` | CWD | Repo path for gate progress file lookup |
| `CODERO_OBSERVABILITY_PORT` | `8080` | Port for the observability + dashboard server |
| `CODERO_OBSERVABILITY_HOST` | `""` (all interfaces) | Bind address for the observability server. Set to `127.0.0.1` for loopback-only. |
| `CODERO_DASHBOARD_BASE_PATH` | `/dashboard` | URL prefix for the dashboard SPA. Change for reverse-proxy deployments. |
| `CODERO_DASHBOARD_PUBLIC_BASE_URL` | `""` | External base URL (overrides address shown by `codero dashboard` and `codero ports`). |

### Port and Base-Path Configuration Examples

**Default (local dev):**
```yaml
observability_port: 8080
# dashboard available at http://localhost:8080/dashboard/
```

**Loopback-only (production, behind a reverse proxy):**
```yaml
observability_host: "127.0.0.1"
observability_port: 8080
dashboard_public_base_url: "https://ops.example.com"
```

**Custom base path (reverse proxy serves at /codero/ui/):**
```yaml
dashboard_base_path: /codero/ui
dashboard_public_base_url: "https://ops.example.com"
# dashboard available at https://ops.example.com/codero/ui/
```

**Check configuration quickly:**
```bash
codero dashboard --check
codero ports
```

---

## Dashboard API

All endpoints return `Content-Type: application/json`.

```bash
BASE=http://localhost:8080/api/v1/dashboard

# Overview metrics
curl $BASE/overview

# Repo list
curl $BASE/repos

# Recent runs
curl $BASE/runs

# Activity feed
curl $BASE/activity

# Block reasons (ranked error sources)
curl $BASE/block-reasons

# Gate health (pass rates by provider)
curl $BASE/gate-health

# Settings (read)
curl $BASE/settings

# Settings (update)
curl -X PUT $BASE/settings \
  -H 'Content-Type: application/json' \
  -d '{"integrations":[{"id":"coderabbit","name":"CodeRabbit","desc":"AI code review","connected":true}],"gate_pipeline":[]}'

# Manual review upload
curl -X POST $BASE/manual-review-upload \
  -F 'file=@fix-auth.go' \
  -F 'repo=acme/api'

# SSE event stream
curl -N $BASE/events
```

---

## Settings Persistence

Settings are stored as JSON alongside the SQLite database:

```
$(dirname $CODERO_DB_PATH)/dashboard-settings.json
```

To reset settings to defaults, delete this file and restart the daemon (or just
reload the settings page — defaults are served when the file is absent).

---

## Troubleshooting

### Dashboard shows "offline" badge

1. Check daemon is running: `curl http://localhost:8080/health`
2. Check DB is accessible: the `/health` endpoint reports Redis + DB status
3. Check logs: `tail -f $CODERO_LOG_PATH`

### No data in overview / runs table

The dashboard only shows real data from the state store. If no runs have been
dispatched, all counters will be zero or `—`. Run `codero register` on a branch
or push to a tracked repo to generate activity.

### Settings not persisting after restart

Verify write access to the directory containing `codero.yaml`/the DB path. The
settings file is written atomically; partial writes cannot corrupt it.

### SSE stream immediately closes

This typically means the reverse proxy (nginx, etc.) is buffering the response.
Ensure `X-Accel-Buffering: no` is honoured by your proxy, or use polling-only
mode (the client falls back automatically).

---

## Rollback Procedure

The dashboard feature is additive — it adds new routes to the existing
observability server and a new database table is not required (all queries
are against existing tables).

To revert:
1. Revert the `feat/COD-025-dashboard` branch changes
2. Rebuild and redeploy the binary
3. Existing `/health`, `/queue`, `/metrics`, `/gate` endpoints are unaffected

The settings file (`dashboard-settings.json`) can be left in place or deleted
without affecting daemon operation.

---

## Deployment Implications

- **Port:** No new port is required. Dashboard is co-served on the existing
  observability port.
- **Disk:** Settings file is typically < 4 KB.
- **Memory:** Embedded static assets (~35 KB) are loaded into the binary.
- **Database:** No schema migration is required (queries against existing tables).
- **Backwards compatibility:** All existing observability endpoint contracts are
  unchanged.
