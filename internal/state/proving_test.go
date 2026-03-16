package state

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestProvingScorecard_Empty(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	card, err := ComputeProvingScorecard(context.Background(), db)
	if err != nil {
		t.Fatalf("ComputeProvingScorecard failed: %v", err)
	}

	if card.BranchesReviewed7Days != 0 {
		t.Errorf("expected 0 branches reviewed, got %d", card.BranchesReviewed7Days)
	}
	if card.StaleDetections30Days != 0 {
		t.Errorf("expected 0 stale detections, got %d", card.StaleDetections30Days)
	}
	if card.LeaseExpiryRecoveries != 0 {
		t.Errorf("expected 0 lease expiry recoveries, got %d", card.LeaseExpiryRecoveries)
	}
	if card.PrecommitReviews7Days != 0 {
		t.Errorf("expected 0 precommit reviews, got %d", card.PrecommitReviews7Days)
	}
	if card.MissedFeedbackDeliveries != 0 {
		t.Errorf("expected 0 missed deliveries, got %d", card.MissedFeedbackDeliveries)
	}
	if card.QueueStallIncidents != 0 {
		t.Errorf("expected 0 queue stalls, got %d", card.QueueStallIncidents)
	}
	if card.UnresolvedThreadFailures != 0 {
		t.Errorf("expected 0 unresolved thread failures, got %d", card.UnresolvedThreadFailures)
	}
	if card.ManualDBRepairs != 0 {
		t.Errorf("expected 0 manual DB repairs, got %d", card.ManualDBRepairs)
	}
}

func TestProvingScorecard_WithData(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	now := time.Now()

	_, err := db.sql.Exec(`
		INSERT INTO review_runs (id, repo, branch, head_hash, provider, status, started_at)
		VALUES 
			('run-1', 'owner/repo1', 'branch-a', 'abc123', 'stub', 'completed', ?),
			('run-2', 'owner/repo1', 'branch-b', 'def456', 'stub', 'completed', ?),
			('run-3', 'owner/repo2', 'branch-c', 'ghi789', 'stub', 'completed', ?),
			('run-4', 'owner/repo1', 'branch-d', 'jkl012', 'stub', 'running', ?)
	`, now.AddDate(0, 0, -3), now.AddDate(0, 0, -5), now.AddDate(0, 0, -6), now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("insert review runs: %v", err)
	}

	_, err = db.sql.Exec(`
		INSERT INTO state_transitions (branch_state_id, from_state, to_state, trigger, created_at)
		VALUES 
			('bs-1', 'merge_ready', 'stale_branch', 'head_mismatch', ?),
			('bs-2', 'coding', 'stale_branch', 'stale_detected', ?),
			('bs-3', 'cli_reviewing', 'queued_cli', 'lease_expired', ?)
	`, now.AddDate(0, 0, -10), now.AddDate(0, 0, -15), now.AddDate(0, 0, -20))
	if err != nil {
		t.Fatalf("insert state transitions: %v", err)
	}

	_, err = db.sql.Exec(`
		INSERT INTO precommit_reviews (id, repo, branch, provider, status, created_at)
		VALUES 
			('pc-1', 'owner/repo1', 'branch-a', 'litellm', 'passed', ?),
			('pc-2', 'owner/repo1', 'branch-b', 'litellm', 'passed', ?),
			('pc-3', 'owner/repo2', 'branch-c', 'coderabbit', 'passed', ?)
	`, now.AddDate(0, 0, -3), now.AddDate(0, 0, -4), now.AddDate(0, 0, -5))
	if err != nil {
		t.Fatalf("insert precommit reviews: %v", err)
	}

	_, err = db.sql.Exec(`
		INSERT INTO proving_events (repo, event_type, details, created_at)
		VALUES 
			('owner/repo1', 'queue_stall', '{}', ?),
			('owner/repo2', 'manual_db_repair', '{"reason": "index corruption"}', ?)
	`, now.AddDate(0, 0, -5), now.AddDate(0, 0, -10))
	if err != nil {
		t.Fatalf("insert proving events: %v", err)
	}

	card, err := ComputeProvingScorecard(context.Background(), db)
	if err != nil {
		t.Fatalf("ComputeProvingScorecard failed: %v", err)
	}

	if card.BranchesReviewed7Days != 3 {
		t.Errorf("expected 3 branches reviewed, got %d", card.BranchesReviewed7Days)
	}

	if card.StaleDetections30Days != 2 {
		t.Errorf("expected 2 stale detections, got %d", card.StaleDetections30Days)
	}

	if card.LeaseExpiryRecoveries != 1 {
		t.Errorf("expected 1 lease expiry recovery, got %d", card.LeaseExpiryRecoveries)
	}

	if card.PrecommitReviews7Days != 3 {
		t.Errorf("expected 3 precommit reviews, got %d", card.PrecommitReviews7Days)
	}

	if card.PrecommitReviewsByRepo["owner/repo1"] != 2 {
		t.Errorf("expected 2 precommit reviews for repo1, got %d", card.PrecommitReviewsByRepo["owner/repo1"])
	}

	if card.QueueStallIncidents != 1 {
		t.Errorf("expected 1 queue stall, got %d", card.QueueStallIncidents)
	}

	if card.ManualDBRepairs != 1 {
		t.Errorf("expected 1 manual DB repair, got %d", card.ManualDBRepairs)
	}
}

