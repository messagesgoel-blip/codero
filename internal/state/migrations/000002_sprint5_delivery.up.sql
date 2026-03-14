-- Sprint 5: delivery events, webhook dedup, review runs, and findings.

-- Delivery events: append-only feedback stream with monotonic seq IDs.
-- seq is assigned from Redis INCR (coordination) and written durably here.
-- Pollers must tolerate gaps (crash between INCR and INSERT leaves a harmless seq gap).
CREATE TABLE IF NOT EXISTS delivery_events (
    seq        INTEGER  NOT NULL,
    repo       TEXT     NOT NULL,
    branch     TEXT     NOT NULL,
    head_hash  TEXT     NOT NULL DEFAULT '',
    event_type TEXT     NOT NULL,   -- "finding_bundle", "system", "state_transition"
    payload    TEXT     NOT NULL,   -- JSON
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (repo, branch, seq)
);

CREATE INDEX IF NOT EXISTS idx_delivery_events_repo_branch_seq
    ON delivery_events (repo, branch, seq);

-- Webhook dedup: durable secondary idempotency beyond the Redis NX hot path.
-- Loss of Redis dedup cannot cause durable corruption because this table exists.
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    delivery_id TEXT     NOT NULL PRIMARY KEY,  -- X-GitHub-Delivery header (UUID)
    event_type  TEXT     NOT NULL,              -- X-GitHub-Event header
    repo        TEXT     NOT NULL DEFAULT '',   -- target repo slug
    processed   INTEGER  NOT NULL DEFAULT 0,    -- 0=received, 1=processed
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Review runs: one row per execution attempt against a branch.
CREATE TABLE IF NOT EXISTS review_runs (
    id          TEXT     NOT NULL PRIMARY KEY,  -- UUID
    repo        TEXT     NOT NULL,
    branch      TEXT     NOT NULL,
    head_hash   TEXT     NOT NULL DEFAULT '',
    provider    TEXT     NOT NULL,              -- "stub", "coderabbit", "litellm"
    status      TEXT     NOT NULL,              -- "pending", "running", "completed", "failed"
    started_at  DATETIME,
    finished_at DATETIME,
    error       TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_review_runs_repo_branch
    ON review_runs (repo, branch);

-- Normalized findings from review runs (append-only, never deleted).
CREATE TABLE IF NOT EXISTS findings (
    id         TEXT     NOT NULL PRIMARY KEY,  -- UUID
    run_id     TEXT     NOT NULL REFERENCES review_runs(id),
    repo       TEXT     NOT NULL,
    branch     TEXT     NOT NULL,
    severity   TEXT     NOT NULL,              -- "error", "warning", "info"
    category   TEXT     NOT NULL DEFAULT '',
    file       TEXT     NOT NULL DEFAULT '',
    line       INTEGER  NOT NULL DEFAULT 0,
    message    TEXT     NOT NULL,
    source     TEXT     NOT NULL,              -- provider name
    rule_id    TEXT     NOT NULL DEFAULT '',
    ts         DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_findings_repo_branch
    ON findings (repo, branch);

CREATE INDEX IF NOT EXISTS idx_findings_run_id
    ON findings (run_id);
