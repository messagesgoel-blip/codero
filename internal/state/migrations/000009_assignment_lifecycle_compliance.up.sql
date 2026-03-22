ALTER TABLE agent_assignments
    ADD COLUMN state TEXT NOT NULL DEFAULT 'active';

ALTER TABLE agent_assignments
    ADD COLUMN blocked_reason TEXT NOT NULL DEFAULT '';

UPDATE agent_assignments
SET
    state = CASE
        WHEN ended_at IS NULL THEN
            CASE
                WHEN assignment_substatus LIKE 'blocked_%' THEN 'blocked'
                WHEN assignment_substatus = 'terminal_cancelled' THEN 'cancelled'
                WHEN assignment_substatus IN ('terminal_lost', 'terminal_stuck_abandoned') THEN 'lost'
                WHEN assignment_substatus LIKE 'terminal_%' THEN 'completed'
                ELSE 'active'
            END
        ELSE
            CASE
                WHEN end_reason = 'superseded' THEN 'superseded'
                WHEN assignment_substatus LIKE 'blocked_%' THEN 'blocked'
                WHEN assignment_substatus = 'terminal_cancelled' OR end_reason IN ('cancelled', 'canceled') THEN 'cancelled'
                WHEN assignment_substatus IN ('terminal_lost', 'terminal_stuck_abandoned') OR end_reason IN ('expired', 'lost', 'stuck_abandoned') THEN 'lost'
                WHEN assignment_substatus LIKE 'terminal_%' THEN 'completed'
                ELSE 'completed'
            END
    END,
    blocked_reason = CASE
        WHEN assignment_substatus LIKE 'blocked_%' THEN substr(assignment_substatus, 9)
        ELSE ''
    END;

INSERT OR IGNORE INTO agent_rules
    (rule_id, rule_name, rule_kind, description, enforcement, violation_action, routing_target, rule_version, active)
VALUES
    (
        'RULE-003',
        'Branch hold TTL',
        'hold',
        'An agent may not hold ownership of a branch for longer than branch_hold_TTL (default: 72 hours). At 1.5x TTL, Codero forcibly releases branch ownership and transitions the assignment to cancelled.',
        'hard',
        '["block","notify"]',
        'tech_lead',
        1,
        1
    ),
    (
        'RULE-004',
        'Heartbeat and progress protocol',
        'protocol',
        'An agent must emit a heartbeat every 30 seconds and advance progress_at at least every 60 minutes. Failure on heartbeat triggers the lost path. Failure on progress triggers the stuck path.',
        'hard',
        '["block","log","notify"]',
        'infra',
        1,
        1
    );
