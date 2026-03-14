"""
Unit tests for collector.py — parsing, state tracking, and metric mapping.
Tests run without a live Redis or codero daemon.
"""

import time
import unittest
from unittest.mock import MagicMock, patch

from prompy.collector import Tracker, poll_redis, poll_http_queue


class TestTracker(unittest.TestCase):
    def test_new_items_are_tracked(self):
        tracker = Tracker()
        graduated = tracker.update_queue("owner/repo", {"feat/a", "feat/b"})
        self.assertEqual(graduated, [])
        self.assertIn("feat/a", tracker._items["owner/repo"])
        self.assertIn("feat/b", tracker._items["owner/repo"])

    def test_departed_items_yield_wait_time(self):
        tracker = Tracker()
        tracker.update_queue("owner/repo", {"feat/a"})
        # Simulate elapsed time by manually backdating first_seen.
        tracker._items["owner/repo"]["feat/a"].first_seen = time.time() - 100
        graduated = tracker.update_queue("owner/repo", set())  # feat/a left
        self.assertEqual(len(graduated), 1)
        self.assertGreaterEqual(graduated[0], 100)

    def test_stalled_when_depth_positive_and_no_lease_seen(self):
        tracker = Tracker()
        tracker.update_queue("owner/repo", {"feat/a"})
        # Backdate first_seen beyond threshold.
        tracker._items["owner/repo"]["feat/a"].first_seen = time.time() - 700
        self.assertTrue(tracker.is_stalled("owner/repo", 1))

    def test_not_stalled_when_queue_empty(self):
        tracker = Tracker()
        self.assertFalse(tracker.is_stalled("owner/repo", 0))

    def test_not_stalled_when_lease_seen_recently(self):
        tracker = Tracker()
        tracker.update_queue("owner/repo", {"feat/a"})
        tracker._items["owner/repo"]["feat/a"].first_seen = time.time() - 700
        tracker.record_lease_seen("owner/repo")
        self.assertFalse(tracker.is_stalled("owner/repo", 1))

    def test_stall_clears_when_lease_seen_after_long_wait(self):
        tracker = Tracker()
        tracker.update_queue("owner/repo", {"feat/a"})
        tracker._items["owner/repo"]["feat/a"].first_seen = time.time() - 700
        # Initially stalled.
        self.assertTrue(tracker.is_stalled("owner/repo", 1))
        # Lease seen.
        tracker.record_lease_seen("owner/repo")
        self.assertFalse(tracker.is_stalled("owner/repo", 1))

    def test_multiple_repos_independent(self):
        tracker = Tracker()
        tracker.update_queue("repo/a", {"feat/x"})
        tracker.update_queue("repo/b", {"feat/y"})
        self.assertIn("feat/x", tracker._items["repo/a"])
        self.assertIn("feat/y", tracker._items["repo/b"])
        self.assertNotIn("feat/x", tracker._items.get("repo/b", {}))

    def test_repeated_update_does_not_duplicate(self):
        tracker = Tracker()
        tracker.update_queue("owner/repo", {"feat/a"})
        first_seen = tracker._items["owner/repo"]["feat/a"].first_seen
        time.sleep(0.01)
        tracker.update_queue("owner/repo", {"feat/a"})
        self.assertAlmostEqual(
            tracker._items["owner/repo"]["feat/a"].first_seen, first_seen, places=2
        )


