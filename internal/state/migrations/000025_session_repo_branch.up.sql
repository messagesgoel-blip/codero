-- WIRE-001: Add repo and branch columns to agent_sessions so heartbeat hooks
-- can report which repo/branch the agent is working in, even without a formal
-- assignment attachment.
ALTER TABLE agent_sessions ADD COLUMN repo TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_sessions ADD COLUMN branch TEXT NOT NULL DEFAULT '';
