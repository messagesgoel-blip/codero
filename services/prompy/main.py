"""
prompy — codero observability sidecar.

Starts two threads:
  1. Metrics HTTP server on :9210/metrics
  2. Collector loop (polls Redis + codero HTTP endpoints every 15s)

Exit on SIGTERM/SIGINT.
"""

import logging
import signal
import sys
import threading

from .collector import Collector
from .server import make_metrics_server, serve_forever

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    datefmt="%Y-%m-%dT%H:%M:%S",
    stream=sys.stdout,
)

log = logging.getLogger(__name__)

_shutdown = threading.Event()


def _handle_signal(signum: int, frame: object) -> None:
    log.info("prompy: received signal %d, shutting down", signum)
    _shutdown.set()


def main() -> None:
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    server = make_metrics_server()
    collector = Collector()

    server_thread = threading.Thread(target=serve_forever, args=(server,), daemon=True, name="metrics-server")
    collector_thread = threading.Thread(target=collector.run_forever, daemon=True, name="collector")

    server_thread.start()
    collector_thread.start()

    log.info("prompy: running (metrics server + collector)")

    _shutdown.wait()

    log.info("prompy: shutting down metrics server")
    server.shutdown()
    log.info("prompy: bye")


if __name__ == "__main__":
    main()
