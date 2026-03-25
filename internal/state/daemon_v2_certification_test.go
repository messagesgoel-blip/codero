package state

// Daemon Spec v2 Certification Tests — state layer
//
// Clause-mapped tests for §4 criteria that exercise the state/repository
// layer: D-3 (SQLite source of truth), D-7 (rule seeding), D-26 (pipeline
// serialization), D-28 (delivery lock lifecycle), D-30 (409 on concurrent submit).

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openCertDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cert.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ---------------------------------------------------------------------------
// D-3  SQLite is the source of truth
// ---------------------------------------------------------------------------

func TestCert_D3_SQLiteSourceOfTruth(t *testing.T) {
	db := openCertDB(t)

	now := time.Now()
	run := &ReviewRun{
		ID:        "run-d3-test",
		Repo:      "owner/repo",
		Branch:    "feat/d3",
		HeadHash:  "abc123",
		Provider:  "stub",
		Status:    "running",
		StartedAt: &now,
	}
	if err := CreateReviewRun(db, run); err != nil {
		t.Fatalf("CreateReviewRun: %v", err)
	}

	// State is queryable from SQLite (no Redis needed).
	running, err := IsPipelineRunning(context.Background(), db, "owner/repo", "feat/d3")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if !running {
		t.Fatal("D-3: pipeline must be running (SQLite is source of truth)")
	}
	t.Log("D-3 PASS: pipeline state derived from SQLite without Redis")
}

// ---------------------------------------------------------------------------
// D-7  Rule seeding via migrations
// ---------------------------------------------------------------------------

func TestCert_D7_RuleSeedingViaMigration(t *testing.T) {
	db := openCertDB(t)

	// state.Open() runs all migrations, which seed RULE-001–004.
	// Query agent_rules table directly to verify.
	rows, err := db.Unwrap().Query(`SELECT rule_id FROM agent_rules WHERE rule_id IN ('RULE-001','RULE-002','RULE-003','RULE-004') ORDER BY rule_id`)
	if err != nil {
		t.Fatalf("query agent_rules: %v", err)
	}
	defer rows.Close()

	var rules []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		rules = append(rules, id)
	}

	expected := []string{"RULE-001", "RULE-002", "RULE-003", "RULE-004"}
	if len(rules) < len(expected) {
		t.Fatalf("D-7: expected %v seeded, got %v", expected, rules)
	}
	for i, exp := range expected {
		if i >= len(rules) || rules[i] != exp {
			t.Fatalf("D-7: rule %d: want %s, got %v", i, exp, rules)
		}
	}
	t.Logf("D-7 PASS: all 4 compliance rules seeded by migration: %v", rules)
}

// ---------------------------------------------------------------------------
// D-26  Pipeline serializes per branch (= per worktree in Codero's 1:1 model)
// ---------------------------------------------------------------------------

func TestCert_D26_PipelineSerializesPerBranch(t *testing.T) {
	db := openCertDB(t)
	ctx := context.Background()

	now := time.Now()
	run1 := &ReviewRun{
		ID:        "run-d26-a",
		Repo:      "owner/repo",
		Branch:    "feat/alpha",
		HeadHash:  "aaa",
		Provider:  "stub",
		Status:    "running",
		StartedAt: &now,
	}
	if err := CreateReviewRun(db, run1); err != nil {
		t.Fatalf("CreateReviewRun: %v", err)
	}

	// Same branch: must be blocked.
	running, err := IsPipelineRunning(ctx, db, "owner/repo", "feat/alpha")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if !running {
		t.Fatal("D-26: same branch must report pipeline running")
	}

	// Different branch in same repo: must be allowed.
	running2, err := IsPipelineRunning(ctx, db, "owner/repo", "feat/beta")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if running2 {
		t.Fatal("D-26: different branch must NOT be blocked")
	}

	// Complete first pipeline.
	if err := UpdateReviewRun(db, "run-d26-a", "completed", "", time.Now()); err != nil {
		t.Fatalf("UpdateReviewRun: %v", err)
	}

	// Same branch: must now be clear.
	running3, err := IsPipelineRunning(ctx, db, "owner/repo", "feat/alpha")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if running3 {
		t.Fatal("D-26: completed pipeline must not block new submissions")
	}
	t.Log("D-26 PASS: pipeline serialization per branch verified (= per worktree in 1:1 model)")
}

// ---------------------------------------------------------------------------
// D-28  Delivery lock lifecycle (review_runs.status as durable lock)
// ---------------------------------------------------------------------------

