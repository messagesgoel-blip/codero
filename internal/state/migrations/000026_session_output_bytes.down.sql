-- Rollback: remove output_bytes from agent_sessions.
-- Uses ALTER TABLE DROP COLUMN (SQLite 3.35+).
ALTER TABLE agent_sessions DROP COLUMN output_bytes;
