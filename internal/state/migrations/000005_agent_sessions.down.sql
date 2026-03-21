-- Rollback agent session tables.

DROP INDEX IF EXISTS idx_agent_events_session_created;
DROP TABLE IF EXISTS agent_events;

DROP INDEX IF EXISTS idx_agent_assignments_task_id;
DROP INDEX IF EXISTS idx_agent_assignments_repo_branch;
DROP INDEX IF EXISTS idx_agent_assignments_agent_id;
DROP INDEX IF EXISTS idx_agent_assignments_session_active;
DROP TABLE IF EXISTS agent_assignments;

DROP INDEX IF EXISTS idx_agent_sessions_last_seen;
DROP INDEX IF EXISTS idx_agent_sessions_agent_id;
DROP TABLE IF EXISTS agent_sessions;
