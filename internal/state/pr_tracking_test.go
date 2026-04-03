package state

import (
	"context"
	"testing"
)

func TestUpsertPRTracking_NewBranch(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	err := UpsertPRTracking(ctx, db, "codero", "feat/test", 42)
	if err != nil {
		t.Fatalf("upsert new: %v", err)
	}

	pr, err := GetPRNumber(ctx, db, "codero", "feat/test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pr != 42 {
		t.Errorf("expected pr_number=42, got %d", pr)
	}
}

func TestUpsertPRTracking_Idempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := UpsertPRTracking(ctx, db, "codero", "feat/test", 42); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := UpsertPRTracking(ctx, db, "codero", "feat/test", 42); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	// Verify exactly one row.
	var count int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM branch_states WHERE repo = ? AND branch = ?`,
		"codero", "feat/test").Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestUpsertPRTracking_Update(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := UpsertPRTracking(ctx, db, "codero", "feat/test", 42); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := UpsertPRTracking(ctx, db, "codero", "feat/test", 99); err != nil {
		t.Fatalf("update upsert: %v", err)
	}

	pr, err := GetPRNumber(ctx, db, "codero", "feat/test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pr != 99 {
		t.Errorf("expected pr_number=99 after update, got %d", pr)
	}
}

func TestUpsertPRTracking_ExistingRow(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Pre-create a branch_states row (as would exist from session heartbeat).
	_, err := db.sql.Exec(`INSERT INTO branch_states (id, repo, branch, state) VALUES ('existing-id', 'codero', 'feat/existing', 'active')`)
	if err != nil {
		t.Fatalf("pre-insert: %v", err)
	}

	if err := UpsertPRTracking(ctx, db, "codero", "feat/existing", 55); err != nil {
		t.Fatalf("upsert on existing: %v", err)
	}

	pr, err := GetPRNumber(ctx, db, "codero", "feat/existing")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if pr != 55 {
		t.Errorf("expected pr_number=55, got %d", pr)
	}
}

func TestUpsertPRTracking_ValidationErrors(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		repo   string
		branch string
		pr     int
	}{
		{"empty repo", "", "main", 1},
		{"empty branch", "codero", "", 1},
		{"zero PR", "codero", "main", 0},
		{"negative PR", "codero", "main", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UpsertPRTracking(ctx, db, tt.repo, tt.branch, tt.pr); err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestGetPRNumber_NotTracked(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	pr, err := GetPRNumber(ctx, db, "nonexistent", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr != 0 {
		t.Errorf("expected 0 for untracked branch, got %d", pr)
	}
}
