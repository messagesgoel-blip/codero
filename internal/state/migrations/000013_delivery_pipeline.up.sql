-- Delivery pipeline state columns on agent_assignments.

ALTER TABLE agent_assignments ADD COLUMN delivery_state    TEXT     NOT NULL DEFAULT 'idle';
ALTER TABLE agent_assignments ADD COLUMN last_submit_at    DATETIME;
ALTER TABLE agent_assignments ADD COLUMN last_gate_result  TEXT     NOT NULL DEFAULT '';
ALTER TABLE agent_assignments ADD COLUMN last_commit_sha   TEXT     NOT NULL DEFAULT '';
ALTER TABLE agent_assignments ADD COLUMN last_push_at      DATETIME;
ALTER TABLE agent_assignments ADD COLUMN revision_count    INTEGER  NOT NULL DEFAULT 0;
