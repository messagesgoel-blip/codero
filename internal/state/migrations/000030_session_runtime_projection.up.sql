-- Persist runtime attribution and recovery metadata used by the canonical
-- dashboard projection layer.
ALTER TABLE agent_sessions ADD COLUMN attribution_source TEXT NOT NULL DEFAULT 'unknown'
    CHECK (attribution_source IN ('unknown', 'explicit_heartbeat', 'hook_metadata', 'launch_context', 'assignment_state', 'unresolved'));
ALTER TABLE agent_sessions ADD COLUMN attribution_confidence TEXT NOT NULL DEFAULT 'unknown'
    CHECK (attribution_confidence IN ('unknown', 'high', 'medium', 'low'));
ALTER TABLE agent_sessions ADD COLUMN last_recovered_at DATETIME;