func TestProvingScorecard_DataWindow(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	now := time.Now()

	_, err := db.sql.Exec(`
		INSERT INTO review_runs (id, repo, branch, head_hash, provider, status, started_at)
		VALUES 
			('run-old', 'owner/repo1', 'branch-old', 'abc123', 'stub', 'completed', ?),
			('run-new', 'owner/repo1', 'branch-new', 'def456', 'stub', 'completed', ?)
	`, now.AddDate(0, 0, -40), now.AddDate(0, 0, -3))
	if err != nil {
		t.Fatalf("insert review runs: %v", err)
	}

	card, err := ComputeProvingScorecard(context.Background(), db)
	if err != nil {
		t.Fatalf("ComputeProvingScorecard failed: %v", err)
	}

	if card.BranchesReviewed7Days != 1 {
		t.Errorf("expected 1 branch reviewed in 7-day window, got %d", card.BranchesReviewed7Days)
	}
}

func TestProvingSnapshot_SaveAndRetrieve(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	snapshotDate := "2026-03-15"
	scorecardJSON := `{"generated_at":"2026-03-15T12:00:00Z","branches_reviewed_7_days":5}`

	err := SaveProvingSnapshot(context.Background(), db, snapshotDate, scorecardJSON)
	if err != nil {
		t.Fatalf("SaveProvingSnapshot failed: %v", err)
	}

	snapshot, err := GetProvingSnapshot(context.Background(), db, snapshotDate)
	if err != nil {
		t.Fatalf("GetProvingSnapshot failed: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snapshot.ScorecardJSON != scorecardJSON {
		t.Errorf("expected %q, got %q", scorecardJSON, snapshot.ScorecardJSON)
	}
}

func TestProvingSnapshot_NotFound(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	snapshot, err := GetProvingSnapshot(context.Background(), db, "2026-01-01")
	if err != nil {
		t.Fatalf("GetProvingSnapshot failed: %v", err)
	}
	if snapshot != nil {
		t.Error("expected nil snapshot for non-existent date")
	}
}

func TestProvingEvent_CreateAndCount(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	err := CreateProvingEvent(context.Background(), db, "queue_stall", "owner/repo1", "{}")
	if err != nil {
		t.Fatalf("CreateProvingEvent failed: %v", err)
	}

	err = CreateProvingEvent(context.Background(), db, "queue_stall", "owner/repo1", "{}")
	if err != nil {
		t.Fatalf("CreateProvingEvent failed: %v", err)
	}

	err = CreateProvingEvent(context.Background(), db, "manual_db_repair", "owner/repo2", "{}")
	if err != nil {
		t.Fatalf("CreateProvingEvent failed: %v", err)
	}

	count, err := CountProvingEvents(context.Background(), db, "queue_stall", time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("CountProvingEvents failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 queue_stall events, got %d", count)
	}

	count, err = CountProvingEvents(context.Background(), db, "manual_db_repair", time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("CountProvingEvents failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 manual_db_repair event, got %d", count)
	}
}

func TestPrecommitReview_CreateAndList(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	review := &PrecommitReview{
		ID:       "pc-1",
		Repo:     "owner/repo1",
		Branch:   "feature/x",
		Provider: "litellm",
		Status:   "passed",
	}

	err := CreatePrecommitReview(context.Background(), db, review)
	if err != nil {
		t.Fatalf("CreatePrecommitReview failed: %v", err)
	}

	since := time.Now().AddDate(0, 0, -7)
	reviews, err := ListPrecommitReviewsByRepo(context.Background(), db, "owner/repo1", since)
	if err != nil {
		t.Fatalf("ListPrecommitReviewsByRepo failed: %v", err)
	}
	if len(reviews) != 1 {
		t.Errorf("expected 1 review, got %d", len(reviews))
	}
	if reviews[0].Provider != "litellm" {
		t.Errorf("expected provider litellm, got %s", reviews[0].Provider)
	}
}

func TestCountBranchesReviewed(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	now := time.Now()
	_, err := db.sql.Exec(`
		INSERT INTO review_runs (id, repo, branch, head_hash, provider, status, started_at)
		VALUES 
			('run-1', 'owner/repo1', 'branch-a', 'abc123', 'stub', 'completed', ?),
			('run-2', 'owner/repo1', 'branch-b', 'def456', 'stub', 'completed', ?),
			('run-3', 'owner/repo2', 'branch-c', 'ghi789', 'stub', 'completed', ?)
	`, now.AddDate(0, 0, -3), now.AddDate(0, 0, -3), now.AddDate(0, 0, -3))
	if err != nil {
		t.Fatalf("insert review_runs: %v", err)
	}

	since := now.AddDate(0, 0, -7)
	total, byRepo, err := CountBranchesReviewed(context.Background(), db, since)
	if err != nil {
		t.Fatalf("CountBranchesReviewed failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 branches reviewed, got %d", total)
	}
	if byRepo["owner/repo1"] != 2 {
		t.Errorf("expected 2 for repo1, got %d", byRepo["owner/repo1"])
	}
	if byRepo["owner/repo2"] != 1 {
		t.Errorf("expected 1 for repo2, got %d", byRepo["owner/repo2"])
	}
}

func TestCountPrecommitReviews(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	now := time.Now()
	_, err := db.sql.Exec(`
		INSERT INTO precommit_reviews (id, repo, branch, provider, status, created_at)
		VALUES 
			('pc-1', 'owner/repo1', 'branch-a', 'litellm', 'passed', ?),
			('pc-2', 'owner/repo1', 'branch-b', 'coderabbit', 'passed', ?),
			('pc-3', 'owner/repo2', 'branch-c', 'litellm', 'failed', ?)
	`, now.AddDate(0, 0, -3), now.AddDate(0, 0, -3), now.AddDate(0, 0, -3))
	if err != nil {
		t.Fatalf("insert precommit_reviews: %v", err)
	}

	since := now.AddDate(0, 0, -7)
	total, byRepo, err := CountPrecommitReviews(context.Background(), db, since)
	if err != nil {
		t.Fatalf("CountPrecommitReviews failed: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 precommit reviews, got %d", total)
	}
	if byRepo["owner/repo1"] != 2 {
		t.Errorf("expected 2 for repo1, got %d", byRepo["owner/repo1"])
	}
}

func setupTestProvingDB(t *testing.T) (*DB, func()) {
	tmpFile, err := os.CreateTemp("", "codero-test-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	dbPath := tmpFile.Name()
	tmpFile.Close()

	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		os.Remove(dbPath)
		t.Fatalf("open database: %v", err)
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			sqlDB.Close()
			os.Remove(dbPath)
			t.Fatalf("exec pragma: %v", err)
		}
	}

	schema := `
		CREATE TABLE IF NOT EXISTS branch_states (
			id TEXT PRIMARY KEY, repo TEXT NOT NULL, branch TEXT NOT NULL,
			head_hash TEXT NOT NULL DEFAULT '', state TEXT NOT NULL,
			retry_count INTEGER NOT NULL DEFAULT 0, max_retries INTEGER NOT NULL DEFAULT 3,
			approved INTEGER NOT NULL DEFAULT 0, ci_green INTEGER NOT NULL DEFAULT 0,
			pending_events INTEGER NOT NULL DEFAULT 0, unresolved_threads INTEGER NOT NULL DEFAULT 0,
			owner_session_id TEXT NOT NULL DEFAULT '', owner_session_last_seen DATETIME,
			queue_priority INTEGER NOT NULL DEFAULT 0, submission_time DATETIME,
			lease_id TEXT, lease_expires_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
			UNIQUE (repo, branch)
		);

		CREATE TABLE IF NOT EXISTS state_transitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			branch_state_id TEXT NOT NULL,
			from_state TEXT NOT NULL, to_state TEXT NOT NULL,
			trigger TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS review_runs (
			id TEXT PRIMARY KEY, repo TEXT NOT NULL, branch TEXT NOT NULL,
			head_hash TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL,
			status TEXT NOT NULL, started_at DATETIME, finished_at DATETIME,
			error TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS precommit_reviews (
			id TEXT PRIMARY KEY, repo TEXT NOT NULL, branch TEXT NOT NULL,
			provider TEXT NOT NULL, status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS proving_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL,
			details TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS proving_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			snapshot_date TEXT NOT NULL UNIQUE,
			scorecard_json TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);
	`

	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		os.Remove(dbPath)
		t.Fatalf("create schema: %v", err)
	}

	db := &DB{sql: sqlDB}

	cleanup := func() {
		db.Close()
		os.Remove(dbPath)
	}

	return db, cleanup
}

