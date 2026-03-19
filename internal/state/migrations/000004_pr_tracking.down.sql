-- Reverse migration: remove pr_number and owner_agent from branch_states.
-- SQLite has no DROP COLUMN before v3.35; use the table-rebuild idiom.
CREATE TABLE branch_states_v3 (
    id                      TEXT     NOT NULL PRIMARY KEY,
    repo                    TEXT     NOT NULL,
    branch                  TEXT     NOT NULL,
    head_hash               TEXT     NOT NULL DEFAULT '',
    state                   TEXT     NOT NULL,
    retry_count             INTEGER  NOT NULL DEFAULT 0,
    max_retries             INTEGER  NOT NULL DEFAULT 3,
    approved                INTEGER  NOT NULL DEFAULT 0,
    ci_green                INTEGER  NOT NULL DEFAULT 0,
    pending_events          INTEGER  NOT NULL DEFAULT 0,
    unresolved_threads      INTEGER  NOT NULL DEFAULT 0,
    owner_session_id        TEXT     NOT NULL DEFAULT '',
    owner_session_last_seen DATETIME,
    queue_priority          INTEGER  NOT NULL DEFAULT 0,
    submission_time         DATETIME,
    lease_id                TEXT,
    lease_expires_at        DATETIME,
    created_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (repo, branch)
);
INSERT INTO branch_states_v3
    SELECT id, repo, branch, head_hash, state, retry_count, max_retries,
           approved, ci_green, pending_events, unresolved_threads,
           owner_session_id, owner_session_last_seen, queue_priority,
           submission_time, lease_id, lease_expires_at, created_at, updated_at
    FROM branch_states;
DROP TABLE branch_states;
ALTER TABLE branch_states_v3 RENAME TO branch_states;
CREATE INDEX IF NOT EXISTS idx_branch_states_state    ON branch_states (state);
CREATE INDEX IF NOT EXISTS idx_branch_states_repo_branch ON branch_states (repo, branch);
