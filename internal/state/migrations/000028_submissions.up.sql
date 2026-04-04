CREATE TABLE IF NOT EXISTS submissions (
    submission_id   TEXT PRIMARY KEY,
    assignment_id   TEXT NOT NULL DEFAULT '',
    session_id      TEXT NOT NULL DEFAULT '',
    repo            TEXT NOT NULL,
    branch          TEXT NOT NULL,
    head_sha        TEXT NOT NULL DEFAULT '',
    diff_hash       TEXT NOT NULL DEFAULT '',
    attempt_local   INTEGER NOT NULL DEFAULT 0,
    attempt_remote  INTEGER NOT NULL DEFAULT 0,
    state           TEXT NOT NULL DEFAULT 'submitted',
    result          TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_submissions_repo_branch ON submissions(repo, branch);
CREATE UNIQUE INDEX IF NOT EXISTS idx_submissions_dedup ON submissions(assignment_id, diff_hash, head_sha)
    WHERE assignment_id != '';
