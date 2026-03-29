-- 000023: index on agent_sessions.started_at for 30-day roster window scans
CREATE INDEX IF NOT EXISTS idx_agent_sessions_started_at ON agent_sessions(started_at);
