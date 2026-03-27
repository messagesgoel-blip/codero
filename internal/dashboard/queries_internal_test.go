package dashboard

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	seedAgentAssignmentForQueryTestWithSubstatus(t, db, assignmentID, sessionID, agentID, repo, branch, "", startedAt)
}

func seedAgentAssignmentForQueryTestWithSubstatus(t *testing.T, db *sql.DB, assignmentID, sessionID, agentID, repo, branch, substatus string, startedAt time.Time) {
	t.Helper()
	stateValue := "active"
	blockedReason := ""
	switch {
	case substatus == "waiting_for_merge_approval":
		stateValue = "blocked"
	case strings.HasPrefix(substatus, "blocked_"):
		stateValue = "blocked"
		blockedReason = strings.TrimPrefix(substatus, "blocked_")
	case substatus == "terminal_cancelled":
		stateValue = "cancelled"
	case substatus == "terminal_lost" || substatus == "terminal_stuck_abandoned":
		stateValue = "lost"
	case strings.HasPrefix(substatus, "terminal_"):
		stateValue = "completed"
	}
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, err := db.Exec(`INSERT INTO agent_assignments
		(assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus, started_at, ended_at, end_reason, superseded_by)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,NULL,'',NULL)`,
		assignmentID, sessionID, agentID, repo, branch, "", "", stateValue, blockedReason, substatus, startedAt)
	if err != nil {
		t.Fatalf("seedAgentAssignmentForQueryTestWithSubstatus: %v", err)
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

func TestActiveSessions_AssignmentSubstatusDrivesActivityState(t *testing.T) {
	db := openDashboardQueryTestDB(t)
	now := time.Now().UTC()

	seedAgentSessionForQueryTest(t, db, "sess-wait", "agent-a", now, now.Add(-20*time.Minute))
	seedAgentAssignmentForQueryTestWithSubstatus(t, db, "assign-wait", "sess-wait", "agent-a", "acme/api", "feat/COD-071-waiting", "waiting_for_merge_approval", now.Add(-10*time.Minute))

	sessions, err := queryActiveSessions(context.Background(), db, 1)
	if err != nil {
		t.Fatalf("queryActiveSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if sessions[0].ActivityState != "waiting" {
		t.Fatalf("activity_state = %q, want waiting", sessions[0].ActivityState)
	}
}

func TestAssignmentStateFromSubstatus_WaitingForMergeApprovalBlocked(t *testing.T) {
	if got := assignmentStateFromSubstatus("waiting_for_merge_approval"); got != "blocked" {
		t.Fatalf("assignmentStateFromSubstatus(waiting_for_merge_approval) = %q, want blocked", got)
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

func TestPercentile_SyntheticFixture(t *testing.T) {
	tests := []struct {
		name     string
		sorted   []float64
		p        float64
		expected float64
	}{
		{
			name:     "single value p50",
			sorted:   []float64{10},
			p:        0.50,
			expected: 10,
		},
		{
			name:     "two values p50",
			sorted:   []float64{10, 20},
			p:        0.50,
			expected: 15, // midpoint
		},
		{
			name:     "three values p50",
			sorted:   []float64{10, 20, 30},
			p:        0.50,
			expected: 20, // exact middle
		},
		{
			name:     "five values p50",
			sorted:   []float64{5, 10, 15, 20, 30},
			p:        0.50,
			expected: 15,
		},
		{
			name:     "five values p90",
			sorted:   []float64{5, 10, 15, 20, 30},
			p:        0.90,
			expected: 26, // 0.9 * 4 = 3.6 → interpolate between index 3 (20) and 4 (30)
		},
		{
			name:     "five values p0",
			sorted:   []float64{5, 10, 15, 20, 30},
			p:        0.0,
			expected: 5,
		},
		{
			name:     "five values p100",
			sorted:   []float64{5, 10, 15, 20, 30},
			p:        1.0,
			expected: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.sorted, tt.p)
			if got != tt.expected {
				t.Errorf("percentile(%v, %.2f) = %.2f, want %.2f", tt.sorted, tt.p, got, tt.expected)
			}
		})
	}
}

func TestQueryETADetail_SyntheticFixture(t *testing.T) {
	db := openDashboardQueryTestDB(t)
	ctx := context.Background()

	// Seed completed runs with varying durations
	now := time.Now().UTC()
	// Create runs with durations: 10, 20, 30, 40, 60 minutes
	durations := []int{10, 20, 30, 40, 60}
	for i, dur := range durations {
		started := now.Add(-time.Duration(24*i) * time.Hour).Add(-time.Duration(dur) * time.Minute)
		finished := started.Add(time.Duration(dur) * time.Minute)
		// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
		_, err := db.Exec(`INSERT INTO review_runs
			(id, repo, branch, provider, status, started_at, finished_at, created_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			fmt.Sprintf("run-%d", i), "acme/api", "feat/COD-001-test", "litellm", "completed", started, finished, started)
		if err != nil {
			t.Fatalf("seed run %d: %v", i, err)
		}
	}

	// Add a running run (currently 5 minutes elapsed)
	runningStarted := now.Add(-5 * time.Minute)
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, err := db.Exec(`INSERT INTO review_runs
		(id, repo, branch, provider, status, started_at, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		"run-running", "acme/api", "feat/COD-002-active", "litellm", "running", runningStarted, runningStarted)
	if err != nil {
		t.Fatalf("seed running run: %v", err)
	}

	detail := queryETADetail(ctx, db, "", "")
	if detail == nil {
		t.Fatal("expected non-nil ETADetail")
	}

	// With durations [10, 20, 30, 40, 60]:
	// avg = (10+20+30+40+60)/5 = 32
	// p50 (index 2) = 30
	// p90 (index 3.6) = 40 + 0.6*(60-40) = 52
	// elapsed = 5
	// eta = 30 - 5 = 25

	if detail.AvgMin != 32 {
		t.Errorf("AvgMin = %d, want 32", detail.AvgMin)
	}
	if detail.P50Min != 30 {
		t.Errorf("P50Min = %d, want 30", detail.P50Min)
	}
	if detail.P90Min != 52 {
		t.Errorf("P90Min = %d, want 52", detail.P90Min)
	}
	if detail.ElapsedMin != 5 {
		t.Errorf("ElapsedMin = %d, want 5", detail.ElapsedMin)
	}
	if detail.ETAMin != 25 {
		t.Errorf("ETAMin = %d, want 25", detail.ETAMin)
	}
}

func TestQueryETADetail_BranchPrefixFilter(t *testing.T) {
	db := openDashboardQueryTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed runs on different branch prefixes with different duration patterns
	// feat/fix/* branches: quick fixes with durations 5, 10, 15 minutes
	// feat/refactor/* branches: larger work with durations 30, 60, 90 minutes
	seedRunWithDuration := func(id, branch string, dur int, started time.Time) {
		finished := started.Add(time.Duration(dur) * time.Minute)
		// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
		_, err := db.Exec(`INSERT INTO review_runs
			(id, repo, branch, provider, status, started_at, finished_at, created_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			id, "acme/api", branch, "litellm", "completed", started, finished, started)
		if err != nil {
			t.Fatalf("seed run %s: %v", id, err)
		}
	}

	seedRunWithDuration("fix-1", "feat/fix/bug-001", 5, now.Add(-2*time.Hour))
	seedRunWithDuration("fix-2", "feat/fix/bug-002", 10, now.Add(-3*time.Hour))
	seedRunWithDuration("fix-3", "feat/fix/bug-003", 15, now.Add(-4*time.Hour))

	seedRunWithDuration("refactor-1", "feat/refactor/api-001", 30, now.Add(-26*time.Hour))
	seedRunWithDuration("refactor-2", "feat/refactor/api-002", 60, now.Add(-27*time.Hour))
	seedRunWithDuration("refactor-3", "feat/refactor/api-003", 90, now.Add(-28*time.Hour))

	// Query with branch_prefix=feat/fix/
	detailFix := queryETADetail(ctx, db, "", "feat/fix/")
	if detailFix == nil {
		t.Fatal("expected non-nil ETADetail for feat/fix/")
	}
	// avg = (5+10+15)/3 = 10, p50 = 10
	if detailFix.P50Min != 10 {
		t.Errorf("feat/fix/ P50Min = %d, want 10", detailFix.P50Min)
	}

	// Query with branch_prefix=feat/refactor/
	detailRefactor := queryETADetail(ctx, db, "", "feat/refactor/")
	if detailRefactor == nil {
		t.Fatal("expected non-nil ETADetail for feat/refactor/")
	}
	// avg = (30+60+90)/3 = 60, p50 = 60
	if detailRefactor.P50Min != 60 {
		t.Errorf("feat/refactor/ P50Min = %d, want 60", detailRefactor.P50Min)
	}

	// Query without prefix (all runs combined)
	detailAll := queryETADetail(ctx, db, "", "")
	if detailAll == nil {
		t.Fatal("expected non-nil ETADetail for all runs")
	}
	// avg = (5+10+15+30+60+90)/6 = 35
	if detailAll.AvgMin != 35 {
		t.Errorf("all runs AvgMin = %d, want 35", detailAll.AvgMin)
	}
}

func TestQueryETADetail_NoHistory(t *testing.T) {
	db := openDashboardQueryTestDB(t)
	ctx := context.Background()

	detail := queryETADetail(ctx, db, "", "")
	if detail != nil {
		t.Errorf("expected nil ETADetail with no history, got %+v", detail)
	}
}
