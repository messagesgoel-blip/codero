-- Rollback WIRE-001: remove repo, branch, worktree from agent_sessions.
-- SQLite does not support DROP COLUMN before 3.35.0; recreate the table
-- preserving all columns added through migration 000024.
CREATE TABLE agent_sessions_backup AS SELECT
    session_id, agent_id, mode, started_at, last_seen_at, ended_at, end_reason,
    tmux_session_name, heartbeat_secret, last_io_at,
    context_pressure, last_compact_at, compact_count, litellm_session_id,
    inferred_status, inferred_status_updated_at
FROM agent_sessions;
DROP TABLE agent_sessions;
ALTER TABLE agent_sessions_backup RENAME TO agent_sessions;
