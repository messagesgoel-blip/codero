"""
Prometheus metric definitions for the prompy observability sidecar.

All metrics are defined here as module-level singletons to ensure
idempotent registration (prometheus_client raises on duplicate registration).
"""

from prometheus_client import Counter, Gauge, Histogram, REGISTRY

# Bucket boundaries for timing histograms (in seconds).
# Covers: 1s, 5s, 15s, 30s, 1m, 5m, 10m, 30m, 1h, 4h, 24h
_TIMING_BUCKETS = (1, 5, 15, 30, 60, 300, 600, 1800, 3600, 14400, 86400)

# -- Queue state metrics -------------------------------------------------------

queue_depth = Gauge(
    "codero_queue_depth",
    "Number of branches waiting in the dispatch queue for a given repo.",
    labelnames=["repo"],
)

queue_stalled = Gauge(
    "codero_queue_stalled",
    "1 if the queue for a repo has been non-empty with no lease activity for "
    "longer than the stall threshold, 0 otherwise.",
    labelnames=["repo"],
)

# -- Timing histograms --------------------------------------------------------

queue_wait_seconds = Histogram(
    "codero_queue_wait_seconds",
    "Time a branch spent waiting in the queue before being leased (seconds).",
    labelnames=["repo"],
    buckets=_TIMING_BUCKETS,
)

review_cycle_seconds = Histogram(
    "codero_review_cycle_seconds",
    "Time from branch enqueue to review completion (seconds).",
    labelnames=["repo"],
    buckets=_TIMING_BUCKETS,
)

merge_ready_lead_seconds = Histogram(
    "codero_merge_ready_lead_seconds",
    "Cumulative lead time from first commit to merge-ready state (seconds).",
    labelnames=["repo"],
    buckets=_TIMING_BUCKETS,
)

# -- Sidecar self-health metrics -----------------------------------------------

poll_failures_total = Counter(
    "codero_poll_failures_total",
    "Total number of failed poll attempts against codero endpoints or Redis.",
    labelnames=["source"],  # "redis" | "http_queue" | "http_health" | "http_metrics"
)

last_success_timestamp_seconds = Gauge(
    "codero_last_success_timestamp_seconds",
    "Unix timestamp of the last successful poll cycle.",
)


def registered_metric_names() -> list[str]:
    """Return the full metric names registered in this module."""
    return [
        "codero_queue_depth",
        "codero_queue_stalled",
        "codero_queue_wait_seconds",
        "codero_review_cycle_seconds",
        "codero_merge_ready_lead_seconds",
        "codero_poll_failures_total",
        "codero_last_success_timestamp_seconds",
    ]
