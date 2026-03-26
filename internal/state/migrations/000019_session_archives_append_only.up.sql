-- Session archives: append-only (allow multiple rows per session) and nullable task fields.
-- Converts empty strings to NULL for task/branch metadata.

CREATE TABLE IF NOT EXISTS session_archives_new (
    archive_id       TEXT     NOT NULL PRIMARY KEY,
    session_id       TEXT     NOT NULL,
    agent_id         TEXT     NOT NULL,
    task_id          TEXT     NULL,
    repo             TEXT     NULL,
    branch           TEXT     NULL,
    result           TEXT     NOT NULL,
    started_at       TEXT     NOT NULL,
    ended_at         TEXT     NOT NULL,
    duration_seconds INTEGER  NOT NULL DEFAULT 0,
    commit_count     INTEGER  NOT NULL DEFAULT 0,
    merge_sha        TEXT     NULL,
    task_source      TEXT     NULL,
    archived_at      TEXT     NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO session_archives_new
    (archive_id, session_id, agent_id, task_id, repo, branch, result,
     started_at, ended_at, duration_seconds, commit_count, merge_sha,
     task_source, archived_at)
SELECT archive_id,
       session_id,
       agent_id,
       NULLIF(task_id, ''),
       NULLIF(repo, ''),
       NULLIF(branch, ''),
       result,
       started_at,
       ended_at,
       duration_seconds,
       commit_count,
       NULLIF(merge_sha, ''),
       NULLIF(task_source, ''),
       archived_at
FROM session_archives;

DROP TABLE session_archives;
ALTER TABLE session_archives_new RENAME TO session_archives;

CREATE INDEX IF NOT EXISTS idx_session_archives_session_id
    ON session_archives (session_id);
CREATE INDEX IF NOT EXISTS idx_session_archives_agent_id
    ON session_archives (agent_id);
CREATE INDEX IF NOT EXISTS idx_session_archives_archived_at
    ON session_archives (archived_at);
CREATE INDEX IF NOT EXISTS idx_session_archives_result
    ON session_archives (result);
