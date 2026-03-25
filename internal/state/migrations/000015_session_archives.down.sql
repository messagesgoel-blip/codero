-- Rollback session_archives table.

DROP INDEX IF EXISTS idx_session_archives_result;
DROP INDEX IF EXISTS idx_session_archives_archived_at;
DROP INDEX IF EXISTS idx_session_archives_agent_id;
DROP INDEX IF EXISTS idx_session_archives_session_id;
DROP TABLE IF EXISTS session_archives;
