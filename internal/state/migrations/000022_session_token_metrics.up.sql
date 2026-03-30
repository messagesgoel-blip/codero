-- Session token metrics: one row per LiteLLM request correlated to a codero session.
CREATE TABLE IF NOT EXISTS session_token_metrics (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id                  TEXT    NOT NULL,
    litellm_request_id          TEXT    UNIQUE, -- dedup key; NULL for non-LiteLLM sources
    model                       TEXT    NOT NULL DEFAULT '',
    prompt_tokens               INTEGER NOT NULL DEFAULT 0,
    completion_tokens           INTEGER NOT NULL DEFAULT 0,
    cumulative_prompt_tokens    INTEGER NOT NULL DEFAULT 0,  -- running sum up to this request
    cumulative_completion_tokens INTEGER NOT NULL DEFAULT 0,
    request_time                DATETIME NOT NULL,
    synced_at                   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_stm_session_id   ON session_token_metrics(session_id);
CREATE INDEX IF NOT EXISTS idx_stm_request_time ON session_token_metrics(session_id, request_time);
CREATE INDEX IF NOT EXISTS idx_stm_synced_at    ON session_token_metrics(synced_at);

-- Context pressure tracking on the session row itself.
ALTER TABLE agent_sessions ADD COLUMN context_pressure   TEXT    DEFAULT 'normal';   -- normal|warning|critical
ALTER TABLE agent_sessions ADD COLUMN last_compact_at    DATETIME;
ALTER TABLE agent_sessions ADD COLUMN compact_count      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_sessions ADD COLUMN litellm_session_id TEXT;                       -- LiteLLM session_id (UUID) for correlation

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_sessions_litellm_session_id ON agent_sessions(litellm_session_id) WHERE litellm_session_id IS NOT NULL;
