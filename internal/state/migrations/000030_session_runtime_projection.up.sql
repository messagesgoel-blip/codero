-- Persist runtime attribution and recovery metadata used by the canonical
-- dashboard projection layer.
ALTER TABLE agent_sessions ADD COLUMN attribution_source TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE agent_sessions ADD COLUMN attribution_confidence TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE agent_sessions ADD COLUMN last_recovered_at DATETIME;
