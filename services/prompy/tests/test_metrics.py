"""
Unit tests for metrics.py — verify all required metric names are registered
and that label sets are correct.
"""

import unittest

from prometheus_client import REGISTRY

from prompy.metrics import (
    last_success_timestamp_seconds,
    merge_ready_lead_seconds,
    poll_failures_total,
    queue_depth,
    queue_stalled,
    queue_wait_seconds,
    registered_metric_names,
    review_cycle_seconds,
)


class TestMetricNames(unittest.TestCase):
    def test_registered_metric_names_complete(self):
        names = registered_metric_names()
        expected = {
            "codero_queue_depth",
            "codero_queue_stalled",
            "codero_queue_wait_seconds",
            "codero_review_cycle_seconds",
            "codero_merge_ready_lead_seconds",
            "codero_poll_failures_total",
            "codero_last_success_timestamp_seconds",
        }
        self.assertEqual(set(names), expected)

    def test_all_metrics_in_registry(self):
        collector_names = {m.name for m in REGISTRY.collect()}
        for name in registered_metric_names():
            # prometheus_client registers Counters by their base name (without
            # the _total suffix that appears in scraped output).
            base = name.removesuffix("_total")
            self.assertIn(
                base,
                collector_names,
                f"Metric '{name}' (base: '{base}') not found in Prometheus registry",
            )


class TestMetricLabels(unittest.TestCase):
    def test_queue_depth_accepts_repo_label(self):
        # Should not raise.
        queue_depth.labels(repo="owner/repo").set(5)

    def test_queue_stalled_accepts_repo_label(self):
        queue_stalled.labels(repo="owner/repo").set(0)

    def test_queue_wait_accepts_repo_label(self):
        queue_wait_seconds.labels(repo="owner/repo").observe(30.0)

    def test_review_cycle_accepts_repo_label(self):
        review_cycle_seconds.labels(repo="owner/repo").observe(120.0)

    def test_merge_ready_lead_accepts_repo_label(self):
        merge_ready_lead_seconds.labels(repo="owner/repo").observe(3600.0)

    def test_poll_failures_accepts_source_label(self):
        for source in ("redis", "http_queue", "http_health", "http_metrics"):
            poll_failures_total.labels(source=source).inc()

    def test_last_success_is_gauge(self):
        last_success_timestamp_seconds.set(1_700_000_000)


class TestMetricBuckets(unittest.TestCase):
    def _get_bucket_bounds(self, metric):
        """Return the upper bounds of histogram buckets."""
        for m in REGISTRY.collect():
            if m.name == metric:
                for sample in m.samples:
                    if sample.name.endswith("_bucket"):
                        yield sample.labels.get("le")
                return

    def test_queue_wait_has_reasonable_buckets(self):
        bounds = list(self._get_bucket_bounds("codero_queue_wait_seconds"))
        # Must include at least 5 buckets (excluding +Inf).
        # Exclude +Inf sentinel.
        finite_bounds = [b for b in bounds if b != "+Inf"]
        self.assertGreaterEqual(len(finite_bounds), 5)

    def test_review_cycle_has_reasonable_buckets(self):
        bounds = list(self._get_bucket_bounds("codero_review_cycle_seconds"))
        finite_bounds = [b for b in bounds if b != "+Inf"]
        self.assertGreaterEqual(len(finite_bounds), 5)


if __name__ == "__main__":
    unittest.main()
