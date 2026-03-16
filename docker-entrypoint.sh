#!/bin/sh
# Remove stale PID file left by a previous container run.
# In Docker every container starts as PID 1; the kernel reuses the PID,
# so ProcessRunning(1) always returns true in the new container, making the
# daemon's stale-PID check incorrectly block startup.
PID_FILE="${CODERO_PID_FILE:-/data/pids/codero.pid}"
if [ -f "$PID_FILE" ]; then
    echo "codero-entrypoint: removing stale PID file $PID_FILE"
    rm -f "$PID_FILE"
fi
exec "$@"
