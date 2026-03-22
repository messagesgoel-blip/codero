PRAGMA foreign_keys=OFF;

DROP INDEX IF EXISTS idx_task_feedback_cache_task;
DROP INDEX IF EXISTS idx_task_feedback_cache_session;
DROP TABLE IF EXISTS task_feedback_cache;

DROP INDEX IF EXISTS idx_codero_github_links_branch;
DROP INDEX IF EXISTS idx_codero_github_links_repo_pr;
DROP TABLE IF EXISTS codero_github_links;

DROP INDEX IF EXISTS idx_agent_assignments_feedback_updated;
DROP INDEX IF EXISTS idx_agent_assignments_task_version;
DROP INDEX IF EXISTS idx_agent_assignments_successor_session;
DROP INDEX IF EXISTS idx_agent_assignments_parent_assignment;

CREATE TABLE agent_assignments_old (
    assignment_id        TEXT     NOT NULL PRIMARY KEY,
    session_id           TEXT     NOT NULL REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
    agent_id             TEXT     NOT NULL,
    repo                 TEXT     NOT NULL,
    branch               TEXT     NOT NULL,
    worktree             TEXT     NOT NULL DEFAULT '',
    task_id              TEXT     NOT NULL DEFAULT '',
    state                TEXT     NOT NULL DEFAULT 'active',
    blocked_reason       TEXT     NOT NULL DEFAULT '',
    assignment_substatus TEXT,
    started_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at             DATETIME,
    end_reason           TEXT     NOT NULL DEFAULT '',
    superseded_by        TEXT
);

INSERT INTO agent_assignments_old (
    assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
    state, blocked_reason, assignment_substatus,
    started_at, ended_at, end_reason, superseded_by
)
SELECT
    assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
    state, blocked_reason, assignment_substatus,
    started_at, ended_at, end_reason, superseded_by
FROM agent_assignments;

DROP TABLE agent_assignments;

ALTER TABLE agent_assignments_old RENAME TO agent_assignments;

CREATE INDEX IF NOT EXISTS idx_agent_assignments_session_active
    ON agent_assignments (session_id, ended_at);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_active_only
    ON agent_assignments (session_id)
    WHERE ended_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_agent_assignments_agent_id
    ON agent_assignments (agent_id);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_repo_branch
    ON agent_assignments (repo, branch);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_task_id
    ON agent_assignments (task_id);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_substatus
    ON agent_assignments (assignment_substatus);

PRAGMA foreign_keys=ON;
