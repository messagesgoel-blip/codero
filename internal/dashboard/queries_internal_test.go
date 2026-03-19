package dashboard

import (
	"context"
	"database/sql"
	"fmt"
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
	// No owner_agent in DB; expect branch-name fallback per resolveOwnerAgent.
	if sessions[0].OwnerAgent != "feat/COD-001-first" || sessions[1].OwnerAgent != "feat/COD-002-unique" {
		t.Fatalf("owner_agent values must fall back to branch name: %+v", sessions)
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
