package state

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3" // driver for raw sql.Open in test helpers
)

// openTestDB opens a file-based test database in t.TempDir().
// File-based is required to test WAL mode (WAL is not supported on :memory:).
func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpen_WALMode(t *testing.T) {
	db := openTestDB(t)

	var mode string
	row := db.Unwrap().QueryRow("PRAGMA journal_mode")
	if err := row.Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode: got %q, want %q", mode, "wal")
	}
}

func TestOpen_BusyTimeout(t *testing.T) {
	db := openTestDB(t)

	var timeout int
	row := db.Unwrap().QueryRow("PRAGMA busy_timeout")
	if err := row.Scan(&timeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if timeout != 5000 {
		t.Errorf("busy_timeout: got %d, want 5000", timeout)
	}
}

func TestOpen_ForeignKeys(t *testing.T) {
	db := openTestDB(t)

	var fk int
	row := db.Unwrap().QueryRow("PRAGMA foreign_keys")
	if err := row.Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys: got %d, want 1", fk)
	}
}

func TestOpen_TablesCreated(t *testing.T) {
	db := openTestDB(t)

	for _, table := range []string{
		"branch_states",
		"state_transitions",
		"agent_sessions",
		"agent_assignments",
		"agent_events",
		"agent_rules",
		"assignment_rule_checks",
		"codero_github_links",
		"task_feedback_cache",
		"schema_migrations",
	} {
		var name string
		err := db.Unwrap().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found after migration: %v", table, err)
		}
	}
}

func TestOpen_BranchStatesSchema(t *testing.T) {
	db := openTestDB(t)

	// Insert a minimal row to verify all NOT NULL columns have defaults.
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, state)
		VALUES ('test-uuid-1', 'acme/api', 'main', 'coding')
	`)
	if err != nil {
		t.Fatalf("insert branch_state: %v", err)
	}

	var (
		retryCount, maxRetries, approved, ciGreen, pendingEvents, unresolvedThreads int
		headHash, ownerSessionID, state                                             string
	)
	err = db.Unwrap().QueryRow(`
		SELECT retry_count, max_retries, approved, ci_green,
		       pending_events, unresolved_threads,
		       head_hash, owner_session_id, state
		FROM branch_states WHERE id='test-uuid-1'
	`).Scan(&retryCount, &maxRetries, &approved, &ciGreen,
		&pendingEvents, &unresolvedThreads,
		&headHash, &ownerSessionID, &state)
	if err != nil {
		t.Fatalf("select branch_state: %v", err)
	}

	checks := []struct {
		field string
		got   int
		want  int
	}{
		{"retry_count", retryCount, 0},
		{"max_retries", maxRetries, 3},
		{"approved", approved, 0},
		{"ci_green", ciGreen, 0},
		{"pending_events", pendingEvents, 0},
		{"unresolved_threads", unresolvedThreads, 0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s default: got %d, want %d", c.field, c.got, c.want)
		}
	}
	if headHash != "" {
		t.Errorf("head_hash default: got %q, want %q", headHash, "")
	}
	if ownerSessionID != "" {
		t.Errorf("owner_session_id default: got %q, want %q", ownerSessionID, "")
	}
	if state != "coding" {
		t.Errorf("state: got %q, want %q", state, "coding")
	}
}

func TestOpen_StateTransitionsSchema(t *testing.T) {
	db := openTestDB(t)

	// Insert branch first (FK constraint).
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, state)
		VALUES ('test-uuid-2', 'acme/api', 'feat/x', 'coding')
	`)
	if err != nil {
		t.Fatalf("insert branch_state: %v", err)
	}

	_, err = db.Unwrap().Exec(`
		INSERT INTO state_transitions (branch_state_id, from_state, to_state, trigger)
		VALUES ('test-uuid-2', 'coding', 'queued_cli', 'codero-cli submit')
	`)
	if err != nil {
		t.Fatalf("insert state_transition: %v", err)
	}

	var fromState, toState, trigger string
	err = db.Unwrap().QueryRow(`
		SELECT from_state, to_state, trigger FROM state_transitions
		WHERE branch_state_id='test-uuid-2'
	`).Scan(&fromState, &toState, &trigger)
	if err != nil {
		t.Fatalf("select state_transition: %v", err)
	}
	if fromState != "coding" || toState != "queued_cli" || trigger != "codero-cli submit" {
		t.Errorf("transition row: got (%q, %q, %q), want (coding, queued_cli, codero-cli submit)",
			fromState, toState, trigger)
	}
}

