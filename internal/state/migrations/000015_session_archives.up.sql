-- Session archives: append-only table. One row per terminal session.
-- Written atomically by the daemon on session end.
-- Spec reference: Session Lifecycle v1 §4.1, §4.2, SL-1, SL-2, SL-3.

CREATE TABLE IF NOT EXISTS session_archives (
    archive_id       TEXT     NOT NULL PRIMARY KEY,
    session_id       TEXT     NOT NULL,
    agent_id         TEXT     NOT NULL,
    task_id          TEXT     NOT NULL DEFAULT '',
    repo             TEXT     NOT NULL DEFAULT '',
    branch           TEXT     NOT NULL DEFAULT '',
    result           TEXT     NOT NULL,
    started_at       TEXT     NOT NULL,
    ended_at         TEXT     NOT NULL,
    duration_seconds INTEGER  NOT NULL DEFAULT 0,
    commit_count     INTEGER  NOT NULL DEFAULT 0,
    merge_sha        TEXT     NOT NULL DEFAULT '',
    task_source      TEXT     NOT NULL DEFAULT '',
    archived_at      TEXT     NOT NULL DEFAULT (datetime('now')),

    CONSTRAINT unique_session_archive UNIQUE (session_id)
);

CREATE INDEX IF NOT EXISTS idx_session_archives_session_id
    ON session_archives (session_id);

CREATE INDEX IF NOT EXISTS idx_session_archives_agent_id
    ON session_archives (agent_id);

CREATE INDEX IF NOT EXISTS idx_session_archives_archived_at
    ON session_archives (archived_at);

CREATE INDEX IF NOT EXISTS idx_session_archives_result
    ON session_archives (result);
