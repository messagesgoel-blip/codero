ALTER TABLE agent_assignments
    ADD COLUMN assignment_substatus TEXT;

CREATE INDEX IF NOT EXISTS idx_agent_assignments_substatus
    ON agent_assignments (assignment_substatus);

INSERT OR IGNORE INTO agent_rules
    (rule_id, rule_name, rule_kind, description, enforcement, violation_action, routing_target, rule_version, active)
VALUES
    (
        'RULE-001',
        'Gate must pass before merge',
        'gate',
        'An agent may not initiate, trigger, or approve a merge to the target branch until all gate checks for that assignment have a result of pass in assignment_rule_checks. Codero intercepts any merge-triggering action and rejects it if this condition is not met.',
        'hard',
        '["block","notify"]',
        'routing_team',
        1,
        1
    ),
    (
        'RULE-002',
        'No silent failure',
        'report',
        'An agent may not transition an assignment to blocked or any terminal state without supplying a substatus from the approved enum. Codero rejects any state transition that arrives without a substatus on these states.',
        'hard',
        '["block","fail"]',
        'routing_team',
        1,
        1
    );
