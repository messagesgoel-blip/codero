CREATE TABLE IF NOT EXISTS agent_rules (
    rule_id           TEXT     NOT NULL PRIMARY KEY,
    rule_name         TEXT     NOT NULL,
    rule_kind         TEXT     NOT NULL,
    description       TEXT     NOT NULL DEFAULT '',
    enforcement       TEXT     NOT NULL DEFAULT 'hard',
    violation_action  TEXT     NOT NULL DEFAULT '[]',
    routing_target    TEXT     NOT NULL DEFAULT '',
    rule_version      INTEGER  NOT NULL DEFAULT 1,
    active            INTEGER  NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS assignment_rule_checks (
    check_id                 TEXT     NOT NULL PRIMARY KEY,
    assignment_id            TEXT     NOT NULL REFERENCES agent_assignments(assignment_id) ON DELETE CASCADE,
    session_id               TEXT     NOT NULL REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
    rule_id                  TEXT     NOT NULL REFERENCES agent_rules(rule_id) ON DELETE CASCADE,
    rule_version             INTEGER  NOT NULL DEFAULT 1,
    checked_at               DATETIME NOT NULL DEFAULT (datetime('now')),
    result                   TEXT     NOT NULL,
    violation_raised         INTEGER  NOT NULL DEFAULT 0,
    violation_action_taken   TEXT     NOT NULL DEFAULT '[]',
    detail                   TEXT     NOT NULL DEFAULT '',
    resolved_at              DATETIME,
    resolved_by              TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_assignment_rule_checks_assignment
    ON assignment_rule_checks (assignment_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_assignment_rule_checks_rule
    ON assignment_rule_checks (rule_id, checked_at DESC);
