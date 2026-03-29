DROP INDEX IF EXISTS idx_agent_sessions_last_io_at;
-- SQLite does not support DROP COLUMN; column is left in place on rollback.
