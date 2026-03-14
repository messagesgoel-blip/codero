# Runbook: prompy Observability Sidecar

**Component:** `services/prompy`
**Task:** COD-013
**Metrics port:** `:9210/metrics`
**Alert rules:** `services/prompy/alerts.yml`
**Dashboard:** `services/prompy/dashboard.json`

---

## Table of Contents

1. [Start](#start)
2. [Validate](#validate)
3. [Troubleshoot](#troubleshoot)
   - [queue_stalled_for_10m](#alert-coderoqueuestalledfor10m)
   - [high_queue_wait_p95](#alert-coderohighqueuewaitp95)
   - [missing_prompy_data](#alert-coderomissingprompydata)
4. [Rollback](#rollback)

---

## Start

### Prerequisites

- Docker and Docker Compose installed.
- A running Redis instance reachable from the container network.
- (Optional) codero daemon running with HTTP endpoints.

### Steps

```bash
cd services/prompy

# 1. Create environment file.
cp .env.example .env
# Edit .env:
#   REDIS_ADDR=redis:6379
#   REDIS_PASSWORD=<your-password>      # leave blank if none
#   CODERO_BASE_URL=http://codero:8080  # optional; leave blank for Redis-only mode
#   DOCKER_NETWORK=codero-net           # must match your existing codero network

# 2. Ensure the Docker network exists.
docker network inspect codero-net >/dev/null 2>&1 \
  || docker network create codero-net

# 3. Build and start.
docker compose up -d --build

# 4. Tail logs to confirm startup.
docker compose logs -f prompy
```

Expected log output:
```text
2026-03-14T10:00:00 INFO prompy.server: metrics: listening on 0.0.0.0:9210/metrics
2026-03-14T10:00:00 INFO prompy.collector: starting; interval=15s stall_threshold=600s ...
```

---

## Validate

### 1. `/metrics` endpoint responds

```bash
curl -s http://localhost:9210/metrics | head -30
```

### 2. Required metric series are present

```bash
curl -s http://localhost:9210/metrics | grep -E \
  "^(codero_queue_depth|codero_queue_stalled|codero_queue_wait_seconds|codero_review_cycle_seconds|codero_merge_ready_lead_seconds|codero_poll_failures_total|codero_last_success_timestamp_seconds)"
```

Expected (example):
```text
codero_last_success_timestamp_seconds 1.741952400e+09
codero_poll_failures_total{source="redis"} 0
codero_queue_depth{repo="owner/repo"} 2
codero_queue_stalled{repo="owner/repo"} 0
# HELP codero_queue_wait_seconds ...
# TYPE codero_queue_wait_seconds histogram
codero_queue_wait_seconds_bucket{repo="owner/repo",le="1.0"} 0
...
```

### 3. Prometheus scrape target is UP

In the Prometheus UI (`http://prometheus:9090/targets`), confirm:

- Job: `codero-prompy`
- State: **UP**
- Last scrape: within the last 15 seconds

Or via API:
```bash
curl -s 'http://prometheus:9090/api/v1/targets' | \
  python3 -c "import sys,json; [print(t['scrapeUrl'], t['health']) for t in json.load(sys.stdin)['data']['activeTargets'] if 'prompy' in t['scrapeUrl']]"
```

### 4. Run unit tests

```bash
cd services/prompy
PYTHONPATH=/srv/shared/python-packages python3 -m pytest tests/ -v
```

All tests should pass.

### 5. Import Grafana dashboard

1. In Grafana, go to **Dashboards → Import**.
2. Upload `services/prompy/dashboard.json`.
3. Select your Prometheus datasource when prompted.
4. Click **Import**.
5. Confirm all panels load without errors.

---

## Troubleshoot

### Alert: `CoderoQueueStalledFor10m`

**Symptoms:** `codero_queue_stalled{repo="..."}` == 1 for ≥ 10 minutes.

**Causes:**
1. Codero daemon crashed or was restarted and has not resumed leasing.
2. Redis connectivity lost between daemon and queue.
3. All agents are busy or offline.

**Steps:**
```bash
# Check daemon status.
docker logs codero --tail 50

# Check Redis queue depth directly.
redis-cli -a "$REDIS_PASSWORD" ZCARD "owner/repo:queue:pending"

# Check if any lease exists.
redis-cli -a "$REDIS_PASSWORD" KEYS "owner/repo:lease:*"

# Restart daemon if needed.
docker restart codero
```

---

### Alert: `CoderoHighQueueWaitP95`

**Symptoms:** p95 queue wait > 30 minutes for a repo.

**Causes:**
1. Too many branches competing for too few agent slots.
2. Agent processing is slow (large PRs, rate-limited GitHub API).
3. Unfair scheduling — one repo starving others.

**Steps:**
```bash
# Check current queue depth across repos.
curl -s http://localhost:9210/metrics | grep codero_queue_depth

# Check prompy's wait histogram for the affected repo.
curl -s http://localhost:9210/metrics | grep "codero_queue_wait_seconds.*owner/repo"

# Check if a single branch has been waiting the longest.
redis-cli -a "$REDIS_PASSWORD" ZRANGEBYSCORE "owner/repo:queue:pending" -inf +inf WITHSCORES
```

**Mitigation:**
- Increase agent concurrency or add more agents.
- Verify hotfix branches get `WeightHotfix = 2.0` priority.
- Consider bumping `PROMPY_STALL_THRESHOLD_SECONDS` if long waits are expected.

---

### Alert: `CoderoMissingPrompyData`

**Symptoms:** `codero_last_success_timestamp_seconds` absent or stale > 5 minutes.

**Causes:**
1. prompy container has crashed or exited.
2. Redis is unreachable from the prompy container.
3. All data sources are failing simultaneously.

**Steps:**
```bash
# Check prompy container status.
docker ps -f name=prompy

# Check logs for errors.
docker logs prompy --tail 100

# Restart prompy.
docker restart prompy

# Confirm metrics endpoint responds after restart.
sleep 10 && curl -s http://localhost:9210/metrics | grep codero_last_success
```

If Redis is unreachable:
```bash
# From inside the prompy container.
docker exec prompy python3 -c "import redis; redis.Redis(host='redis', port=6379).ping(); print('ok')"
```

---

## Rollback

1. Stop and remove the prompy container:
   ```bash
   cd services/prompy
   docker compose down
   ```

2. No database or state changes are made by prompy. All state lives in Redis (owned by codero daemon) and Prometheus (time-series only).

3. Prometheus will stop receiving codero metrics; existing data remains available for the configured retention period.

4. To revert the code change:
   ```bash
   git revert <commit-hash>  # or git checkout main -- services/prompy
   git push origin main
   ```

5. Prometheus alert rules can be removed by deleting `services/prompy/alerts.yml` from the `rule_files` config and sending SIGHUP to Prometheus:
   ```bash
   kill -HUP $(pgrep prometheus)
   ```