func TestOpen_ForeignKeyEnforced(t *testing.T) {
	db := openTestDB(t)

	// Inserting a transition for a non-existent branch_state_id must fail.
	_, err := db.Unwrap().Exec(`
		INSERT INTO state_transitions (branch_state_id, from_state, to_state, trigger)
		VALUES ('nonexistent-id', 'coding', 'queued_cli', 'test')
	`)
	if err == nil {
		t.Error("expected FK violation error, got nil")
	}
}

func TestOpen_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idempotent.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	_ = db1.Close()

	// Second open on the same file must succeed (ErrNoChange from migrate).
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open (idempotent): %v", err)
	}
	_ = db2.Close()
}

func TestOpen_AgentAssignmentsTaskLayerDefaults(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Unwrap().Exec(`
		INSERT INTO agent_sessions (session_id, agent_id)
		VALUES ('session-1', 'codex-a')
	`); err != nil {
		t.Fatalf("insert agent_session: %v", err)
	}

	if _, err := db.Unwrap().Exec(`
		INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch)
		VALUES ('assignment-1', 'session-1', 'codex-a', 'acme/api', 'feat/task-layer')
	`); err != nil {
		t.Fatalf("insert agent_assignment: %v", err)
	}

	var (
		assignmentVersion                          int
		parentAssignmentID, successorSessionID     sql.NullString
		description, suggestedSubstatus            sql.NullString
		actualSubstatus                            sql.NullString
		lastEmitAt, blockedSince                   sql.NullTime
		firstFeedbackAt, lastFeedbackAt            sql.NullTime
		feedbackPollCount, substatusDeviationCount int
	)
	err := db.Unwrap().QueryRow(`
		SELECT assignment_version, parent_assignment_id, successor_session_id,
		       description, last_emit_at, blocked_since, first_feedback_at, last_feedback_at,
		       feedback_poll_count, suggested_substatus_last, actual_substatus_last,
		       substatus_deviation_count
		FROM agent_assignments
		WHERE assignment_id = 'assignment-1'
	`).Scan(
		&assignmentVersion, &parentAssignmentID, &successorSessionID,
		&description, &lastEmitAt, &blockedSince, &firstFeedbackAt, &lastFeedbackAt,
		&feedbackPollCount, &suggestedSubstatus, &actualSubstatus,
		&substatusDeviationCount,
	)
	if err != nil {
		t.Fatalf("select task-layer agent_assignment defaults: %v", err)
	}

	if assignmentVersion != 1 {
		t.Errorf("assignment_version default: got %d, want 1", assignmentVersion)
	}
	if parentAssignmentID.Valid {
		t.Errorf("parent_assignment_id default: got %q, want NULL", parentAssignmentID.String)
	}
	if successorSessionID.Valid {
		t.Errorf("successor_session_id default: got %q, want NULL", successorSessionID.String)
	}
	if description.Valid {
		t.Errorf("description default: got %q, want NULL", description.String)
	}
	if lastEmitAt.Valid {
		t.Errorf("last_emit_at default: got %v, want NULL", lastEmitAt.Time)
	}
	if blockedSince.Valid {
		t.Errorf("blocked_since default: got %v, want NULL", blockedSince.Time)
	}
	if firstFeedbackAt.Valid {
		t.Errorf("first_feedback_at default: got %v, want NULL", firstFeedbackAt.Time)
	}
	if lastFeedbackAt.Valid {
		t.Errorf("last_feedback_at default: got %v, want NULL", lastFeedbackAt.Time)
	}
	if feedbackPollCount != 0 {
		t.Errorf("feedback_poll_count default: got %d, want 0", feedbackPollCount)
	}
	if suggestedSubstatus.Valid {
		t.Errorf("suggested_substatus_last default: got %q, want NULL", suggestedSubstatus.String)
	}
	if actualSubstatus.Valid {
		t.Errorf("actual_substatus_last default: got %q, want NULL", actualSubstatus.String)
	}
	if substatusDeviationCount != 0 {
		t.Errorf("substatus_deviation_count default: got %d, want 0", substatusDeviationCount)
	}
}

