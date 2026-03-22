package main

// task_test.go — tests for the "task accept" cobra subcommand.
//
// The tests exercise taskAcceptCmd end-to-end by:
//  1. Opening a real (temp-dir) state DB.
//  2. Pointing at it via CODERO_DB_PATH (env-based config fallback).
//  3. Executing the cobra command with args, capturing stdout/stderr.
//  4. Asserting exit behaviour and output.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codero/codero/internal/state"
)

// openTaskTestDB opens a temp-dir state DB, closes it, and returns its path.
func openTaskTestDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "codero.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	_ = db.Close()
	return dbPath
}

// runTaskAccept invokes the taskAcceptCmd cobra command in-process and returns
// (stdout output, error).  CODERO_DB_PATH must be set by the caller.
func runTaskAccept(t *testing.T, flags ...string) (string, error) {
	t.Helper()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	cfgPath := "codero.yaml" // non-existent → falls back to LoadEnv → CODERO_DB_PATH
	cmd := taskAcceptCmd(&cfgPath)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(flags)

	execErr := cmd.ExecuteContext(context.Background())

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	return buf.String(), execErr
}

// TestTaskAcceptCmd_HappyPath verifies the command succeeds and prints expected fields.
func TestTaskAcceptCmd_HappyPath(t *testing.T) {
	dbPath := openTaskTestDB(t)
	t.Setenv("CODERO_DB_PATH", dbPath)

	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	if err := state.RegisterAgentSession(context.Background(), db, "cli-sess-1", "agent-a", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	_ = db.Close()

	out, err := runTaskAccept(t, "--session", "cli-sess-1", "--task", "CLI-TASK-001")
	if err != nil {
		t.Fatalf("expected success, got: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"assignment_id:", "session_id: cli-sess-1",
		"task_id: CLI-TASK-001", "state: active", "substatus: in_progress",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// TestTaskAcceptCmd_Idempotent verifies repeated calls return the same assignment_id.
func TestTaskAcceptCmd_Idempotent(t *testing.T) {
	dbPath := openTaskTestDB(t)
	t.Setenv("CODERO_DB_PATH", dbPath)

	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	if err := state.RegisterAgentSession(context.Background(), db, "cli-sess-idem", "agent-a", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	_ = db.Close()

	out1, err := runTaskAccept(t, "--session", "cli-sess-idem", "--task", "CLI-TASK-IDEM")
	if err != nil {
		t.Fatalf("first accept: %v", err)
	}
	out2, err := runTaskAccept(t, "--session", "cli-sess-idem", "--task", "CLI-TASK-IDEM")
	if err != nil {
		t.Fatalf("second accept (idempotent): %v", err)
	}

	id1 := extractField(out1, "assignment_id:")
	id2 := extractField(out2, "assignment_id:")
	if id1 == "" || id1 != id2 {
		t.Errorf("idempotent: assignment_id changed: %q -> %q", id1, id2)
	}
}

// TestTaskAcceptCmd_Conflict verifies that a rival session gets an error.
func TestTaskAcceptCmd_Conflict(t *testing.T) {
	dbPath := openTaskTestDB(t)
	t.Setenv("CODERO_DB_PATH", dbPath)

	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	ctx := context.Background()
	if err := state.RegisterAgentSession(ctx, db, "cli-owner", "agent-a", ""); err != nil {
		t.Fatalf("RegisterAgentSession owner: %v", err)
	}
	if err := state.RegisterAgentSession(ctx, db, "cli-rival", "agent-b", ""); err != nil {
		t.Fatalf("RegisterAgentSession rival: %v", err)
	}
	_ = db.Close()

	if _, err := runTaskAccept(t, "--session", "cli-owner", "--task", "CLI-TASK-CONFLICT"); err != nil {
		t.Fatalf("owner accept: %v", err)
	}

	_, err = runTaskAccept(t, "--session", "cli-rival", "--task", "CLI-TASK-CONFLICT")
	if err == nil {
		t.Fatal("expected conflict error from rival session, got nil")
	}
	if !strings.Contains(err.Error(), "already claimed") {
		t.Errorf("error should mention 'already claimed'; got: %v", err)
	}
}

func TestTaskAcceptCmd_MissingSessionIsUsageError(t *testing.T) {
	dbPath := openTaskTestDB(t)
	t.Setenv("CODERO_DB_PATH", dbPath)
	t.Setenv("CODERO_SESSION_ID", "")

	_, err := runTaskAccept(t, "--task", "CLI-TASK-NO-SESSION")
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected UsageError, got %T: %v", err, err)
	}
}

func TestTaskAcceptCmd_MissingTaskIsUsageError(t *testing.T) {
	dbPath := openTaskTestDB(t)
	t.Setenv("CODERO_DB_PATH", dbPath)
	t.Setenv("CODERO_SESSION_ID", "cli-sess-has-env")

	_, err := runTaskAccept(t, "--session", "cli-sess-has-env")
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected UsageError, got %T: %v", err, err)
	}
}

// extractField extracts the value from a "key: value\n" line in output.
func extractField(output, key string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, key) {
			return strings.TrimSpace(strings.TrimPrefix(line, key))
		}
	}
	return ""
}
