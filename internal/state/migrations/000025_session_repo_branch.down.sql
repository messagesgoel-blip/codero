-- Rollback WIRE-001: remove repo, branch from agent_sessions.
-- SQLite does not support DROP COLUMN before 3.35.0; recreate the table
-- preserving all columns added through migration 000024 with constraints.
CREATE TABLE agent_sessions_backup (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    last_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
    last_progress_at DATETIME,
    last_io_at DATETIME,
    context_pressure TEXT NOT NULL DEFAULT 'normal',
    last_compact_at DATETIME,
    compact_count INTEGER NOT NULL DEFAULT 0,
    litellm_session_id TEXT NOT NULL DEFAULT '',
    inferred_status TEXT NOT NULL DEFAULT '',
    inferred_status_updated_at DATETIME,
    tmux_session_name TEXT NOT NULL DEFAULT '',
    heartbeat_secret TEXT NOT NULL DEFAULT '',
    ended_at DATETIME,
    end_reason TEXT NOT NULL DEFAULT ''
);
INSERT INTO agent_sessions_backup
    SELECT session_id, agent_id, mode, started_at, last_seen_at, last_progress_at, last_io_at,
           context_pressure, last_compact_at, compact_count, litellm_session_id,
           inferred_status, inferred_status_updated_at, tmux_session_name,
           heartbeat_secret, ended_at, end_reason
    FROM agent_sessions;
DROP TABLE agent_sessions;
ALTER TABLE agent_sessions_backup RENAME TO agent_sessions;