class TestPollRedis(unittest.TestCase):
    def _make_redis_mock(self, queue_keys, queue_members, lease_ttls=None):
        """
        Helper to build a mock Redis client.

        queue_keys: list of keys returned by scan
        queue_members: dict[key -> list of branch names]
        lease_ttls: dict[lease_key -> ttl_value]
        """
        lease_ttls = lease_ttls or {}
        rc = MagicMock()

        # scan returns (cursor=0, keys) on first call, terminates.
        rc.scan.return_value = (0, queue_keys)
        rc.zrange.side_effect = lambda key, *_: queue_members.get(key, [])
        rc.ttl.side_effect = lambda key: lease_ttls.get(key, -2)
        return rc

    def test_queue_depth_populated(self):
        tracker = Tracker()
        rc = self._make_redis_mock(
            queue_keys=["owner/repo:queue:pending"],
            queue_members={"owner/repo:queue:pending": ["feat/a", "feat/b"]},
        )
        depths, complete = poll_redis(rc, tracker)
        self.assertEqual(depths["owner/repo"], 2)
        self.assertTrue(complete)

    def test_empty_queue_returns_zero_depth(self):
        tracker = Tracker()
        rc = self._make_redis_mock(
            queue_keys=["owner/repo:queue:pending"],
            queue_members={"owner/repo:queue:pending": []},
        )
        depths, complete = poll_redis(rc, tracker)
        # Contract: a visible (scanned) queue key with no members must appear
        # in the result dict with depth 0, not be absent entirely.
        self.assertIn("owner/repo", depths)
        self.assertEqual(depths["owner/repo"], 0)
        self.assertTrue(complete)

    def test_lease_seen_recorded_when_ttl_positive(self):
        tracker = Tracker()
        rc = self._make_redis_mock(
            queue_keys=["owner/repo:queue:pending"],
            queue_members={"owner/repo:queue:pending": ["feat/a"]},
            lease_ttls={"owner/repo:lease:feat/a": 20},
        )
        poll_redis(rc, tracker)
        self.assertIn("owner/repo", tracker._last_lease_seen)

    def test_lease_not_recorded_when_no_lease(self):
        tracker = Tracker()
        rc = self._make_redis_mock(
            queue_keys=["owner/repo:queue:pending"],
            queue_members={"owner/repo:queue:pending": ["feat/a"]},
            lease_ttls={},
        )
        poll_redis(rc, tracker)
        self.assertNotIn("owner/repo", tracker._last_lease_seen)

    def test_multiple_repos_handled(self):
        tracker = Tracker()
        rc = MagicMock()
        rc.scan.return_value = (
            0,
            ["repo/a:queue:pending", "repo/b:queue:pending"],
        )
        rc.zrange.side_effect = lambda key, *_: {
            "repo/a:queue:pending": ["feat/x"],
            "repo/b:queue:pending": ["feat/y", "feat/z"],
        }.get(key, [])
        rc.ttl.return_value = -2
        depths, complete = poll_redis(rc, tracker)
        self.assertEqual(depths["repo/a"], 1)
        self.assertEqual(depths["repo/b"], 2)

    def test_redis_error_on_zrange_increments_failure(self):
        import redis as redis_lib
        from prompy import metrics as m

        tracker = Tracker()
        rc = MagicMock()
        rc.scan.return_value = (0, ["owner/repo:queue:pending"])
        rc.zrange.side_effect = redis_lib.RedisError("connection lost")

        before = m.poll_failures_total.labels(source="redis")._value.get()
        depths, complete = poll_redis(rc, tracker)
        after = m.poll_failures_total.labels(source="redis")._value.get()
        self.assertGreater(after, before)
        self.assertFalse(complete)  # per-repo error → partial snapshot


class TestPollHttpQueue(unittest.TestCase):
    def test_returns_none_when_no_base_url(self):
        with patch("prompy.collector.CODERO_BASE_URL", ""):
            tracker = Tracker()
            result = poll_http_queue(tracker)
            self.assertIsNone(result)

    @patch("prompy.collector.requests.get")
    def test_parses_depth_from_response(self, mock_get):
        mock_resp = MagicMock()
        mock_resp.raise_for_status.return_value = None
        mock_resp.json.return_value = {
            "repos": {
                "owner/repo": {
                    "depth": 3,
                    "branches": ["feat/a", "feat/b", "feat/c"],
                    "leased": False,
                }
            }
        }
        mock_get.return_value = mock_resp

        with patch("prompy.collector.CODERO_BASE_URL", "http://codero:8080"):
            tracker = Tracker()
            result = poll_http_queue(tracker)

        self.assertIsNotNone(result)
        self.assertEqual(result["owner/repo"], 3)

    @patch("prompy.collector.requests.get")
    def test_records_lease_seen_when_leased_true(self, mock_get):
        mock_resp = MagicMock()
        mock_resp.raise_for_status.return_value = None
        mock_resp.json.return_value = {
            "repos": {
                "owner/repo": {"depth": 1, "branches": ["feat/a"], "leased": True}
            }
        }
        mock_get.return_value = mock_resp

        with patch("prompy.collector.CODERO_BASE_URL", "http://codero:8080"):
            tracker = Tracker()
            poll_http_queue(tracker)

        self.assertIn("owner/repo", tracker._last_lease_seen)

    @patch("prompy.collector.requests.get")
    def test_returns_none_on_connection_error(self, mock_get):
        import requests as req_lib

        mock_get.side_effect = req_lib.exceptions.ConnectionError("refused")
        with patch("prompy.collector.CODERO_BASE_URL", "http://codero:8080"):
            with patch("prompy.collector.MAX_RETRIES", 1):
                tracker = Tracker()
                result = poll_http_queue(tracker)
        self.assertIsNone(result)


if __name__ == "__main__":
    unittest.main()
