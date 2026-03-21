DROP INDEX IF EXISTS idx_agent_sessions_last_progress;
ALTER TABLE agent_sessions
    DROP COLUMN last_progress_at;
