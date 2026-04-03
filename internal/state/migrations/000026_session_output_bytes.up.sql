-- Add output_bytes to agent_sessions for cross-family output tracking.
-- Updated on each heartbeat when the agent wrapper reports cumulative output.
ALTER TABLE agent_sessions ADD COLUMN output_bytes INTEGER NOT NULL DEFAULT 0;
