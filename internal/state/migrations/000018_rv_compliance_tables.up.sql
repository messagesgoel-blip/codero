-- §2.9 Compliance view backing tables.
CREATE TABLE IF NOT EXISTS compliance_rules (
    rule_id      TEXT PRIMARY KEY,
    rule_version INTEGER NOT NULL DEFAULT 1,
    description  TEXT    NOT NULL DEFAULT '',
    enforcement  TEXT    NOT NULL DEFAULT 'blocking', -- blocking | warning | info
    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS compliance_checks (
    check_id      TEXT PRIMARY KEY,
    assignment_id TEXT NOT NULL DEFAULT '',
    session_id    TEXT NOT NULL DEFAULT '',
    rule_id       TEXT NOT NULL REFERENCES compliance_rules(rule_id),
    result        TEXT NOT NULL DEFAULT 'pending', -- pass | fail | pending
    violation     BOOLEAN NOT NULL DEFAULT 0,
    checked_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    resolved_by   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_compliance_checks_session ON compliance_checks(session_id);
CREATE INDEX IF NOT EXISTS idx_compliance_checks_rule    ON compliance_checks(rule_id);
