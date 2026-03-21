-- Agent sessions, assignments, and event log.

CREATE TABLE IF NOT EXISTS agent_sessions (
    session_id  TEXT     NOT NULL PRIMARY KEY,
    agent_id    TEXT     NOT NULL,
    mode        TEXT     NOT NULL DEFAULT '',
    started_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    last_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at    DATETIME,
    end_reason  TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_agent_id
    ON agent_sessions (agent_id);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_last_seen
    ON agent_sessions (last_seen_at);

CREATE TABLE IF NOT EXISTS agent_assignments (
    assignment_id TEXT     NOT NULL PRIMARY KEY,
    session_id    TEXT     NOT NULL REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
    agent_id      TEXT     NOT NULL,
    repo          TEXT     NOT NULL,
    branch        TEXT     NOT NULL,
    worktree      TEXT     NOT NULL DEFAULT '',
    task_id       TEXT     NOT NULL DEFAULT '',
    started_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at      DATETIME,
    end_reason    TEXT     NOT NULL DEFAULT '',
    superseded_by TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_session_active
    ON agent_assignments (session_id, ended_at);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_agent_id
    ON agent_assignments (agent_id);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_repo_branch
    ON agent_assignments (repo, branch);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_task_id
    ON agent_assignments (task_id);

CREATE TABLE IF NOT EXISTS agent_events (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    session_id TEXT     NOT NULL REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
    agent_id   TEXT     NOT NULL,
    event_type TEXT     NOT NULL,
    payload    TEXT     NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_agent_events_session_created
    ON agent_events (session_id, created_at);
