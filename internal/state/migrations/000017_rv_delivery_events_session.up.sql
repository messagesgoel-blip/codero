-- Real-Time Views v1: add session/assignment context to delivery events
-- for SSE event schema compliance (§4.2).
ALTER TABLE delivery_events ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE delivery_events ADD COLUMN assignment_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_delivery_events_session
    ON delivery_events (session_id)
    WHERE session_id <> '';
