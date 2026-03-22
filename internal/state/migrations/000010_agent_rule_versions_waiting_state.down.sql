PRAGMA foreign_keys=OFF;

DROP INDEX IF EXISTS idx_assignment_rule_checks_assignment_rule_version;
DROP INDEX IF EXISTS idx_assignment_rule_checks_rule;
DROP INDEX IF EXISTS idx_assignment_rule_checks_assignment;

CREATE TABLE agent_rules_old (
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

CREATE TABLE assignment_rule_checks_old (
    check_id                 TEXT     NOT NULL PRIMARY KEY,
    assignment_id            TEXT     NOT NULL REFERENCES agent_assignments(assignment_id) ON DELETE CASCADE,
    session_id               TEXT     NOT NULL REFERENCES agent_sessions(session_id) ON DELETE CASCADE,
    rule_id                  TEXT     NOT NULL REFERENCES agent_rules_old(rule_id) ON DELETE CASCADE,
    rule_version             INTEGER  NOT NULL DEFAULT 1,
    checked_at               DATETIME NOT NULL DEFAULT (datetime('now')),
    result                   TEXT     NOT NULL,
    violation_raised         INTEGER  NOT NULL DEFAULT 0,
    violation_action_taken   TEXT     NOT NULL DEFAULT '[]',
    detail                   TEXT     NOT NULL DEFAULT '',
    resolved_at              DATETIME,
    resolved_by              TEXT     NOT NULL DEFAULT ''
);

INSERT INTO agent_rules_old (
    rule_id, rule_name, rule_kind, description, enforcement,
    violation_action, routing_target, rule_version, active
)
SELECT
    ar.rule_id, ar.rule_name, ar.rule_kind, ar.description, ar.enforcement,
    ar.violation_action, ar.routing_target, ar.rule_version, ar.active
FROM agent_rules ar
WHERE NOT EXISTS (
    SELECT 1
    FROM agent_rules newer
    WHERE newer.rule_id = ar.rule_id
      AND (
          newer.active > ar.active OR
          (newer.active = ar.active AND newer.rule_version > ar.rule_version)
      )
);

INSERT INTO assignment_rule_checks_old (
    check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
    result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
)
SELECT
    check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
    result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
FROM assignment_rule_checks;

DROP TABLE assignment_rule_checks;
DROP INDEX IF EXISTS idx_agent_rules_active_version;
DROP INDEX IF EXISTS idx_agent_rules_active_unique;
DROP TABLE agent_rules;

ALTER TABLE agent_rules_old RENAME TO agent_rules;
ALTER TABLE assignment_rule_checks_old RENAME TO assignment_rule_checks;

CREATE INDEX IF NOT EXISTS idx_assignment_rule_checks_assignment
    ON assignment_rule_checks (assignment_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS idx_assignment_rule_checks_rule
    ON assignment_rule_checks (rule_id, checked_at DESC);

UPDATE agent_assignments
SET state = 'active',
    blocked_reason = ''
WHERE ended_at IS NULL
  AND assignment_substatus = 'waiting_for_merge_approval';

PRAGMA foreign_keys=ON;
