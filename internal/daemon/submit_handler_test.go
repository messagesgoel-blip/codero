package daemon

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	deliverypipeline "github.com/codero/codero/internal/delivery_pipeline"
	"github.com/codero/codero/internal/state"
)

// setupSubmitTestDB opens an in-memory SQLite DB with all migrations applied
// and returns the raw *sql.DB for use in handler tests.
func setupSubmitTestDB(t *testing.T) *state.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedAssignment inserts a session and assignment into the test DB.
func seedAssignment(t *testing.T, obs *ObservabilityServer, sessionID, assignmentID, worktree string) {
	t.Helper()
	_, err := obs.db.Exec(
		`INSERT INTO agent_sessions (session_id, agent_id, mode) VALUES (?, 'agent-1', 'coding')`,
		sessionID,
	)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	_, err = obs.db.Exec(
		`INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch, worktree) VALUES (?, ?, 'agent-1', 'acme/api', 'main', ?)`,
		assignmentID, sessionID, worktree,
	)
	if err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
}

func TestHandleSubmit_202(t *testing.T) {
	db := setupSubmitTestDB(t)
	obs := &ObservabilityServer{db: db.Unwrap()}

	seedAssignment(t, obs, "test-sess", "assign-1", "")

	body, _ := json.Marshal(submitRequest{SessionID: "test-sess"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assignments/assign-1/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	obs.handleSubmit(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "submitted" {
		t.Errorf("expected status=submitted, got %q", resp["status"])
	}
	if resp["assignment_id"] != "assign-1" {
		t.Errorf("expected assignment_id=assign-1, got %q", resp["assignment_id"])
	}
}

func TestHandleSubmit_404(t *testing.T) {
	db := setupSubmitTestDB(t)
	obs := &ObservabilityServer{db: db.Unwrap()}

	body, _ := json.Marshal(submitRequest{SessionID: "no-such-sess"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assignments/nonexistent/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	obs.handleSubmit(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSubmit_403(t *testing.T) {
	db := setupSubmitTestDB(t)
	obs := &ObservabilityServer{db: db.Unwrap()}

	seedAssignment(t, obs, "owner-sess", "assign-2", "")

	body, _ := json.Marshal(submitRequest{SessionID: "intruder-sess"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assignments/assign-2/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	obs.handleSubmit(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSubmit_409(t *testing.T) {
	db := setupSubmitTestDB(t)
	obs := &ObservabilityServer{db: db.Unwrap()}

	worktreeDir := t.TempDir()
	seedAssignment(t, obs, "test-sess", "assign-3", worktreeDir)

	// Pre-acquire the delivery lock.
	if err := deliverypipeline.Lock(worktreeDir, "test-sess", "assign-3"); err != nil {
		t.Fatalf("pre-lock: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(worktreeDir, ".codero"))
	})

	body, _ := json.Marshal(submitRequest{SessionID: "test-sess"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assignments/assign-3/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	obs.handleSubmit(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSubmit_DeliveryStateUpdated(t *testing.T) {
	db := setupSubmitTestDB(t)
	obs := &ObservabilityServer{db: db.Unwrap()}

	seedAssignment(t, obs, "test-sess", "assign-4", "")

	body, _ := json.Marshal(submitRequest{SessionID: "test-sess"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assignments/assign-4/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	obs.handleSubmit(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify DB state was updated.
	var deliveryState string
	var revisionCount int
	err := obs.db.QueryRow(
		`SELECT delivery_state, revision_count FROM agent_assignments WHERE assignment_id = ?`,
		"assign-4",
	).Scan(&deliveryState, &revisionCount)
	if err != nil {
		t.Fatalf("query after submit: %v", err)
	}
	if deliveryState != "staging" {
		t.Errorf("expected delivery_state=staging, got %q", deliveryState)
	}
	if revisionCount != 1 {
		t.Errorf("expected revision_count=1, got %d", revisionCount)
	}
}

func TestHandleSubmit_LazyAssignment(t *testing.T) {
	db := setupSubmitTestDB(t)
	obs := &ObservabilityServer{db: db.Unwrap()}

	// Seed session without assignment.
	_, err := obs.db.Exec(
		`INSERT INTO agent_sessions (session_id, agent_id, mode) VALUES (?, 'agent-1', 'coding')`,
		"lazy-sess",
	)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	prev := inferRepoBranch
	inferRepoBranch = func(worktree string) (string, string, error) {
		if worktree == "" {
			t.Fatal("expected worktree in lazy assignment")
		}
		return "acme/lazy", "feat/lazy", nil
	}
	t.Cleanup(func() { inferRepoBranch = prev })

	body, _ := json.Marshal(submitRequest{SessionID: "lazy-sess", Worktree: t.TempDir()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assignments/lazy-assign/submit", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	obs.handleSubmit(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var repo, branch, worktree string
	var deliveryState string
	var revisionCount int
	var lastSubmitAt sql.NullTime
	err = obs.db.QueryRow(
		`SELECT repo, branch, worktree, delivery_state, revision_count, last_submit_at
		 FROM agent_assignments WHERE assignment_id = ?`,
		"lazy-assign",
	).Scan(&repo, &branch, &worktree, &deliveryState, &revisionCount, &lastSubmitAt)
	if err != nil {
		t.Fatalf("query lazy assignment: %v", err)
	}
	if repo != "acme/lazy" || branch != "feat/lazy" {
		t.Errorf("repo/branch = %q/%q, want acme/lazy/feat/lazy", repo, branch)
	}
	if deliveryState != "staging" {
		t.Errorf("delivery_state = %q, want staging", deliveryState)
	}
	if revisionCount != 1 {
		t.Errorf("revision_count = %d, want 1", revisionCount)
	}
	if !lastSubmitAt.Valid {
		t.Error("last_submit_at should be set")
	}
	if worktree == "" {
		t.Error("worktree should be stored on lazy assignment")
	}
}
