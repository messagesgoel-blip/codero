DROP INDEX IF EXISTS idx_agent_assignments_substatus;

DELETE FROM assignment_rule_checks
WHERE rule_id IN ('RULE-001', 'RULE-002');

DELETE FROM agent_rules
WHERE rule_id IN ('RULE-001', 'RULE-002');

PRAGMA foreign_keys=OFF;

CREATE TABLE agent_assignments_old (
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

INSERT INTO agent_assignments_old (
    assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
    started_at, ended_at, end_reason, superseded_by
)
SELECT
    assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
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

PRAGMA foreign_keys=ON;
