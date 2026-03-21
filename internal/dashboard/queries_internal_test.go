package dashboard

import (
	"context"
	"database/sql"
	"os"
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

func seedAgentSessionForQueryTest(t *testing.T, db *sql.DB, sessionID, agentID string, lastSeen, startedAt time.Time) {
	t.Helper()
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, err := db.Exec(`INSERT INTO agent_sessions
		(session_id, agent_id, mode, started_at, last_seen_at, ended_at, end_reason)
		VALUES (?,?,?,?,?,NULL,'')`,
		sessionID, agentID, "cli", startedAt, lastSeen)
	if err != nil {
		t.Fatalf("seedAgentSessionForQueryTest: %v", err)
	}
}

func seedAgentAssignmentForQueryTest(t *testing.T, db *sql.DB, assignmentID, sessionID, agentID, repo, branch string, startedAt time.Time) {
	t.Helper()
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, err := db.Exec(`INSERT INTO agent_assignments
		(assignment_id, session_id, agent_id, repo, branch, worktree, task_id, started_at, ended_at, end_reason, superseded_by)
		VALUES (?,?,?,?,?,?,?,?,NULL,'',NULL)`,
		assignmentID, sessionID, agentID, repo, branch, "", "", startedAt)
	if err != nil {
		t.Fatalf("seedAgentAssignmentForQueryTest: %v", err)
	}
}

func TestActiveSessions_DedupeBeforeLimit(t *testing.T) {
	db := openDashboardQueryTestDB(t)
	now := time.Now().UTC()

	seedAgentSessionForQueryTest(t, db, "sess-dup", "agent-a", now, now.Add(-20*time.Minute))
	seedAgentSessionForQueryTest(t, db, "sess-unique", "agent-b", now.Add(-10*time.Second), now.Add(-30*time.Minute))
	seedAgentAssignmentForQueryTest(t, db, "assign-1", "sess-dup", "agent-a", "acme/api", "feat/COD-001-first", now.Add(-20*time.Minute))
	seedAgentAssignmentForQueryTest(t, db, "assign-2", "sess-unique", "agent-b", "acme/web", "feat/COD-002-unique", now.Add(-30*time.Minute))

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
	if sessions[0].OwnerAgent != "agent-a" || sessions[1].OwnerAgent != "agent-b" {
		t.Fatalf("owner_agent values must match agent_id: %+v", sessions)
	}
}

func TestParseCoverageFilePath_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "coverage.out")
	content := "mode: set\ngithub.com/codero/codero/internal/dashboard/queries.go:10.20,15.2 3 1\ngithub.com/codero/codero/internal/dashboard/queries.go:20.10,25.2 2 0\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write coverage file: %v", err)
	}
	pct := parseCoverageFilePath(path)
	if pct == nil {
		t.Fatal("expected non-nil coverage pct")
	}
	// 3 stmts covered out of 5 total → 60%
	if got := *pct; got < 59.9 || got > 60.1 {
		t.Errorf("coverage pct = %.2f, want ~60.0", got)
	}
}

func TestParseCoverageFilePath_MissingFile(t *testing.T) {
	pct := parseCoverageFilePath(filepath.Join(t.TempDir(), "missing-coverage.out"))
	if pct != nil {
		t.Errorf("expected nil for missing file, got %v", pct)
	}
}

func TestCoveragePath_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-coverage.out")
	content := "mode: set\ngithub.com/codero/codero/x.go:1.1,2.1 4 1\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write coverage file: %v", err)
	}
	t.Setenv("CODERO_COVERAGE_PATH", path)

	// Validate through queryDashboardHealth so env resolution and parsing are
	// both exercised together (avoids duplicating the resolution logic here).
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer db.Close()

	h, err := queryDashboardHealth(context.Background(), db.Unwrap())
	if err != nil {
		t.Fatalf("queryDashboardHealth: %v", err)
	}
	if h.CoveragePct == nil {
		t.Fatal("expected non-nil CoveragePct with CODERO_COVERAGE_PATH set")
	}
	if got := *h.CoveragePct; got < 99.9 || got > 100.1 {
		t.Errorf("CoveragePct = %.2f, want 100.0", got)
	}
}
