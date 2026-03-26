-- Roll back append-only session archives to unique-per-session schema.

CREATE TABLE IF NOT EXISTS session_archives_old (
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

INSERT OR IGNORE INTO session_archives_old
    (archive_id, session_id, agent_id, task_id, repo, branch, result,
     started_at, ended_at, duration_seconds, commit_count, merge_sha,
     task_source, archived_at)
SELECT archive_id,
       session_id,
       agent_id,
       COALESCE(task_id, ''),
       COALESCE(repo, ''),
       COALESCE(branch, ''),
       result,
       started_at,
       ended_at,
       duration_seconds,
       commit_count,
       COALESCE(merge_sha, ''),
       COALESCE(task_source, ''),
       archived_at
FROM session_archives
ORDER BY archived_at DESC;

DROP TABLE session_archives;
ALTER TABLE session_archives_old RENAME TO session_archives;

CREATE INDEX IF NOT EXISTS idx_session_archives_session_id
    ON session_archives (session_id);
CREATE INDEX IF NOT EXISTS idx_session_archives_agent_id
    ON session_archives (agent_id);
CREATE INDEX IF NOT EXISTS idx_session_archives_archived_at
    ON session_archives (archived_at);
CREATE INDEX IF NOT EXISTS idx_session_archives_result
    ON session_archives (result);
