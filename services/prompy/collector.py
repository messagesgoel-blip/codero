"""
Collector: polls codero runtime signals and updates Prometheus metrics.

Data sources (in priority order):
1. Redis direct connection — always used for queue depth and stall detection.
2. Codero HTTP endpoints (/health, /queue, /metrics) — optional; used when
   CODERO_BASE_URL is configured and the daemon is reachable.

Resilience:
- Each source failure increments codero_poll_failures_total{source=...}.
- A partial failure (one source down) does not prevent other sources from running.
- Retries with exponential back-off on transient errors (up to MAX_RETRIES).
- Queue items tracked across cycles to compute wait-time observations.
"""

from __future__ import annotations

import logging
import os
import time
from collections import defaultdict
from dataclasses import dataclass, field
from typing import Optional

import redis as redis_lib
import requests

from . import metrics as m

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

STALL_THRESHOLD_SECONDS = int(os.environ.get("PROMPY_STALL_THRESHOLD_SECONDS", "600"))
POLL_INTERVAL_SECONDS = float(os.environ.get("PROMPY_POLL_INTERVAL_SECONDS", "15"))
HTTP_TIMEOUT_SECONDS = float(os.environ.get("PROMPY_HTTP_TIMEOUT_SECONDS", "5"))
MAX_RETRIES = int(os.environ.get("PROMPY_MAX_RETRIES", "3"))
REDIS_ADDR = os.environ.get("REDIS_ADDR", "redis:6379")
CODERO_BASE_URL = os.environ.get("CODERO_BASE_URL", "").rstrip("/")


# ---------------------------------------------------------------------------
# Internal state tracking
# ---------------------------------------------------------------------------

@dataclass
class QueueItemState:
    """Tracks when a branch was first seen in the queue."""
    first_seen: float = field(default_factory=time.time)
    last_seen: float = field(default_factory=time.time)


class Tracker:
    """
    Tracks queue-item first-seen timestamps across poll cycles.
    Used to derive wait-time observations when items leave the queue.
    """

    def __init__(self) -> None:
        # repo -> {branch: QueueItemState}
        self._items: dict[str, dict[str, QueueItemState]] = defaultdict(dict)
        # repo -> last time any lease was detected alive
        self._last_lease_seen: dict[str, float] = {}

    def update_queue(self, repo: str, branches: set[str]) -> list[float]:
        """
        Update tracked items for *repo* given the current set of *branches*
        in the queue.

        Returns wait durations (seconds) for items that have left the queue
        since the previous cycle (i.e., graduated items).
        """
        now = time.time()
        known = self._items[repo]
        graduated_wait_times: list[float] = []

        # Items that disappeared — they were dequeued (leased or removed).
        for branch, state in list(known.items()):
            if branch not in branches:
                wait = now - state.first_seen
                graduated_wait_times.append(wait)
                del known[branch]

        # Track new items.
        for branch in branches:
            if branch in known:
                known[branch].last_seen = now
            else:
                known[branch] = QueueItemState(first_seen=now, last_seen=now)

        return graduated_wait_times

    def record_lease_seen(self, repo: str) -> None:
        self._last_lease_seen[repo] = time.time()

    def clear_repo(self, repo: str) -> None:
        """Remove all tracking state for a repo that has disappeared from the queue."""
        self._items.pop(repo, None)
        self._last_lease_seen.pop(repo, None)

    def is_stalled(self, repo: str, queue_depth: int) -> bool:
        """
        A queue is stalled if depth > 0 and no lease was seen within
        STALL_THRESHOLD_SECONDS.
        """
        if queue_depth == 0:
            return False
        last = self._last_lease_seen.get(repo)
        if last is None:
            # Never seen a lease — stall if items have been sitting for threshold.
            items = self._items.get(repo, {})
            if not items:
                return False
            oldest_first_seen = min(s.first_seen for s in items.values())
            return (time.time() - oldest_first_seen) >= STALL_THRESHOLD_SECONDS
        return (time.time() - last) >= STALL_THRESHOLD_SECONDS


# ---------------------------------------------------------------------------
# Redis polling
# ---------------------------------------------------------------------------

def _redis_client() -> Optional[redis_lib.Redis]:
    """Create a Redis client from REDIS_ADDR. Returns None on failure."""
    try:
        host, _, port_str = REDIS_ADDR.rpartition(":")
        port = int(port_str) if port_str.isdigit() else 6379
        password = os.environ.get("REDIS_PASSWORD") or None
        client = redis_lib.Redis(
            host=host or "redis",
            port=port,
            password=password,
            socket_connect_timeout=3,
            socket_timeout=3,
            decode_responses=True,
        )
        client.ping()
        return client
    except Exception as exc:
        log.warning("redis: connection failed: %s", exc)
        return None


