"""
HTTP server that exposes Prometheus metrics on :9210/metrics.
Uses prometheus_client's built-in WSGI app; no additional framework required.
"""

import logging
import os
from wsgiref.simple_server import WSGIRequestHandler, make_server

from prometheus_client import make_wsgi_app

log = logging.getLogger(__name__)

METRICS_PORT = int(os.environ.get("PROMPY_PORT", "9210"))
METRICS_ADDR = os.environ.get("PROMPY_ADDR", "0.0.0.0")


class _SilentHandler(WSGIRequestHandler):
    """Suppress per-request log lines from wsgiref."""

    def log_message(self, fmt: str, *args: object) -> None:  # type: ignore[override]
        pass


def make_metrics_server():
    """Create and return the WSGI server (does not start it)."""
    app = make_wsgi_app()
    server = make_server(METRICS_ADDR, METRICS_PORT, app, handler_class=_SilentHandler)
    return server


def serve_forever(server) -> None:
    """Block serving metrics requests."""
    log.info("metrics: listening on %s:%d/metrics", METRICS_ADDR, METRICS_PORT)
    server.serve_forever()
