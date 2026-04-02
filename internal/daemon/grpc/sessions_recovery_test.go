package grpc

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestSessionRecoveryService(t *testing.T) {
	// Create an in-memory SQLite database for testing
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Manually run the schema creation SQL since we can't call state.Migrate
	// This replicates what's in the migration files
	_, err = db.Exec(`
		CREATE TABLE agent_sessions (
			session_id  TEXT     NOT NULL PRIMARY KEY,
			agent_id    TEXT     NOT NULL,
			mode        TEXT     NOT NULL DEFAULT '',
			started_at  DATETIME NOT NULL DEFAULT (datetime('now')),
			last_seen_at DATETIME NOT NULL DEFAULT (datetime('now')),
			ended_at    DATETIME,
			end_reason  TEXT     NOT NULL DEFAULT '',
			last_progress_at DATETIME,
			tmux_session_name TEXT NOT NULL DEFAULT '',
			last_io_at DATETIME,
			inferred_status TEXT NOT NULL DEFAULT 'unknown',
			inferred_status_updated_at DATETIME
		);

		CREATE INDEX idx_agent_sessions_agent_id ON agent_sessions (agent_id);
		CREATE INDEX idx_agent_sessions_last_seen ON agent_sessions (last_seen_at);
		CREATE INDEX idx_agent_sessions_last_io_at ON agent_sessions (last_io_at);
	`)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	service := NewSessionRecoveryService(db, nil)

	t.Run("RecoverActiveSessions finds active sessions", func(t *testing.T) {
		// Insert a few test sessions: one active, one ended, one idle
		now := time.Now()
		activeSessionID := "active-session-123"
		endedSessionID := "ended-session-456"
		
		// Active session (not ended)
		_, err = db.ExecContext(ctx, `
			INSERT INTO agent_sessions (
				session_id, agent_id, mode, started_at, last_seen_at, 
				last_progress_at, tmux_session_name, last_io_at, inferred_status, inferred_status_updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			activeSessionID, "test-agent", "agent", now, now,
			now, "tmux-test", now, "working", now,
		)
		if err != nil {
			t.Fatal(err)
		}

		// Ended session (has ended_at)
		_, err = db.ExecContext(ctx, `
			INSERT INTO agent_sessions (
				session_id, agent_id, mode, started_at, last_seen_at, ended_at,
				end_reason, last_progress_at, tmux_session_name, last_io_at, inferred_status, inferred_status_updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			endedSessionID, "test-agent", "agent", now.Add(-1*time.Hour), now.Add(-30*time.Minute), now.Add(-10*time.Minute),
			"completed", now.Add(-30*time.Minute), "tmux-ended", now.Add(-30*time.Minute), "idle", now.Add(-30*time.Minute),
		)
		if err != nil {
			t.Fatal(err)
		}

		// Recover active sessions
		err = service.RecoverActiveSessions(ctx)
		if err != nil {
			t.Fatal(err)
		}

		// Verify the active session can be recovered but ended session cannot
		active, err := service.IsSessionRecoverable(ctx, activeSessionID, "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		if !active {
			t.Errorf("Expected active session to be recoverable, but it was not")
		}

		ended, err := service.IsSessionRecoverable(ctx, endedSessionID, "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		if ended {
			t.Errorf("Expected ended session to not be recoverable, but it was")
		}
	})

	t.Run("IsSessionRecoverable handles non-existent sessions", func(t *testing.T) {
		recoverable, err := service.IsSessionRecoverable(ctx, "nonexistent-session", "test-agent")
		if err != nil {
			t.Fatal(err)
		}
		if recoverable {
			t.Errorf("Expected non-existent session to not be recoverable, but it was")
		}
	})

	t.Run("RecoverActiveSessions handles empty database", func(t *testing.T) {
		service := NewSessionRecoveryService(db, nil)
		err := service.RecoverActiveSessions(ctx)
		if err != nil {
			t.Fatal(err)
		}
	})
}