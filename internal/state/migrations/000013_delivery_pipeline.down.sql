-- Roll back delivery pipeline columns from agent_assignments.

CREATE TABLE IF NOT EXISTS agent_assignments_old (
    assignment_id          TEXT     NOT NULL PRIMARY KEY,
    session_id             TEXT     NOT NULL REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
    agent_id               TEXT     NOT NULL,
    repo                   TEXT     NOT NULL,
    branch                 TEXT     NOT NULL,
    worktree               TEXT     NOT NULL DEFAULT '',
    task_id                TEXT     NOT NULL DEFAULT '',
    started_at             DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at               DATETIME,
    end_reason             TEXT     NOT NULL DEFAULT '',
    superseded_by          TEXT,
    assignment_substatus   TEXT,
    state                  TEXT     NOT NULL DEFAULT 'active',
    blocked_reason         TEXT     NOT NULL DEFAULT '',
    assignment_version     INTEGER  NOT NULL DEFAULT 1,
    parent_assignment_id   TEXT,
    successor_session_id   TEXT,
    description            TEXT,
    last_emit_at           DATETIME,
    blocked_since          DATETIME,
    first_feedback_at      DATETIME,
    last_feedback_at       DATETIME,
    feedback_poll_count    INTEGER  NOT NULL DEFAULT 0,
    suggested_substatus_last    TEXT,
    actual_substatus_last       TEXT,
    substatus_deviation_count   INTEGER NOT NULL DEFAULT 0
);

INSERT INTO agent_assignments_old
    (assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
     started_at, ended_at, end_reason, superseded_by,
     assignment_substatus, state, blocked_reason,
     assignment_version, parent_assignment_id, successor_session_id,
     description, last_emit_at, blocked_since,
     first_feedback_at, last_feedback_at, feedback_poll_count,
     suggested_substatus_last, actual_substatus_last, substatus_deviation_count)
SELECT
     assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
     started_at, ended_at, end_reason, superseded_by,
     assignment_substatus, state, blocked_reason,
     assignment_version, parent_assignment_id, successor_session_id,
     description, last_emit_at, blocked_since,
     first_feedback_at, last_feedback_at, feedback_poll_count,
     suggested_substatus_last, actual_substatus_last, substatus_deviation_count
FROM agent_assignments;

DROP TABLE agent_assignments;
ALTER TABLE agent_assignments_old RENAME TO agent_assignments;

-- Recreate indexes from 000005.
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

-- Recreate indexes from 000008.
CREATE INDEX IF NOT EXISTS idx_agent_assignments_substatus
    ON agent_assignments (assignment_substatus);

-- Recreate indexes from 000011.
CREATE INDEX IF NOT EXISTS idx_agent_assignments_parent_assignment
    ON agent_assignments (parent_assignment_id);
CREATE INDEX IF NOT EXISTS idx_agent_assignments_successor_session
    ON agent_assignments (successor_session_id);
CREATE INDEX IF NOT EXISTS idx_agent_assignments_task_version
    ON agent_assignments (task_id, assignment_version DESC);
CREATE INDEX IF NOT EXISTS idx_agent_assignments_live_task_id
    ON agent_assignments (task_id)
    WHERE ended_at IS NULL AND task_id <> '';
CREATE INDEX IF NOT EXISTS idx_agent_assignments_feedback_updated
    ON agent_assignments (last_feedback_at DESC);
