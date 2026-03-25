-- SL-11: Store the tmux session name alongside the durable session row.
ALTER TABLE agent_sessions ADD COLUMN tmux_session_name TEXT NOT NULL DEFAULT '';
