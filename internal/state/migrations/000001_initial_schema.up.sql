-- Branch state records: one row per tracked branch.
-- Covers all fields required by the canonical state machine
-- (all 11 states, all 20 transitions from codero-roadmap-v4.md).
CREATE TABLE IF NOT EXISTS branch_states (
    id                      TEXT    NOT NULL PRIMARY KEY,  -- opaque branch identifier (UUID)
    repo                    TEXT    NOT NULL,               -- "owner/name" e.g. "acme/api"
    branch                  TEXT    NOT NULL,               -- branch name
    head_hash               TEXT    NOT NULL DEFAULT '',    -- current HEAD SHA; stale detection (T12)
    state                   TEXT    NOT NULL,               -- canonical state machine state
    retry_count             INTEGER NOT NULL DEFAULT 0,     -- incremented on lease expiry (T07) or re-submit (T13)
    max_retries             INTEGER NOT NULL DEFAULT 3,     -- threshold for blocked state (T16)
    approved                INTEGER NOT NULL DEFAULT 0,     -- GitHub approval flag; bool (T10)
    ci_green                INTEGER NOT NULL DEFAULT 0,     -- CI pass flag; bool (T10)
    pending_events          INTEGER NOT NULL DEFAULT 0,     -- unprocessed GitHub events (T10)
    unresolved_threads      INTEGER NOT NULL DEFAULT 0,     -- open review threads (T10)
    owner_session_id        TEXT    NOT NULL DEFAULT '',    -- session that owns this branch
    owner_session_last_seen DATETIME,                       -- last heartbeat; NULL until first heartbeat (T14)
    queue_priority          INTEGER NOT NULL DEFAULT 0,     -- WFQ priority [0,20]; validated at submit
    submission_time         DATETIME,                       -- when branch entered queue; used for WFQ wait score
    lease_id                TEXT,                           -- active lease identifier; NULL when not in cli_reviewing
    lease_expires_at        DATETIME,                       -- lease expiry; NULL when no active lease (T07)
    created_at              DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at              DATETIME NOT NULL DEFAULT (datetime('now')),

    UNIQUE (repo, branch)
);

CREATE INDEX IF NOT EXISTS idx_branch_states_state
    ON branch_states (state);

CREATE INDEX IF NOT EXISTS idx_branch_states_repo_branch
    ON branch_states (repo, branch);

-- State transition audit log: every valid transition is appended here.
-- Used by P1-S1-07 crash recovery and the structured internal log (P1-S1-08).
CREATE TABLE IF NOT EXISTS state_transitions (
    id              INTEGER  PRIMARY KEY AUTOINCREMENT,
    branch_state_id TEXT     NOT NULL REFERENCES branch_states (id),
    from_state      TEXT     NOT NULL,
    to_state        TEXT     NOT NULL,
    trigger         TEXT     NOT NULL,  -- e.g. "lease_expired", "codero-cli submit", "heartbeat_timeout"
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_state_transitions_branch
    ON state_transitions (branch_state_id);