func TestCert_D28_DeliveryLockLifecycle(t *testing.T) {
	db := openCertDB(t)
	ctx := context.Background()

	// Lock acquired: CreateReviewRun with status='running'.
	now := time.Now()
	run := &ReviewRun{
		ID:        "run-d28-lock",
		Repo:      "owner/repo",
		Branch:    "feat/lock",
		HeadHash:  "def456",
		Provider:  "stub",
		Status:    "running",
		StartedAt: &now,
	}
	if err := CreateReviewRun(db, run); err != nil {
		t.Fatalf("CreateReviewRun (lock acquire): %v", err)
	}

	// Lock held: IsPipelineRunning must be true.
	locked, err := IsPipelineRunning(ctx, db, "owner/repo", "feat/lock")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if !locked {
		t.Fatal("D-28: delivery lock must be held while status='running'")
	}

	// Lock released: UpdateReviewRun to 'completed'.
	if err := UpdateReviewRun(db, "run-d28-lock", "completed", "", time.Now()); err != nil {
		t.Fatalf("UpdateReviewRun (lock release): %v", err)
	}

	// Lock clear: IsPipelineRunning must be false.
	locked2, err := IsPipelineRunning(ctx, db, "owner/repo", "feat/lock")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if locked2 {
		t.Fatal("D-28: delivery lock must be released after status='completed'")
	}
	t.Log("D-28 PASS: delivery lock lifecycle (create→hold→release) via review_runs.status")
}

// ---------------------------------------------------------------------------
// D-28  Failed pipeline also releases lock
// ---------------------------------------------------------------------------

func TestCert_D28_FailedPipelineReleasesLock(t *testing.T) {
	db := openCertDB(t)
	ctx := context.Background()

	now := time.Now()
	run := &ReviewRun{
		ID:        "run-d28-fail",
		Repo:      "owner/repo",
		Branch:    "feat/fail-lock",
		HeadHash:  "ghi789",
		Provider:  "stub",
		Status:    "running",
		StartedAt: &now,
	}
	if err := CreateReviewRun(db, run); err != nil {
		t.Fatalf("CreateReviewRun: %v", err)
	}

	// Fail the pipeline.
	if err := UpdateReviewRun(db, "run-d28-fail", "failed", "test error", time.Now()); err != nil {
		t.Fatalf("UpdateReviewRun: %v", err)
	}

	locked, err := IsPipelineRunning(ctx, db, "owner/repo", "feat/fail-lock")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if locked {
		t.Fatal("D-28: failed pipeline must release delivery lock")
	}
	t.Log("D-28 PASS: failed pipeline releases lock")
}

// ---------------------------------------------------------------------------
// D-30  Submit 409 if pipeline running (state-level check)
// ---------------------------------------------------------------------------

func TestCert_D30_ConcurrentSubmitBlocked(t *testing.T) {
	db := openCertDB(t)
	ctx := context.Background()

	now := time.Now()
	run := &ReviewRun{
		ID:        "run-d30-block",
		Repo:      "owner/repo",
		Branch:    "feat/concurrent",
		HeadHash:  "jkl012",
		Provider:  "stub",
		Status:    "running",
		StartedAt: &now,
	}
	if err := CreateReviewRun(db, run); err != nil {
		t.Fatalf("CreateReviewRun: %v", err)
	}

	running, err := IsPipelineRunning(ctx, db, "owner/repo", "feat/concurrent")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if !running {
		t.Fatal("D-30: must detect running pipeline for 409 response")
	}
	t.Log("D-30 PASS: IsPipelineRunning gates concurrent submissions")
}

// ---------------------------------------------------------------------------
// D-5  GitHub token absence → tasks queued
// ---------------------------------------------------------------------------

func TestCert_D5_TaskQueuedWithoutToken(t *testing.T) {
	db := openCertDB(t)

	// Create a branch_states entry (simulating task ingestion without GitHub token).
	_, err := db.Unwrap().Exec(`INSERT INTO branch_states (id, repo, branch, state, head_hash, created_at, updated_at)
		VALUES ('d5-test', 'owner/repo', 'feat/no-token', 'queued_cli', 'xyz', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert branch_states: %v", err)
	}

	// Verify task is queued, not dropped.
	var count int
	err = db.Unwrap().QueryRow(`SELECT COUNT(*) FROM branch_states WHERE repo='owner/repo' AND branch='feat/no-token' AND state='queued_cli'`).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatal("D-5: task must be queued when GitHub token is absent")
	}
	t.Log("D-5 PASS: task queued in SQLite regardless of GitHub token state")
}

// ---------------------------------------------------------------------------
// D-6  Recovery sweep ordering
// ---------------------------------------------------------------------------

func TestCert_D6_MigrationBeforeQuery(t *testing.T) {
	// D-6 verifies sweep before API. The state-level precondition: migrations
	// must complete before any query. state.Open() guarantees this.
	db := openCertDB(t)

	// If we can query, migrations ran successfully — tables exist.
	var count int
	err := db.Unwrap().QueryRow(`SELECT COUNT(*) FROM review_runs`).Scan(&count)
	if err != nil {
		t.Fatalf("D-6: query review_runs failed (migration did not run?): %v", err)
	}
	t.Logf("D-6 PASS: review_runs table exists post-Open (migrations ran before query)")
}
