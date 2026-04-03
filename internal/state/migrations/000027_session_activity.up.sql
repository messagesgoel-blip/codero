-- Session activity samples for output sparkline.
-- One row per minute per session, recording cumulative output_bytes at that time.
-- The sparkline is derived from deltas between consecutive samples.
CREATE TABLE session_activity (
    session_id TEXT    NOT NULL,
    bucket     TEXT    NOT NULL, -- minute bucket: "2026-04-03T10:05"
    output_bytes INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, bucket)
);