// --- CreatePrecommitReviewIdempotent tests ---

// TestCreatePrecommitReviewIdempotent_InsertOnce verifies that the first insert succeeds.
func TestCreatePrecommitReviewIdempotent_InsertOnce(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	rev := &PrecommitReview{
		ID:       "pc-run001-copilot",
		Repo:     "owner/repo",
		Branch:   "feat/x",
		Provider: "copilot",
		Status:   "passed",
	}

	if err := CreatePrecommitReviewIdempotent(context.Background(), db, rev); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Verify record exists.
	var count int
	_ = db.sql.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM precommit_reviews WHERE id = ?`, rev.ID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 record, got %d", count)
	}
}

// TestCreatePrecommitReviewIdempotent_DuplicateSkipped verifies that a second
// insert with the same ID is silently ignored (INSERT OR IGNORE).
func TestCreatePrecommitReviewIdempotent_DuplicateSkipped(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	rev := &PrecommitReview{
		ID:       "pc-run002-litellm",
		Repo:     "owner/repo",
		Branch:   "feat/y",
		Provider: "litellm",
		Status:   "passed",
	}

	if err := CreatePrecommitReviewIdempotent(context.Background(), db, rev); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Second insert with same ID — must not return an error.
	if err := CreatePrecommitReviewIdempotent(context.Background(), db, rev); err != nil {
		t.Errorf("duplicate insert should be silently skipped, got error: %v", err)
	}

	// Row count must remain 1.
	var count int
	_ = db.sql.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM precommit_reviews WHERE id = ?`, rev.ID).Scan(&count)
	if count != 1 {
		t.Errorf("expected exactly 1 record after duplicate insert, got %d", count)
	}
}

