ALTER TABLE agent_sessions
    ADD COLUMN last_progress_at DATETIME;

CREATE INDEX IF NOT EXISTS idx_agent_sessions_last_progress
    ON agent_sessions (last_progress_at);