def poll_redis(
    rc: redis_lib.Redis,
    tracker: Tracker,
) -> dict[str, int]:
    """
    Scan Redis for all repos with active queue keys.
    Returns {repo: queue_depth}.
    Updates the tracker with current queue membership.

    Queue keys follow codero's pattern: `{repo}:queue:pending`
    Lease keys follow: `{repo}:lease:{branch}`
    """
    queue_depths: dict[str, int] = {}

    # Scan for queue keys. Pattern: *:queue:pending
    try:
        cursor = 0
        seen_repos: set[str] = set()
        while True:
            cursor, keys = rc.scan(cursor=cursor, match="*:queue:pending", count=200)
            for key in keys:
                # key format: owner/repo:queue:pending
                repo = key.rsplit(":queue:pending", 1)[0]
                if repo in seen_repos:
                    continue
                seen_repos.add(repo)

                try:
                    branches_raw = rc.zrange(key, 0, -1)
                    branches = set(branches_raw)
                    depth = len(branches)
                    queue_depths[repo] = depth

                    # Observe wait times for graduated items.
                    wait_times = tracker.update_queue(repo, branches)
                    for wt in wait_times:
                        m.queue_wait_seconds.labels(repo=repo).observe(wt)

                    # Check for alive leases.
                    for branch in branches:
                        lease_key = f"{repo}:lease:{branch}"
                        ttl = rc.ttl(lease_key)
                        if ttl > 0:
                            tracker.record_lease_seen(repo)
                            break

                except redis_lib.RedisError as exc:
                    log.warning("redis: error reading queue for %s: %s", repo, exc)
                    m.poll_failures_total.labels(source="redis").inc()

            if cursor == 0:
                break
    except redis_lib.RedisError as exc:
        log.error("redis: scan failed: %s", exc)
        m.poll_failures_total.labels(source="redis").inc()
        # Re-raise so the caller does not treat a total scan failure as a
        # successful cycle (which would suppress CoderoMissingPrompyData).
        raise

    return queue_depths


# ---------------------------------------------------------------------------
# HTTP polling
# ---------------------------------------------------------------------------

def _get_json(path: str, source_label: str) -> Optional[dict]:
    """GET {CODERO_BASE_URL}{path} and return parsed JSON, or None on error."""
    if not CODERO_BASE_URL:
        return None
    url = f"{CODERO_BASE_URL}{path}"
    for attempt in range(1, MAX_RETRIES + 1):
        try:
            resp = requests.get(url, timeout=HTTP_TIMEOUT_SECONDS)
            resp.raise_for_status()
            return resp.json()
        except requests.exceptions.RequestException as exc:
            if attempt == MAX_RETRIES:
                # Log only the path, never the full URL — it may contain credentials.
                log.warning(
                    "http: endpoint unreachable after %d attempts (path=%s): %s",
                    MAX_RETRIES,
                    path,
                    exc,
                )
                m.poll_failures_total.labels(source=source_label).inc()
            else:
                backoff = 0.5 * (2 ** (attempt - 1))
                time.sleep(backoff)
    return None


def poll_http_queue(tracker: Tracker) -> Optional[dict[str, int]]:
    """
    Poll codero /queue endpoint.

    Expected response shape:
    {
      "repos": {
        "owner/repo": {
          "depth": 3,
          "branches": ["feat/x", "feat/y", "feat/z"],
          "leased": true
        }
      }
    }

    Returns {repo: depth} or None if endpoint unavailable.
    """
    data = _get_json("/queue", "http_queue")
    if data is None:
        return None

    repos_data = data.get("repos", {})
    result: dict[str, int] = {}
    for repo, info in repos_data.items():
        depth = info.get("depth", 0)
        result[repo] = depth
        branches = set(info.get("branches", []))
        wait_times = tracker.update_queue(repo, branches)
        for wt in wait_times:
            m.queue_wait_seconds.labels(repo=repo).observe(wt)
        if info.get("leased"):
            tracker.record_lease_seen(repo)

    return result


def poll_http_metrics(repo: str) -> bool:
    """
    Poll codero /metrics endpoint for timing observations.

    Expected shape (for a single repo batch):
    {
      "review_cycle_seconds_observations": [120.5, 300.0],
      "merge_ready_lead_seconds_observations": [3600.0]
    }

    Returns True if the endpoint responded with data, False otherwise.
    """
    data = _get_json(f"/metrics?repo={repo}", "http_metrics")
    if data is None:
        return False
    for obs in data.get("review_cycle_seconds_observations", []):
        try:
            m.review_cycle_seconds.labels(repo=repo).observe(float(obs))
        except (TypeError, ValueError):
            pass
    for obs in data.get("merge_ready_lead_seconds_observations", []):
        try:
            m.merge_ready_lead_seconds.labels(repo=repo).observe(float(obs))
        except (TypeError, ValueError):
            pass
    return True


