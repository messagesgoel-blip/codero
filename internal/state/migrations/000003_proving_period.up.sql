-- Sprint 6: proving period tracking for Phase 1F sign-off.
-- Tracks explicit events for metrics that cannot be derived from existing tables.

-- Pre-commit review attempts: one row per pre-commit review (LiteLLM or CodeRabbit).
-- This enables tracking "pre-commit reviews per project per week".
CREATE TABLE IF NOT EXISTS precommit_reviews (
    id          TEXT     NOT NULL PRIMARY KEY,  -- UUID
    repo        TEXT     NOT NULL,
    branch      TEXT     NOT NULL,
    provider    TEXT     NOT NULL,              -- "litellm" or "coderabbit"
    status      TEXT     NOT NULL,              -- "passed", "failed", "error"
    error       TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_precommit_reviews_repo_created
    ON precommit_reviews (repo, created_at);

-- Proving events: explicit operational events that need tracking.
-- event_type values: "queue_stall", "manual_db_repair", "unresolved_thread_failure",
--                    "missed_delivery", "lease_expiry_recovery"
CREATE TABLE IF NOT EXISTS proving_events (
    id          INTEGER  PRIMARY KEY AUTOINCREMENT,
    repo        TEXT     NOT NULL,
    event_type  TEXT     NOT NULL,
    details     TEXT     NOT NULL DEFAULT '',   -- JSON details
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    CHECK(trim(repo) <> '')
);

CREATE INDEX IF NOT EXISTS idx_proving_events_type_created
    ON proving_events (event_type, created_at);

CREATE INDEX IF NOT EXISTS idx_proving_events_repo_created
    ON proving_events (repo, created_at);

-- Proving snapshots: daily persisted scorecard for 30-day sign-off evidence.
-- One row per day, appended (never updated).
CREATE TABLE IF NOT EXISTS proving_snapshots (
    id          INTEGER  PRIMARY KEY AUTOINCREMENT,
    snapshot_date TEXT   NOT NULL UNIQUE,       -- YYYY-MM-DD
    scorecard_json TEXT  NOT NULL,              -- Full JSON scorecard
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_proving_snapshots_date
    ON proving_snapshots (snapshot_date);