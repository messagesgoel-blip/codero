-- 000024: inferred_status for structured hook/event-based agent status detection.
-- Values: 'unknown' (default), 'working', 'waiting_for_input', 'idle'.
-- See also: inferred_status_updated_at for stale detection and idle transition guards.
ALTER TABLE agent_sessions ADD COLUMN inferred_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE agent_sessions ADD COLUMN inferred_status_updated_at DATETIME;