def poll_http_health() -> bool:
    """
    Poll codero /health endpoint.
    Returns True if codero daemon is healthy, False otherwise.
    """
    data = _get_json("/health", "http_health")
    if data is None:
        return False
    return data.get("status") == "ok"


# ---------------------------------------------------------------------------
# Main collection cycle
# ---------------------------------------------------------------------------

class Collector:
    """
    Orchestrates all data sources and updates Prometheus metrics every
    POLL_INTERVAL_SECONDS.
    """

    def __init__(self) -> None:
        self._tracker = Tracker()
        self._rc: Optional[redis_lib.Redis] = None
        # Track repos seen in the previous cycle to zero out stale gauge labels.
        self._known_repos: set[str] = set()

    def _ensure_redis(self) -> Optional[redis_lib.Redis]:
        """Lazy-connect/reconnect to Redis."""
        if self._rc is not None:
            try:
                self._rc.ping()
                return self._rc
            except redis_lib.RedisError:
                self._rc = None

        self._rc = _redis_client()
        if self._rc is None:
            m.poll_failures_total.labels(source="redis").inc()
        return self._rc

    def _run_cycle(self) -> None:
        """Execute one full collection cycle."""
        queue_depths: dict[str, int] = {}
        # Only mark the cycle as successful when at least one source returns data.
        cycle_succeeded = False

        # --- Redis source ---
        rc = self._ensure_redis()
        if rc is not None:
            try:
                redis_depths = poll_redis(rc, self._tracker)
                queue_depths.update(redis_depths)
                cycle_succeeded = True
            except Exception as exc:
                log.error("collector: redis poll error: %s", exc)
                m.poll_failures_total.labels(source="redis").inc()

        # --- HTTP /queue source (merges/overrides Redis data) ---
        http_depths = poll_http_queue(self._tracker)
        if http_depths is not None:
            queue_depths.update(http_depths)
            cycle_succeeded = True

        # --- Stale-label cleanup: zero out repos that disappeared this cycle ---
        current_repos = set(queue_depths.keys())
        for repo in self._known_repos - current_repos:
            m.queue_depth.labels(repo=repo).set(0)
            m.queue_stalled.labels(repo=repo).set(0)
            # Also clear tracker state so stale timestamps don't affect
            # stall detection or wait-time observations if the repo reappears.
            self._tracker.clear_repo(repo)

        # --- Update queue_depth and queue_stalled gauges ---
        for repo, depth in queue_depths.items():
            m.queue_depth.labels(repo=repo).set(depth)
            stalled = 1 if self._tracker.is_stalled(repo, depth) else 0
            m.queue_stalled.labels(repo=repo).set(stalled)
            if depth == 0:
                # Clear stall flag when queue drains.
                m.queue_stalled.labels(repo=repo).set(0)

        self._known_repos = current_repos

        # --- HTTP /metrics source for timing histograms ---
        for repo in queue_depths:
            try:
                if poll_http_metrics(repo):
                    cycle_succeeded = True
            except Exception as exc:
                log.warning("collector: http metrics poll error for %s: %s", repo, exc)

        # --- Record successful cycle only when at least one source provided data ---
        if cycle_succeeded:
            m.last_success_timestamp_seconds.set(time.time())

    def run_forever(self) -> None:
        """Block and run collection cycles indefinitely."""
        log.info(
            "collector: starting; interval=%ss stall_threshold=%ss redis=%s base_url=%s",
            POLL_INTERVAL_SECONDS,
            STALL_THRESHOLD_SECONDS,
            REDIS_ADDR,
            # Never log the raw CODERO_BASE_URL — it may contain embedded credentials.
            "configured" if CODERO_BASE_URL else "(none)",
        )
        while True:
            start = time.monotonic()
            try:
                self._run_cycle()
            except Exception as exc:
                log.error("collector: unhandled error in cycle: %s", exc, exc_info=True)
                # No single source is identifiable at this outer level; use "unknown".
                m.poll_failures_total.labels(source="unknown").inc()

            elapsed = time.monotonic() - start
            sleep_for = max(0.0, POLL_INTERVAL_SECONDS - elapsed)
            time.sleep(sleep_for)
