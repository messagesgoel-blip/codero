-- EL-23: heartbeat_secret enforces launcher-only heartbeat semantics.
ALTER TABLE agent_sessions ADD COLUMN heartbeat_secret TEXT NOT NULL DEFAULT '';
