-- last_io_at tracks raw terminal I/O from the codero agent wrapper.
-- It is updated on every heartbeat where the child process produced output
-- in the last progressWindow (60 s). This is separate from last_progress_at,
-- which remains reserved for meaningful work milestones used by the 60-minute
-- compliance stuck-detection rule.
ALTER TABLE agent_sessions ADD COLUMN last_io_at DATETIME;

CREATE INDEX IF NOT EXISTS idx_agent_sessions_last_io_at
    ON agent_sessions (last_io_at);
