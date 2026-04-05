-- Extend session_activity with richer cumulative telemetry counters.
ALTER TABLE session_activity ADD COLUMN runtime_bytes INTEGER NOT NULL DEFAULT 0 CHECK (runtime_bytes >= 0);
ALTER TABLE session_activity ADD COLUMN output_lines INTEGER NOT NULL DEFAULT 0 CHECK (output_lines >= 0);
ALTER TABLE session_activity ADD COLUMN tool_calls INTEGER NOT NULL DEFAULT 0 CHECK (tool_calls >= 0);
ALTER TABLE session_activity ADD COLUMN file_writes INTEGER NOT NULL DEFAULT 0 CHECK (file_writes >= 0);
ALTER TABLE session_activity ADD COLUMN diff_changes INTEGER NOT NULL DEFAULT 0 CHECK (diff_changes >= 0);
ALTER TABLE session_activity ADD COLUMN proc_events INTEGER NOT NULL DEFAULT 0 CHECK (proc_events >= 0);