// TestCreatePrecommitReviewIdempotent_TwoProvidersSameRun verifies that copilot and
// litellm records for the same run_id are stored as separate rows.
func TestCreatePrecommitReviewIdempotent_TwoProvidersSameRun(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	runID := "20260316-abc999"

	for _, p := range []struct {
		id, provider, status string
	}{
		{"pc-" + runID + "-copilot", "copilot", "passed"},
		{"pc-" + runID + "-litellm", "litellm", "failed"},
	} {
		rev := &PrecommitReview{
			ID:       p.id,
			Repo:     "owner/repo",
			Branch:   "feat/z",
			Provider: p.provider,
			Status:   p.status,
		}
		if err := CreatePrecommitReviewIdempotent(context.Background(), db, rev); err != nil {
			t.Fatalf("insert %s failed: %v", p.provider, err)
		}
	}

	var count int
	_ = db.sql.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM precommit_reviews WHERE id LIKE ?`, "pc-"+runID+"-%").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 records for run %s, got %d", runID, count)
	}
}

// TestCreatePrecommitReviewIdempotent_GateStatusMapping verifies PASS/FAIL/error
// status values accepted from the gate-state-to-precommit-status mapping.
func TestCreatePrecommitReviewIdempotent_GateStatusMapping(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	cases := []struct {
		id     string
		status string
	}{
		{"pc-map-passed", "passed"},
		{"pc-map-failed", "failed"},
		{"pc-map-error", "error"},
	}

	for _, tc := range cases {
		rev := &PrecommitReview{
			ID:       tc.id,
			Repo:     "owner/repo",
			Branch:   "feat/map",
			Provider: "copilot",
			Status:   tc.status,
		}
		if err := CreatePrecommitReviewIdempotent(context.Background(), db, rev); err != nil {
			t.Errorf("insert with status=%q failed: %v", tc.status, err)
		}
	}

	// Verify all three records are present.
	var count int
	_ = db.sql.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM precommit_reviews WHERE repo = 'owner/repo' AND branch = 'feat/map'`).Scan(&count)
	if count != 3 {
		t.Errorf("expected 3 records, got %d", count)
	}
}