func TestOpen_TaskLayerTablesConstraints(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Unwrap().Exec(`
		INSERT INTO agent_sessions (session_id, agent_id)
		VALUES ('session-1', 'codex-a')
	`); err != nil {
		t.Fatalf("insert agent_session: %v", err)
	}

	if _, err := db.Unwrap().Exec(`
		INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch, task_id)
		VALUES ('assignment-1', 'session-1', 'codex-a', 'acme/api', 'feat/task-layer', 'TASK-1')
	`); err != nil {
		t.Fatalf("insert agent_assignment: %v", err)
	}

	if _, err := db.Unwrap().Exec(`
		INSERT INTO codero_github_links (link_id, task_id, repo_full_name, pr_number, pr_state)
		VALUES ('link-1', 'TASK-1', 'acme/api', 42, 'open')
	`); err != nil {
		t.Fatalf("insert codero_github_links: %v", err)
	}

	if _, err := db.Unwrap().Exec(`
		INSERT INTO task_feedback_cache (cache_id, assignment_id, session_id, task_id, cache_hash)
		VALUES ('cache-1', 'assignment-1', 'session-1', 'TASK-1', 'hash-1')
	`); err != nil {
		t.Fatalf("insert task_feedback_cache: %v", err)
	}

	if _, err := db.Unwrap().Exec(`
		INSERT INTO codero_github_links (link_id, task_id, repo_full_name)
		VALUES ('link-2', 'TASK-1', 'acme/api')
	`); err == nil {
		t.Fatal("expected unique task_id violation for codero_github_links, got nil")
	}

	if _, err := db.Unwrap().Exec(`
		INSERT INTO task_feedback_cache (cache_id, assignment_id, session_id, task_id, cache_hash)
		VALUES ('cache-2', 'assignment-1', 'session-1', 'TASK-1', 'hash-2')
	`); err == nil {
		t.Fatal("expected unique assignment_id violation for task_feedback_cache, got nil")
	}

	if _, err := db.Unwrap().Exec(`
		INSERT INTO codero_github_links (link_id, task_id, repo_full_name, pr_state)
		VALUES ('link-3', 'TASK-2', 'acme/api', 'invalid')
	`); err == nil {
		t.Fatal("expected pr_state check constraint violation, got nil")
	}
}

func TestOpen_UniqueConstraint(t *testing.T) {
	db := openTestDB(t)

	insert := func() error {
		_, err := db.Unwrap().Exec(`
			INSERT INTO branch_states (id, repo, branch, state)
			VALUES ('id-' || hex(randomblob(8)), 'acme/api', 'main', 'coding')
		`)
		return err
	}

	if err := insert(); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := insert(); err == nil {
		t.Error("duplicate (repo, branch) insert should fail, got nil")
	}
}

func TestOpen_ErrMigrationWrapping(t *testing.T) {
	// Force a migration failure by pre-creating a schema_migrations table
	// with a wrong schema so golang-migrate cannot use it.
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.db")

	rawDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	// Create a schema_migrations table with a wrong column type to confuse the
	// migration driver. The driver expects (version INT, dirty BOOL).
	_, err = rawDB.Exec(`CREATE TABLE schema_migrations (wrong_col TEXT)`)
	if err != nil {
		t.Fatalf("create bad table: %v", err)
	}
	_ = rawDB.Close()

	_, err = Open(path)
	if err == nil {
		t.Fatal("expected error on bad schema_migrations table, got nil")
	}
	if !errors.Is(err, ErrMigration) {
		t.Errorf("want errors.Is(err, ErrMigration); got: %v", err)
	}
}

func TestOpen_CreatesParentDirectory(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "sub", "dir", "codero.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open with nested path: %v", err)
	}
	_ = db.Close()
}
