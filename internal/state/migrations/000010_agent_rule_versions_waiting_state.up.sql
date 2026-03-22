PRAGMA foreign_keys=OFF;

CREATE TABLE agent_rules_v2 (
    rule_id           TEXT     NOT NULL,
    rule_name         TEXT     NOT NULL,
    rule_kind         TEXT     NOT NULL,
    description       TEXT     NOT NULL DEFAULT '',
    enforcement       TEXT     NOT NULL DEFAULT 'hard',
    violation_action  TEXT     NOT NULL DEFAULT '[]',
    routing_target    TEXT     NOT NULL DEFAULT '',
    rule_version      INTEGER  NOT NULL DEFAULT 1,
    active            INTEGER  NOT NULL DEFAULT 1,
    PRIMARY KEY (rule_id, rule_version)
);

INSERT INTO agent_rules_v2 (
    rule_id, rule_name, rule_kind, description, enforcement,
    violation_action, routing_target, rule_version, active
)
SELECT
    rule_id, rule_name, rule_kind, description, enforcement,
    violation_action, routing_target, rule_version, active
FROM agent_rules;

DROP TABLE agent_rules;

ALTER TABLE agent_rules_v2 RENAME TO agent_rules;

CREATE UNIQUE INDEX idx_agent_rules_active_unique
    ON agent_rules (rule_id)
    WHERE active = 1;

CREATE INDEX idx_agent_rules_active_version
    ON agent_rules (rule_id, active, rule_version DESC);

CREATE TABLE assignment_rule_checks_v2 (
    check_id                 TEXT     NOT NULL PRIMARY KEY,
    assignment_id            TEXT     NOT NULL REFERENCES agent_assignments(assignment_id) ON DELETE CASCADE,
    session_id               TEXT     NOT NULL REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
    rule_id                  TEXT     NOT NULL,
    rule_version             INTEGER  NOT NULL DEFAULT 1,
    checked_at               DATETIME NOT NULL DEFAULT (datetime('now')),
    result                   TEXT     NOT NULL,
    violation_raised         INTEGER  NOT NULL DEFAULT 0,
    violation_action_taken   TEXT     NOT NULL DEFAULT '[]',
    detail                   TEXT     NOT NULL DEFAULT '',
    resolved_at              DATETIME,
    resolved_by              TEXT     NOT NULL DEFAULT '',
    FOREIGN KEY (rule_id, rule_version) REFERENCES agent_rules(rule_id, rule_version) ON DELETE CASCADE
);

INSERT INTO assignment_rule_checks_v2 (
    check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
    result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
)
SELECT
    check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
    result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
FROM assignment_rule_checks;

DROP TABLE assignment_rule_checks;

ALTER TABLE assignment_rule_checks_v2 RENAME TO assignment_rule_checks;

CREATE INDEX idx_assignment_rule_checks_assignment
    ON assignment_rule_checks (assignment_id, checked_at DESC);

CREATE INDEX idx_assignment_rule_checks_rule
    ON assignment_rule_checks (rule_id, rule_version, checked_at DESC);

CREATE UNIQUE INDEX idx_assignment_rule_checks_assignment_rule_version
    ON assignment_rule_checks (assignment_id, rule_id, rule_version);

UPDATE agent_assignments
SET state = 'blocked',
    blocked_reason = ''
WHERE ended_at IS NULL
  AND assignment_substatus = 'waiting_for_merge_approval';

PRAGMA foreign_keys=ON;
