package dashboard

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/codero/codero/internal/state"
)

func openDashboardQueryTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := state.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db.Unwrap()
}

func seedBranchSessionForQueryTest(t *testing.T, db *sql.DB, repo, branch, st, sessionID string, lastSeen, submittedAt time.Time) {
	t.Helper()
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, err := db.Exec(`INSERT INTO branch_states
		(id, repo, branch, head_hash, state, retry_count, max_retries, approved, ci_green,
		 pending_events, unresolved_threads, owner_session_id, owner_session_last_seen,
		 queue_priority, submission_time, created_at, updated_at)
		VALUES (?,?,?,?,?,0,3,0,0,0,0,?,?,?,?,?,?)`,
		fmt.Sprintf("id-%s-%s", repo, branch), repo, branch, "abc123", st,
		sessionID, lastSeen, 0, submittedAt, submittedAt, submittedAt)
	if err != nil {
		t.Fatalf("seedBranchSessionForQueryTest: %v", err)
	}
}

func TestActiveSessions_DedupeBeforeLimit(t *testing.T) {
	db := openDashboardQueryTestDB(t)
	now := time.Now().UTC()

	// Two rows share the same owner_session_id and are the freshest rows.
	seedBranchSessionForQueryTest(t, db, "acme/api", "feat/COD-001-first", "coding", "sess-dup", now, now.Add(-20*time.Minute))
	seedBranchSessionForQueryTest(t, db, "acme/api", "feat/COD-001-second", "queued_cli", "sess-dup", now.Add(-1*time.Second), now.Add(-20*time.Minute))
	// Distinct session follows behind them in the sort order.
	seedBranchSessionForQueryTest(t, db, "acme/web", "feat/COD-002-unique", "coding", "sess-unique", now.Add(-10*time.Second), now.Add(-30*time.Minute))

	sessions, err := queryActiveSessions(context.Background(), db, 2)
	if err != nil {
		t.Fatalf("queryActiveSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}
	if sessions[0].SessionID != "sess-dup" {
		t.Fatalf("sessions[0].session_id = %q, want sess-dup", sessions[0].SessionID)
	}
	if sessions[1].SessionID != "sess-unique" {
		t.Fatalf("sessions[1].session_id = %q, want sess-unique", sessions[1].SessionID)
	}
	if sessions[0].OwnerAgent != "unknown" || sessions[1].OwnerAgent != "unknown" {
		t.Fatalf("owner_agent values must remain unknown: %+v", sessions)
	}
}