// TestCreatePrecommitReviewIdempotent_PollingLoop verifies that calling the
// idempotent function multiple times (simulating a polling loop that retries
// on the same run_id) results in exactly one record.
func TestCreatePrecommitReviewIdempotent_PollingLoop(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	rev := &PrecommitReview{
		ID:       "pc-poll-run-copilot",
		Repo:     "owner/repo",
		Branch:   "feat/poll",
		Provider: "copilot",
		Status:   "passed",
	}

	// Simulate being called 5 times (e.g. polling loop).
	for i := 0; i < 5; i++ {
		if err := CreatePrecommitReviewIdempotent(context.Background(), db, rev); err != nil {
			t.Fatalf("call %d failed: %v", i+1, err)
		}
	}

	var count int
	_ = db.sql.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM precommit_reviews WHERE id = ?`, rev.ID).Scan(&count)
	if count != 1 {
		t.Errorf("expected exactly 1 record after 5 idempotent calls, got %d", count)
	}
}

// TestCreatePrecommitReview_NonIdempotent verifies the original CreatePrecommitReview
// still returns an error on duplicate ID (preserving existing behavior).
func TestCreatePrecommitReview_NonIdempotent(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	rev := &PrecommitReview{
		ID:       "pc-non-idempotent-test",
		Repo:     "owner/repo",
		Branch:   "feat/ni",
		Provider: "litellm",
		Status:   "passed",
	}

	if err := CreatePrecommitReview(context.Background(), db, rev); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Second insert with same ID must fail (UNIQUE constraint).
	if err := CreatePrecommitReview(context.Background(), db, rev); err == nil {
		t.Error("expected error on duplicate insert with CreatePrecommitReview, got nil")
	}
}

// TestCreatePrecommitReview_CountsInScorecard verifies that auto-recorded entries
// are correctly aggregated by ComputeProvingScorecard.
func TestCreatePrecommitReview_CountsInScorecard(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	recentTime := now.AddDate(0, 0, -3).Format("2006-01-02 15:04:05")

	// Insert 3 records within the 7-day window.
	for i, p := range []struct{ id, provider string }{
		{"pc-sc-run1-copilot", "copilot"},
		{"pc-sc-run1-litellm", "litellm"},
		{"pc-sc-run2-copilot", "copilot"},
	} {
		_, err := db.sql.ExecContext(ctx, `
			INSERT INTO precommit_reviews (id, repo, branch, provider, status, created_at)
			VALUES (?, 'owner/repo', 'feat/sc', ?, 'passed', ?)`,
			p.id, p.provider, recentTime)
		if err != nil {
			t.Fatalf("insert %d failed: %v", i, err)
		}
	}

	card, err := ComputeProvingScorecard(ctx, db)
	if err != nil {
		t.Fatalf("ComputeProvingScorecard failed: %v", err)
	}

	if card.PrecommitReviews7Days != 3 {
		t.Errorf("PrecommitReviews7Days: got %d, want 3", card.PrecommitReviews7Days)
	}
	if card.PrecommitReviewsByRepo["owner/repo"] != 3 {
		t.Errorf("PrecommitReviewsByRepo[owner/repo]: got %d, want 3", card.PrecommitReviewsByRepo["owner/repo"])
	}
}

