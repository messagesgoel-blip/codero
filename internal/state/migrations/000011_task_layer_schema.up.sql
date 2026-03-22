ALTER TABLE agent_assignments
    ADD COLUMN assignment_version INTEGER NOT NULL DEFAULT 1 CHECK (assignment_version >= 1);

ALTER TABLE agent_assignments
    ADD COLUMN parent_assignment_id TEXT REFERENCES agent_assignments(assignment_id) ON DELETE SET NULL;

ALTER TABLE agent_assignments
    ADD COLUMN successor_session_id TEXT REFERENCES agent_sessions(session_id) ON DELETE SET NULL;

ALTER TABLE agent_assignments
    ADD COLUMN description TEXT;

ALTER TABLE agent_assignments
    ADD COLUMN last_emit_at DATETIME;

ALTER TABLE agent_assignments
    ADD COLUMN blocked_since DATETIME;

ALTER TABLE agent_assignments
    ADD COLUMN first_feedback_at DATETIME;

ALTER TABLE agent_assignments
    ADD COLUMN last_feedback_at DATETIME;

ALTER TABLE agent_assignments
    ADD COLUMN feedback_poll_count INTEGER NOT NULL DEFAULT 0 CHECK (feedback_poll_count >= 0);

ALTER TABLE agent_assignments
    ADD COLUMN suggested_substatus_last TEXT;

ALTER TABLE agent_assignments
    ADD COLUMN actual_substatus_last TEXT;

ALTER TABLE agent_assignments
    ADD COLUMN substatus_deviation_count INTEGER NOT NULL DEFAULT 0 CHECK (substatus_deviation_count >= 0);

UPDATE agent_assignments
SET
    assignment_version = CASE
        WHEN assignment_version < 1 THEN 1
        ELSE assignment_version
    END,
    last_emit_at = COALESCE(last_emit_at, started_at),
    blocked_since = CASE
        WHEN state = 'blocked' AND ended_at IS NULL THEN COALESCE(blocked_since, started_at)
        ELSE blocked_since
    END,
    actual_substatus_last = COALESCE(actual_substatus_last, assignment_substatus);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_parent_assignment
    ON agent_assignments (parent_assignment_id);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_successor_session
    ON agent_assignments (successor_session_id);

CREATE INDEX IF NOT EXISTS idx_agent_assignments_task_version
    ON agent_assignments (task_id, assignment_version DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_assignments_live_task_id
    ON agent_assignments (task_id)
    WHERE ended_at IS NULL AND task_id <> '';

CREATE INDEX IF NOT EXISTS idx_agent_assignments_feedback_updated
    ON agent_assignments (last_feedback_at DESC);

CREATE TABLE IF NOT EXISTS codero_github_links (
    link_id         TEXT    NOT NULL PRIMARY KEY,
    task_id         TEXT    NOT NULL UNIQUE,
    repo_full_name  TEXT    NOT NULL,
    pr_number       INTEGER,
    issue_number    INTEGER,
    branch_name     TEXT,
    head_sha        TEXT,
    pr_state        TEXT,
    last_ci_run_id  TEXT,
    CHECK (pr_state IS NULL OR pr_state IN ('open', 'closed', 'merged'))
);

CREATE INDEX IF NOT EXISTS idx_codero_github_links_repo_pr
    ON codero_github_links (repo_full_name, pr_number);

CREATE INDEX IF NOT EXISTS idx_codero_github_links_branch
    ON codero_github_links (repo_full_name, branch_name);

CREATE TABLE IF NOT EXISTS task_feedback_cache (
    cache_id                TEXT     NOT NULL PRIMARY KEY,
    assignment_id           TEXT     NOT NULL UNIQUE REFERENCES agent_assignments(assignment_id) ON DELETE CASCADE,
    session_id              TEXT     NOT NULL REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
    task_id                 TEXT     NOT NULL,
    ci_snapshot             TEXT,
    coderabbit_snapshot     TEXT,
    human_review_snapshot   TEXT,
    compliance_snapshot     TEXT,
    context_block           TEXT,
    snapshot_at             DATETIME NOT NULL DEFAULT (datetime('now')),
    cache_hash              TEXT     NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_task_feedback_cache_session
    ON task_feedback_cache (session_id, snapshot_at DESC);

CREATE INDEX IF NOT EXISTS idx_task_feedback_cache_task
    ON task_feedback_cache (task_id, snapshot_at DESC);
