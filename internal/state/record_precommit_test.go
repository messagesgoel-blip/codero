package state

import (
	"context"
	"testing"
	"time"
)

func TestRecordPrecommitResult_Pass(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	err := RecordPrecommitResult(context.Background(), db,
		"codero", "main", "abc123", "pass", 4200, "gitleaks,govet,ai-gate")
	if err != nil {
		t.Fatalf("RecordPrecommitResult pass: %v", err)
	}

	// Verify precommit_reviews row.
	var pcStatus, pcProvider, pcError string
	if err := db.sql.QueryRow(
		`SELECT status, provider, error FROM precommit_reviews WHERE repo='codero' AND branch='main'`,
	).Scan(&pcStatus, &pcProvider, &pcError); err != nil {
		t.Fatalf("query precommit_reviews: %v", err)
	}
	if pcStatus != "passed" {
		t.Errorf("precommit_reviews.status: want passed, got %q", pcStatus)
	}
	if pcProvider != "precommit" {
		t.Errorf("precommit_reviews.provider: want precommit, got %q", pcProvider)
	}
	if pcError != "" {
		t.Errorf("precommit_reviews.error: want empty on pass, got %q", pcError)
	}

	// Verify review_runs row.
	var rrStatus, rrProvider, rrHeadHash, rrError string
	var startedAt, finishedAt time.Time
	if err := db.sql.QueryRow(
		`SELECT status, provider, head_hash, error, started_at, finished_at FROM review_runs WHERE repo='codero' AND branch='main'`,
	).Scan(&rrStatus, &rrProvider, &rrHeadHash, &rrError, &startedAt, &finishedAt); err != nil {
		t.Fatalf("query review_runs: %v", err)
	}
	if rrStatus != "completed" {
		t.Errorf("review_runs.status: want completed, got %q", rrStatus)
	}
	if rrProvider != "precommit" {
		t.Errorf("review_runs.provider: want precommit, got %q", rrProvider)
	}
	if rrHeadHash != "abc123" {
		t.Errorf("review_runs.head_hash: want abc123, got %q", rrHeadHash)
	}
	if rrError != "" {
		t.Errorf("review_runs.error: want empty on pass, got %q", rrError)
	}
	if !startedAt.Before(finishedAt) {
		t.Errorf("review_runs: startedAt (%v) should be before finishedAt (%v) when durationMS>0", startedAt, finishedAt)
	}
}

func TestRecordPrecommitResult_Fail(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	err := RecordPrecommitResult(context.Background(), db,
		"codero", "feat/x", "", "fail", 0, "gitleaks,ruff")
	if err != nil {
		t.Fatalf("RecordPrecommitResult fail: %v", err)
	}

	var pcStatus, pcError string
	if err := db.sql.QueryRow(
		`SELECT status, error FROM precommit_reviews WHERE repo='codero' AND branch='feat/x'`,
	).Scan(&pcStatus, &pcError); err != nil {
		t.Fatalf("query precommit_reviews: %v", err)
	}
	if pcStatus != "failed" {
		t.Errorf("precommit_reviews.status: want failed, got %q", pcStatus)
	}
	if pcError != "checks: gitleaks,ruff" {
		t.Errorf("precommit_reviews.error: want 'checks: gitleaks,ruff', got %q", pcError)
	}

	var rrStatus string
	if err := db.sql.QueryRow(
		`SELECT status FROM review_runs WHERE repo='codero' AND branch='feat/x'`,
	).Scan(&rrStatus); err != nil {
		t.Fatalf("query review_runs: %v", err)
	}
	if rrStatus != "failed" {
		t.Errorf("review_runs.status: want failed, got %q", rrStatus)
	}
}

func TestRecordPrecommitResult_FailNoChecks(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	err := RecordPrecommitResult(context.Background(), db,
		"codero", "feat/y", "", "fail", 0, "")
	if err != nil {
		t.Fatalf("RecordPrecommitResult fail-no-checks: %v", err)
	}

	var pcError string
	if err := db.sql.QueryRow(
		`SELECT error FROM precommit_reviews WHERE repo='codero' AND branch='feat/y'`,
	).Scan(&pcError); err != nil {
		t.Fatalf("query precommit_reviews: %v", err)
	}
	if pcError != "gate failed" {
		t.Errorf("precommit_reviews.error: want 'gate failed', got %q", pcError)
	}
}

func TestRecordPrecommitResult_InvalidResult(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	err := RecordPrecommitResult(context.Background(), db,
		"codero", "main", "", "unknown", 0, "")
	if err == nil {
		t.Fatal("expected error for invalid result, got nil")
	}
	// No rows should have been written.
	var pcCount, rrCount int
	db.sql.QueryRow(`SELECT COUNT(*) FROM precommit_reviews WHERE repo='codero' AND branch='main'`).Scan(&pcCount)
	db.sql.QueryRow(`SELECT COUNT(*) FROM review_runs WHERE repo='codero' AND branch='main'`).Scan(&rrCount)
	if pcCount != 0 {
		t.Errorf("expected no precommit_reviews rows on invalid result, got %d", pcCount)
	}
	if rrCount != 0 {
		t.Errorf("expected no review_runs rows on invalid result, got %d", rrCount)
	}
}

func TestRecordPrecommitResult_NegativeDuration(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	err := RecordPrecommitResult(context.Background(), db,
		"codero", "main", "", "pass", -1, "")
	if err == nil {
		t.Fatal("expected error for negative durationMS, got nil")
	}
}

func TestRecordPrecommitResult_ZeroDuration(t *testing.T) {
	db, cleanup := setupTestProvingDB(t)
	defer cleanup()

	err := RecordPrecommitResult(context.Background(), db,
		"codero", "main", "", "pass", 0, "")
	if err != nil {
		t.Fatalf("RecordPrecommitResult zero-duration: %v", err)
	}

	var startedAt, finishedAt time.Time
	if err := db.sql.QueryRow(
		`SELECT started_at, finished_at FROM review_runs WHERE repo='codero' AND branch='main'`,
	).Scan(&startedAt, &finishedAt); err != nil {
		t.Fatalf("query review_runs: %v", err)
	}
	// When durationMS==0, started_at and finished_at are the same.
	diff := finishedAt.Sub(startedAt)
	if diff < 0 {
		t.Errorf("started_at should not be after finished_at, diff=%v", diff)
	}
}
