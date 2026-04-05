package dashboard_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/state"
)

// openTestDB opens an in-memory SQLite DB with the full production schema.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := state.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db.Unwrap()
}

// newTestHandler creates a dashboard handler backed by a fresh test DB.
func newTestHandler(t *testing.T) (*dashboard.Handler, *sql.DB) {
	t.Helper()
	db := openTestDB(t)
	store := dashboard.NewSettingsStore(t.TempDir())
	cfg := config.LoadEnv()
	return dashboard.NewHandler(db, store, cfg), db
}

// seedBranch inserts one branch_states row.
func seedBranch(t *testing.T, db *sql.DB, repo, branch, st string) {
	t.Helper()
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, err := db.Exec(`INSERT INTO branch_states
		(id, repo, branch, head_hash, state, retry_count, max_retries, approved, ci_green,
		 pending_events, unresolved_threads, owner_session_id, queue_priority)
		VALUES (?,?,?,?,?,0,3,0,0,0,0,'',0)`,
		fmt.Sprintf("id-%s-%s", repo, branch), repo, branch, "abc123", st)
	if err != nil {
		t.Fatalf("seedBranch: %v", err)
	}
}

// seedBranchSession inserts one branch_states row with a fresh owner session.
func seedBranchSession(t *testing.T, db *sql.DB, repo, branch, st, sessionID string, lastSeen, submittedAt time.Time) {
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
		t.Fatalf("seedBranchSession: %v", err)
	}
}

// seedAgentSession inserts one agent_sessions row.
func seedAgentSession(t *testing.T, db *sql.DB, sessionID, agentID, mode string, startedAt, lastSeen time.Time) {
	t.Helper()
	if mode == "" {
		mode = "cli"
	}
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, err := db.Exec(`INSERT INTO agent_sessions
		(session_id, agent_id, mode, started_at, last_seen_at, last_progress_at, ended_at, end_reason)
		VALUES (?,?,?,?,?,?,NULL,'')`,
		sessionID, agentID, mode, startedAt, lastSeen, lastSeen)
	if err != nil {
		t.Fatalf("seedAgentSession: %v", err)
	}
}

func seedSessionRuntimeAttribution(t *testing.T, db *sql.DB, sessionID, repo, branch, source, confidence string) {
	t.Helper()
	_, err := db.Exec(`UPDATE agent_sessions
		SET repo = ?, branch = ?, attribution_source = ?, attribution_confidence = ?
		WHERE session_id = ?`,
		repo, branch, source, confidence, sessionID)
	if err != nil {
		t.Fatalf("seedSessionRuntimeAttribution: %v", err)
	}
}

func seedSessionRecovered(t *testing.T, db *sql.DB, sessionID string, recoveredAt time.Time) {
	t.Helper()
	_, err := db.Exec(`UPDATE agent_sessions SET last_recovered_at = ? WHERE session_id = ?`, recoveredAt, sessionID)
	if err != nil {
		t.Fatalf("seedSessionRecovered: %v", err)
	}
}

// seedAgentAssignment inserts one agent_assignments row.
func seedAgentAssignment(t *testing.T, db *sql.DB, assignmentID, sessionID, agentID, repo, branch, worktree, taskID string, startedAt time.Time) {
	t.Helper()
	seedAgentAssignmentWithSubstatus(t, db, assignmentID, sessionID, agentID, repo, branch, worktree, taskID, "", startedAt)
}

// seedAgentAssignmentWithSubstatus inserts one agent_assignments row with an optional substatus.
func seedAgentAssignmentWithSubstatus(t *testing.T, db *sql.DB, assignmentID, sessionID, agentID, repo, branch, worktree, taskID, substatus string, startedAt time.Time) {
	t.Helper()
	stateValue := "active"
	blockedReason := ""
	switch {
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
		assignmentID, sessionID, agentID, repo, branch, worktree, taskID, stateValue, blockedReason, substatus, startedAt)
	if err != nil {
		t.Fatalf("seedAgentAssignmentWithSubstatus: %v", err)
	}
}

// seedAgentEvent inserts one agent_events row.
func seedAgentEvent(t *testing.T, db *sql.DB, sessionID, agentID, eventType, payload string, createdAt time.Time) {
	t.Helper()
	if payload == "" {
		payload = "{}"
	}
	_, err := db.Exec(`INSERT INTO agent_events
		(session_id, agent_id, event_type, payload, created_at)
		VALUES (?,?,?,?,?)`,
		sessionID, agentID, eventType, payload, createdAt)
	if err != nil {
		t.Fatalf("seedAgentEvent: %v", err)
	}
}

// seedRun inserts one review_runs row.
func seedRun(t *testing.T, db *sql.DB, id, repo, branch, provider, status string, dur time.Duration) {
	t.Helper()
	started := time.Now().UTC()
	finished := started.Add(dur)
	_, err := db.Exec(`INSERT INTO review_runs
		(id, repo, branch, head_hash, provider, status, started_at, finished_at, error, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, repo, branch, "abc", provider, status, started, finished, "", started)
	if err != nil {
		t.Fatalf("seedRun: %v", err)
	}
}

// seedFinding inserts one findings row with a unique ID.
func seedFinding(t *testing.T, db *sql.DB, runID, repo, branch, severity, source string) {
	t.Helper()
	uid := fmt.Sprintf("f-%s-%s-%d", runID, source, time.Now().UnixNano())
	// nosemgrep: go.lang.security.audit.sqli.gosql-sqli.gosql-sqli
	_, err := db.Exec(`INSERT INTO findings
		(id, run_id, repo, branch, severity, category, file, line, message, source, rule_id, ts)
		VALUES (?,?,?,?,?,?,?,0,?,?,?,?)`,
		uid, runID, repo, branch,
		severity, "test", "file.go", "test finding", source, "rule-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("seedFinding: %v", err)
	}
}

// seedDeliveryEvent inserts one delivery_events row.
func seedDeliveryEvent(t *testing.T, db *sql.DB, seq int64, repo, branch, evType, payload string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO delivery_events (seq, repo, branch, head_hash, event_type, payload, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		seq, repo, branch, "abc", evType, payload, time.Now().UTC())
	if err != nil {
		t.Fatalf("seedDeliveryEvent: %v", err)
	}
}

// doRequest runs an HTTP request through the handler mux and returns the recorder.
func doRequest(t *testing.T, h *dashboard.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

/* ══════════════════════ OVERVIEW ═══════════════════════════ */

func TestOverview_EmptyDB(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/overview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var ov dashboard.OverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ov); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ov.RunsToday != 0 {
		t.Errorf("want 0 runs, got %d", ov.RunsToday)
	}
	if ov.PassRate != -1 {
		t.Errorf("want pass_rate -1, got %f", ov.PassRate)
	}
}

func TestOverview_WithRuns(t *testing.T) {
	h, db := newTestHandler(t)
	seedRun(t, db, "r1", "acme/api", "main", "litellm", "completed", 30*time.Second)
	seedRun(t, db, "r2", "acme/api", "main", "litellm", "failed", 20*time.Second)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/overview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var ov dashboard.OverviewResponse
	json.Unmarshal(rec.Body.Bytes(), &ov)
	if ov.RunsToday != 2 {
		t.Errorf("want 2 runs today, got %d", ov.RunsToday)
	}
	if ov.PassRate < 40 || ov.PassRate > 60 {
		t.Errorf("want ~50%% pass rate, got %f", ov.PassRate)
	}
	if ov.AvgGateSec <= 0 {
		t.Errorf("want avg gate sec > 0, got %f", ov.AvgGateSec)
	}
}

func TestOverview_BlockedCount(t *testing.T) {
	h, db := newTestHandler(t)
	seedBranch(t, db, "acme/api", "fix/bug", "blocked")
	seedBranch(t, db, "acme/api", "feat/x", "submitted")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/overview", nil)
	var ov dashboard.OverviewResponse
	json.Unmarshal(rec.Body.Bytes(), &ov)
	if ov.BlockedCount != 1 {
		t.Errorf("want 1 blocked, got %d", ov.BlockedCount)
	}
}

func TestOverview_MethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/overview", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rec.Code)
	}
}

/* ══════════════════════ REPOS ═══════════════════════════════ */

func TestRepos_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/repos", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	repos := out["repos"].([]interface{})
	if len(repos) != 0 {
		t.Errorf("want 0 repos, got %d", len(repos))
	}
}

func TestRepos_WithBranches(t *testing.T) {
	h, db := newTestHandler(t)
	seedBranch(t, db, "acme/api", "main", "submitted")
	seedBranch(t, db, "acme/ui", "feat/x", "merge_ready")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/repos", nil)
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	repos := out["repos"].([]interface{})
	if len(repos) != 2 {
		t.Errorf("want 2 repos, got %d", len(repos))
	}
}

/* ══════════════════════ RUNS ════════════════════════════════ */

func TestRuns_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/runs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	runs := out["runs"].([]interface{})
	if len(runs) != 0 {
		t.Errorf("want 0 runs, got %d", len(runs))
	}
}

func TestRuns_ReturnsSorted(t *testing.T) {
	h, db := newTestHandler(t)
	seedRun(t, db, "run-1", "acme/api", "main", "litellm", "completed", 10*time.Second)
	seedRun(t, db, "run-2", "acme/api", "main", "coderabbit", "failed", 5*time.Second)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/runs", nil)
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	runs := out["runs"].([]interface{})
	if len(runs) != 2 {
		t.Errorf("want 2 runs, got %d", len(runs))
	}
}

/* ══════════════════════ ACTIVITY ════════════════════════════ */

func TestActivity_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/activity", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	evts := out["events"].([]interface{})
	if len(evts) != 0 {
		t.Errorf("want 0 events, got %d", len(evts))
	}
}

func TestActivity_WithEvents(t *testing.T) {
	h, db := newTestHandler(t)
	seedDeliveryEvent(t, db, 1, "acme/api", "main", "state_transition", `{"to_state":"review_approved"}`)
	seedDeliveryEvent(t, db, 2, "acme/api", "main", "finding_bundle", `{"message":"semgrep blocked"}`)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/activity", nil)
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	evts := out["events"].([]interface{})
	if len(evts) != 2 {
		t.Errorf("want 2 events, got %d", len(evts))
	}
}

/* ══════════════════════ BLOCK REASONS ═══════════════════════ */

func TestBlockReasons_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/block-reasons", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	reasons := out["reasons"].([]interface{})
	if len(reasons) != 0 {
		t.Errorf("want 0 reasons, got %d", len(reasons))
	}
}

func TestBlockReasons_Ranked(t *testing.T) {
	h, db := newTestHandler(t)
	seedRun(t, db, "run-a", "acme/api", "main", "semgrep", "failed", 5*time.Second)
	seedFinding(t, db, "run-a", "acme/api", "main", "error", "semgrep")
	seedFinding(t, db, "run-a", "acme/api", "main", "error", "semgrep")
	seedFinding(t, db, "run-a", "acme/api", "main", "error", "gitleaks")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/block-reasons", nil)
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	reasons := out["reasons"].([]interface{})
	if len(reasons) < 1 {
		t.Fatal("want at least 1 reason")
	}
	top := reasons[0].(map[string]interface{})
	if top["source"] != "semgrep" {
		t.Errorf("want semgrep as top blocker, got %v", top["source"])
	}
}

/* ══════════════════════ GATE HEALTH ═════════════════════════ */

func TestGateHealth_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate-health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestGateHealth_PassRates(t *testing.T) {
	h, db := newTestHandler(t)
	seedRun(t, db, "r1", "acme/api", "main", "litellm", "completed", 10*time.Second)
	seedRun(t, db, "r2", "acme/api", "main", "litellm", "completed", 10*time.Second)
	seedRun(t, db, "r3", "acme/api", "main", "litellm", "failed", 5*time.Second)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate-health", nil)
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	gates := out["gates"].([]interface{})
	if len(gates) != 1 {
		t.Fatalf("want 1 gate, got %d", len(gates))
	}
	g := gates[0].(map[string]interface{})
	pct := g["pass_rate"].(float64)
	if pct < 65 || pct > 70 {
		t.Errorf("want ~66.7%% pass rate, got %f", pct)
	}
}

/* ══════════════════════ SETTINGS ════════════════════════════ */

func TestSettings_GetDefaults(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var s dashboard.SettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(s.Integrations) == 0 {
		t.Error("want at least one integration")
	}
	if len(s.GatePipeline) == 0 {
		t.Error("want at least one gate in pipeline")
	}
}

func TestSettings_PutValid(t *testing.T) {
	h, _ := newTestHandler(t)

	// First fetch current state.
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/settings", nil)
	var s dashboard.SettingsResponse
	json.Unmarshal(rec.Body.Bytes(), &s)

	// Toggle first integration.
	if len(s.Integrations) > 0 {
		s.Integrations[0].Connected = !s.Integrations[0].Connected
	}

	body, _ := json.Marshal(dashboard.SettingsUpdateRequest{
		Integrations: s.Integrations,
		GatePipeline: s.GatePipeline,
	})
	putRec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/settings", bytes.NewReader(body))
	if putRec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", putRec.Code, putRec.Body.String())
	}

	// Re-fetch and verify.
	getAfter := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/settings", nil)
	var after dashboard.SettingsResponse
	json.Unmarshal(getAfter.Body.Bytes(), &after)
	if len(s.Integrations) > 0 && after.Integrations[0].Connected != s.Integrations[0].Connected {
		t.Error("settings change did not persist")
	}
}

func TestSettings_PutInvalidGateTimeout(t *testing.T) {
	h, _ := newTestHandler(t)
	body := `{"gate_pipeline":[{"name":"gitleaks","enabled":true,"blocks_commit":true,"timeout_sec":-1,"provider":"built-in"}]}`
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/settings", strings.NewReader(body))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSettings_PutInvalidJSON(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/settings", strings.NewReader("{bad json}"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestSettings_PutEmptyGateName(t *testing.T) {
	h, _ := newTestHandler(t)
	body := `{"gate_pipeline":[{"name":"","enabled":true,"blocks_commit":false,"timeout_sec":30,"provider":"built-in"}]}`
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/settings", strings.NewReader(body))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

/* ══════════════════════ UPLOAD ══════════════════════════════ */

func TestUpload_ValidFile(t *testing.T) {
	h, _ := newTestHandler(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "fix-auth.go")
	io.WriteString(fw, "package main\nfunc main(){}")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/manual-review-upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dashboard.UploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RunID == "" {
		t.Error("want non-empty run_id")
	}
	if resp.Status != "pending" {
		t.Errorf("want status pending, got %q", resp.Status)
	}
}

func TestUpload_RejectedExtension(t *testing.T) {
	h, _ := newTestHandler(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "malware.exe")
	io.WriteString(fw, "bad content")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/manual-review-upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpload_MissingFileField(t *testing.T) {
	h, _ := newTestHandler(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("repo", "acme/api")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/manual-review-upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestUpload_WrongMethod(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/manual-review-upload", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rec.Code)
	}
}

/* ══════════════════════ COVERAGE UPLOAD ═════════════════════ */

func TestCoverageUpload_Success(t *testing.T) {
	h, _ := newTestHandler(t)
	dir := t.TempDir()
	covPath := filepath.Join(dir, "coverage.out")
	t.Setenv("CODERO_COVERAGE_PATH", covPath)

	content := "mode: set\ngithub.com/codero/codero/file.go:1.1,2.2 2 1\n"
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "coverage.out")
	io.WriteString(fw, content)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/coverage-upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.CoverageUploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CoveragePct == nil || *resp.CoveragePct != 100.0 {
		t.Errorf("want 100%% coverage, got %v", resp.CoveragePct)
	}
	if resp.Path != covPath {
		t.Errorf("path = %q, want %q", resp.Path, covPath)
	}

	// Verify it persists on disk.
	saved, _ := os.ReadFile(covPath)
	if string(saved) != content {
		t.Errorf("saved content mismatch")
	}

	// Verify health endpoint reflects it.
	healthRec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/health", nil)
	var health dashboard.DashboardHealth
	json.Unmarshal(healthRec.Body.Bytes(), &health)
	if health.CoveragePct == nil || *health.CoveragePct != 100.0 {
		t.Errorf("health coverage_pct = %v, want 100", health.CoveragePct)
	}
}

func TestCoverageUpload_InvalidMethod(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/coverage-upload", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rec.Code)
	}
}

func TestCoverageUpload_MissingFile(t *testing.T) {
	h, _ := newTestHandler(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("foo", "bar")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/coverage-upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestCoverageUpload_MalformedContent(t *testing.T) {
	h, _ := newTestHandler(t)
	dir := t.TempDir()
	covPath := filepath.Join(dir, "coverage.out")
	t.Setenv("CODERO_COVERAGE_PATH", covPath)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "coverage.out")
	io.WriteString(fw, "not a coverage file")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/coverage-upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp dashboard.CoverageUploadResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.CoveragePct != nil {
		t.Errorf("want nil coverage_pct for malformed file, got %v", resp.CoveragePct)
	}
}

/* ══════════════════════ STATIC EMBED ═══════════════════════ */

func TestStaticEmbedHasIndexHTML(t *testing.T) {
	subFS, err := fs.Sub(dashboard.Static, "static")
	if err != nil {
		t.Fatalf("sub FS: %v", err)
	}
	// Verify index.html is embedded and references the split assets.
	f, err := subFS.Open("index.html")
	if err != nil {
		t.Fatalf("index.html not embedded: %v", err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Contains(data, []byte("codero")) {
		t.Error("index.html does not contain expected content")
	}
	if !bytes.Contains(data, []byte(`href="./styles.css"`)) {
		t.Error("index.html does not reference ./styles.css")
	}
	if !bytes.Contains(data, []byte(`src="./js/main.js"`)) {
		t.Error("index.html does not reference ./js/main.js")
	}

	// Verify styles.css is embedded.
	sf, err := subFS.Open("styles.css")
	if err != nil {
		t.Fatalf("styles.css not embedded: %v", err)
	}
	sData, _ := io.ReadAll(sf)
	sf.Close()
	if len(sData) == 0 {
		t.Error("styles.css is empty")
	}

	// Verify js/main.js is embedded.
	jf, err := subFS.Open("js/main.js")
	if err != nil {
		t.Fatalf("js/main.js not embedded: %v", err)
	}
	jData, _ := io.ReadAll(jf)
	jf.Close()
	if len(jData) == 0 {
		t.Error("js/main.js is empty")
	}
}

/* ══════════════════════ SETTINGS STORE ══════════════════════ */

func TestSettingsStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := dashboard.NewSettingsStore(dir)

	ps, err := store.Load()
	if err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if len(ps.Integrations) == 0 {
		t.Fatal("want default integrations")
	}

	// Flip first integration.
	original := ps.Integrations[0].Connected
	ps.Integrations[0].Connected = !original

	if err := store.Save(&dashboard.SettingsUpdateRequest{
		Integrations: ps.Integrations,
		GatePipeline: ps.GatePipeline,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Integrations[0].Connected != !original {
		t.Error("setting did not persist after reload")
	}
}

func TestSettingsStore_PersistFile(t *testing.T) {
	dir := t.TempDir()
	store := dashboard.NewSettingsStore(dir)
	ps, _ := store.Load()
	_ = store.Save(&dashboard.SettingsUpdateRequest{
		Integrations: ps.Integrations,
		GatePipeline: ps.GatePipeline,
	})
	if _, err := os.Stat(filepath.Join(dir, "dashboard-settings.json")); err != nil {
		t.Errorf("settings file not created: %v", err)
	}
}

// Ensure unused imports are used.
var _ = fmt.Sprintf
var _ = sql.ErrNoRows
var _ fs.FS = nil

/* ══════════════════════ GATE CHECKS ═════════════════════════ */

func TestGateChecks_NoReport(t *testing.T) {
	h, _ := newTestHandler(t)
	// No report file → expect 200 with report:null
	t.Setenv("CODERO_GATE_CHECK_REPORT_PATH", filepath.Join(t.TempDir(), "nonexistent.json"))
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate-checks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(resp["report"]) != "null" {
		t.Errorf("expected report:null when no file, got %s", resp["report"])
	}
	if _, ok := resp["message"]; !ok {
		t.Error("expected 'message' field when no report")
	}
}

func TestGateChecks_WithReport(t *testing.T) {
	h, _ := newTestHandler(t)

	// Write a minimal gate-check report JSON to a temp file.
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "last-report.json")
	sampleReport := `{
"summary": {"overall_status":"pass","passed":1,"failed":0,"skipped":2,"infra_bypassed":0,"disabled":3,"total":6,"required_failed":0,"required_disabled":0,"profile":"portable","schema_version":"1"},
"checks": [
{"id":"file-size","name":"File size limit","group":"format","required":true,"enabled":true,"status":"skip","reason":"no staged files","reason_code":"not_in_scope","duration_ms":0},
{"id":"ai-gate","name":"AI review gate","group":"ai","required":false,"enabled":false,"status":"disabled","reason_code":"not_in_scope","reason":"AI gate runs separately","duration_ms":0}
],
"run_at":"2025-01-01T00:00:00Z"
}`
	if err := os.WriteFile(reportPath, []byte(sampleReport), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CODERO_GATE_CHECK_REPORT_PATH", reportPath)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate-checks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["report"]; !ok {
		t.Error("expected 'report' field in response")
	}
	if _, ok := resp["generated_at"]; !ok {
		t.Error("expected 'generated_at' field in response")
	}
	if _, ok := resp["report_path"]; !ok {
		t.Error("expected 'report_path' field in response")
	}
	// Report should not be null
	if string(resp["report"]) == "null" {
		t.Error("report should not be null when file exists")
	}
	var reportPathOut string
	if err := json.Unmarshal(resp["report_path"], &reportPathOut); err != nil {
		t.Fatalf("unmarshal report_path: %v", err)
	}
	if reportPathOut != reportPath {
		t.Fatalf("report_path = %q, want %q", reportPathOut, reportPath)
	}

	var report gatecheck.Report
	if err := json.Unmarshal(resp["report"], &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Summary.OverallStatus != gatecheck.StatusPass {
		t.Fatalf("overall_status = %q, want %q", report.Summary.OverallStatus, gatecheck.StatusPass)
	}
	if len(report.Checks) != 2 {
		t.Fatalf("checks length = %d, want 2", len(report.Checks))
	}
	if report.Checks[0].ReasonCode != gatecheck.ReasonNotInScope || report.Checks[0].Reason != "no staged files" {
		t.Fatalf("first check lost reason fields: got reason_code=%q reason=%q", report.Checks[0].ReasonCode, report.Checks[0].Reason)
	}
	if report.Checks[1].ReasonCode != gatecheck.ReasonNotInScope || report.Checks[1].Reason != "AI gate runs separately" {
		t.Fatalf("second check lost reason fields: got reason_code=%q reason=%q", report.Checks[1].ReasonCode, report.Checks[1].Reason)
	}
}

func TestGateChecks_MethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/gate-checks", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

/* ═══════════════════ ACTIVE SESSIONS ════════════════════════ */

func TestActiveSessions_Empty(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ActiveCount != 0 {
		t.Fatalf("active_count = %d, want 0", resp.ActiveCount)
	}
	if len(resp.Sessions) != 0 {
		t.Fatalf("sessions length = %d, want 0", len(resp.Sessions))
	}
}

func TestActiveSessions_WithFreshSession(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-45 * time.Minute).UTC()
	lastSeen := time.Now().Add(-2 * time.Minute).UTC()
	seedAgentSession(t, db, "sess-123", "agent-1", "cli", startedAt, lastSeen)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ActiveCount != 1 {
		t.Fatalf("active_count = %d, want 1", resp.ActiveCount)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions length = %d, want 1", len(resp.Sessions))
	}
	s := resp.Sessions[0]
	if s.SessionID != "sess-123" || s.AgentID != "agent-1" {
		t.Fatalf("unexpected session row: %+v", s)
	}
	if s.ActivityState != "orphaned" {
		t.Fatalf("activity_state = %q, want orphaned", s.ActivityState)
	}
	if s.LifecycleState != "orphaned" {
		t.Fatalf("lifecycle_state = %q, want orphaned", s.LifecycleState)
	}
	if s.AttachmentState != "orphaned" {
		t.Fatalf("attachment_state = %q, want orphaned", s.AttachmentState)
	}
	if s.OwnerAgent != "agent-1" {
		t.Fatalf("owner_agent = %q, want agent-1", s.OwnerAgent)
	}
	if s.ElapsedSec <= 0 {
		t.Fatalf("elapsed_sec = %d, want > 0", s.ElapsedSec)
	}
	if s.LastHeartbeatAt.IsZero() {
		t.Fatalf("last_heartbeat_at must be set")
	}
	if s.ProgressAt == nil || s.ProgressAt.IsZero() {
		t.Fatalf("progress_at must be set for seeded agent sessions")
	}
}

func TestActiveSessions_OmitsNilProgressAt(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-30 * time.Minute).UTC()
	lastSeen := time.Now().Add(-1 * time.Minute).UTC()

	_, err := db.Exec(`INSERT INTO agent_sessions
		(session_id, agent_id, mode, started_at, last_seen_at, last_progress_at, ended_at, end_reason)
		VALUES (?,?,?,?,?,NULL,NULL,'')`,
		"sess-noprogress", "agent-noprogress", "cli", startedAt, lastSeen)
	if err != nil {
		t.Fatalf("seedAgentSession without progress: %v", err)
	}

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ActiveCount != 1 {
		t.Fatalf("active_count = %d, want 1", resp.ActiveCount)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions length = %d, want 1", len(resp.Sessions))
	}
	s := resp.Sessions[0]
	if s.ProgressAt != nil {
		t.Fatalf("progress_at = %v, want nil", s.ProgressAt)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode raw body: %v", err)
	}
	sessions, ok := body["sessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("raw sessions = %#v, want 1 row", body["sessions"])
	}
	session, ok := sessions[0].(map[string]any)
	if !ok {
		t.Fatalf("raw session row = %#v, want object", sessions[0])
	}
	if _, exists := session["progress_at"]; exists {
		t.Fatalf("progress_at should be omitted from response JSON: %#v", session)
	}
}

func TestActiveSessions_DuplicateOwnerSession(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-20 * time.Minute).UTC()
	lastSeen := time.Now().Add(-1 * time.Minute).UTC()

	seedAgentSession(t, db, "sess-dup", "agent-dup", "cli", startedAt, lastSeen)
	seedAgentAssignment(t, db, "assign-1", "sess-dup", "agent-dup", "acme/api", "feat/a", "", "", startedAt)
	seedAgentAssignment(t, db, "assign-2", "sess-dup", "agent-dup", "acme/api", "feat/b", "", "", startedAt.Add(2*time.Minute))

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ActiveCount != 1 || len(resp.Sessions) != 1 {
		t.Fatalf("expected 1 unique session, got active_count=%d len=%d", resp.ActiveCount, len(resp.Sessions))
	}
	if resp.Sessions[0].SessionID != "sess-dup" {
		t.Fatalf("session_id = %q, want sess-dup", resp.Sessions[0].SessionID)
	}
	if resp.Sessions[0].Branch != "feat/b" {
		t.Fatalf("branch = %q, want latest assignment branch feat/b", resp.Sessions[0].Branch)
	}
}

func TestActiveSessions_AssignmentSubstatusWaiting(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-20 * time.Minute).UTC()
	lastSeen := time.Now().Add(-45 * time.Second).UTC()

	seedAgentSession(t, db, "sess-wait", "agent-wait", "cli", startedAt, lastSeen)
	seedAgentAssignmentWithSubstatus(t, db, "assign-wait", "sess-wait", "agent-wait", "acme/api", "feat/COD-071-waiting", "", "COD-071", "waiting_for_ci", startedAt)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions length = %d, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].ActivityState != "syncing" {
		t.Fatalf("activity_state = %q, want syncing", resp.Sessions[0].ActivityState)
	}
	if resp.Sessions[0].Task == nil || resp.Sessions[0].Task.Phase != "syncing" {
		t.Fatalf("task phase = %#v, want syncing", resp.Sessions[0].Task)
	}
}

func TestActiveSessions_FiltersStale(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-2 * time.Hour).UTC()
	lastSeen := time.Now().Add(-31 * time.Minute).UTC()
	seedAgentSession(t, db, "sess-old", "agent-old", "cli", startedAt, lastSeen)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ActiveCount != 0 {
		t.Fatalf("active_count = %d, want 0", resp.ActiveCount)
	}
	if len(resp.Sessions) != 0 {
		t.Fatalf("sessions length = %d, want 0", len(resp.Sessions))
	}
}

func TestActiveSessions_MethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestAssignments_WithAgentAssignments(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-15 * time.Minute).UTC()
	lastSeen := time.Now().Add(-30 * time.Second).UTC()
	seedAgentSession(t, db, "sess-assign-1", "agent-assign-1", "cli", startedAt, lastSeen)
	seedAgentAssignment(t, db, "assign-1", "sess-assign-1", "agent-assign-1", "acme/api", "feat/live", "", "COD-100", startedAt)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/assignments", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.AssignmentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 1 || len(resp.Assignments) != 1 {
		t.Fatalf("assignments count = %d len=%d, want 1", resp.Count, len(resp.Assignments))
	}
	if resp.Assignments[0].State != "active" {
		t.Fatalf("state = %q, want active", resp.Assignments[0].State)
	}
	if resp.Assignments[0].TaskID != "COD-100" {
		t.Fatalf("task_id = %q, want COD-100", resp.Assignments[0].TaskID)
	}
}

func TestAssignments_SubstatusAndTerminalState(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-15 * time.Minute).UTC()
	lastSeen := time.Now().Add(-30 * time.Second).UTC()
	endedAt := time.Now().Add(-2 * time.Minute).UTC()

	seedAgentSession(t, db, "sess-assign-live", "agent-assign-live", "cli", startedAt, lastSeen)
	seedAgentSession(t, db, "sess-assign-done", "agent-assign-done", "cli", startedAt.Add(-time.Minute), lastSeen.Add(-time.Minute))
	seedAgentAssignmentWithSubstatus(t, db, "assign-live", "sess-assign-live", "agent-assign-live", "acme/api", "feat/live", "", "COD-101", "waiting_for_ci", startedAt)
	seedAgentAssignmentWithSubstatus(t, db, "assign-done", "sess-assign-done", "agent-assign-done", "acme/api", "feat/done", "", "COD-102", "terminal_finished", startedAt.Add(-time.Minute))
	if _, err := db.Exec(`UPDATE agent_assignments SET ended_at = ?, end_reason = 'done', state = 'completed' WHERE assignment_id = ?`, endedAt, "assign-done"); err != nil {
		t.Fatalf("finalize seeded assignment: %v", err)
	}
	if _, err := db.Exec(`UPDATE agent_sessions SET ended_at = ?, end_reason = 'done' WHERE session_id = ?`, endedAt, "sess-assign-done"); err != nil {
		t.Fatalf("finalize seeded session: %v", err)
	}

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/assignments", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.AssignmentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 || len(resp.Assignments) != 2 {
		t.Fatalf("assignments count = %d len=%d, want 2", resp.Count, len(resp.Assignments))
	}

	var live, done *dashboard.AssignmentSummary
	for i := range resp.Assignments {
		switch resp.Assignments[i].AssignmentID {
		case "assign-live":
			live = &resp.Assignments[i]
		case "assign-done":
			done = &resp.Assignments[i]
		}
	}
	if live == nil || done == nil {
		t.Fatalf("missing assignments in response: %+v", resp.Assignments)
	}
	if live.Substatus != "waiting_for_ci" || live.ActivityState != "waiting" || live.State != "active" {
		t.Fatalf("live assignment = %+v, want waiting substatus with active state", *live)
	}
	if done.Substatus != "terminal_finished" || done.State != "completed" {
		t.Fatalf("done assignment = %+v, want completed terminal state", *done)
	}
	if done.ActivityState != "" {
		t.Fatalf("done activity_state = %q, want empty", done.ActivityState)
	}
}

func TestAssignments_EndedRowsDoNotInheritLiveSessionActivity(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-30 * time.Minute).UTC()
	lastSeen := time.Now().Add(-20 * time.Second).UTC()
	endedAt := time.Now().Add(-5 * time.Minute).UTC()

	seedAgentSession(t, db, "sess-assign-mixed", "agent-mixed", "cli", startedAt, lastSeen)
	seedAgentAssignmentWithSubstatus(t, db, "assign-old", "sess-assign-mixed", "agent-mixed", "acme/api", "feat/old", "", "COD-201", "terminal_finished", startedAt)
	if _, err := db.Exec(`UPDATE agent_assignments SET ended_at = ?, end_reason = 'done', state = 'completed' WHERE assignment_id = ?`, endedAt, "assign-old"); err != nil {
		t.Fatalf("finalize old assignment: %v", err)
	}
	seedAgentAssignmentWithSubstatus(t, db, "assign-live", "sess-assign-mixed", "agent-mixed", "acme/api", "feat/live", "", "COD-202", "waiting_for_ci", startedAt.Add(10*time.Minute))

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/assignments", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.AssignmentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var ended *dashboard.AssignmentSummary
	for i := range resp.Assignments {
		if resp.Assignments[i].AssignmentID == "assign-old" {
			ended = &resp.Assignments[i]
			break
		}
	}
	if ended == nil {
		t.Fatalf("missing ended assignment in response: %+v", resp.Assignments)
	}
	if ended.ActivityState != "" {
		t.Fatalf("ended activity_state = %q, want empty", ended.ActivityState)
	}
	if ended.Mode != "" {
		t.Fatalf("ended mode = %q, want empty", ended.Mode)
	}
}

func TestCompliance_Empty(t *testing.T) {
	h, _ := newTestHandler(t)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/compliance", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ComplianceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 4 {
		t.Fatalf("rules len = %d, want 4 seeded rules", len(resp.Rules))
	}
	wantRuleIDs := []string{"RULE-001", "RULE-002", "RULE-003", "RULE-004"}
	for i, want := range wantRuleIDs {
		if resp.Rules[i].RuleID != want {
			t.Fatalf("rules[%d].rule_id = %q, want %q", i, resp.Rules[i].RuleID, want)
		}
	}
	if len(resp.Checks) != 0 {
		t.Fatalf("checks len = %d, want 0", len(resp.Checks))
	}
}

func TestCompliance_WithRulesAndChecks(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-15 * time.Minute).UTC()
	lastSeen := time.Now().Add(-1 * time.Minute).UTC()
	checkedAt := time.Now().Add(-2 * time.Minute).UTC()

	seedAgentSession(t, db, "sess-comp-1", "agent-comp-1", "cli", startedAt, lastSeen)
	seedAgentAssignment(t, db, "assign-comp-1", "sess-comp-1", "agent-comp-1", "acme/api", "task-compliance", "", "COD-200", startedAt)

	_, err := db.Exec(`
		INSERT INTO assignment_rule_checks
			(check_id, assignment_id, session_id, rule_id, rule_version, checked_at, result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, '')`,
		"chk-comp-1", "assign-comp-1", "sess-comp-1", "RULE-001", 1, checkedAt, "fail", 1, `["block","notify"]`, `{"reason":"ci red"}`,
	)
	if err != nil {
		t.Fatalf("insert assignment rule check: %v", err)
	}

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/compliance", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ComplianceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 4 {
		t.Fatalf("rules len = %d, want 4", len(resp.Rules))
	}
	if len(resp.Checks) != 1 {
		t.Fatalf("checks len = %d, want 1", len(resp.Checks))
	}
	var rule001 *dashboard.AgentRuleRow
	for i := range resp.Rules {
		if resp.Rules[i].RuleID == "RULE-001" {
			rule001 = &resp.Rules[i]
			break
		}
	}
	if rule001 == nil {
		t.Fatalf("missing RULE-001 in %+v", resp.Rules)
	}
	if len(rule001.ViolationAction) != 2 {
		t.Fatalf("rule violation_action len = %d, want 2", len(rule001.ViolationAction))
	}
	if resp.Checks[0].AssignmentID != "assign-comp-1" {
		t.Fatalf("assignment_id = %q, want assign-comp-1", resp.Checks[0].AssignmentID)
	}
	if !resp.Checks[0].ViolationRaised {
		t.Fatal("violation_raised = false, want true")
	}
	if len(resp.Checks[0].ViolationActionTaken) != 2 {
		t.Fatalf("violation_action_taken len = %d, want 2", len(resp.Checks[0].ViolationActionTaken))
	}
}

func TestAgentEvents_WithRows(t *testing.T) {
	h, db := newTestHandler(t)
	ts := time.Now().Add(-2 * time.Minute).UTC()
	seedAgentSession(t, db, "sess-evt-1", "agent-evt-1", "cli", ts, ts)
	seedAgentEvent(t, db, "sess-evt-1", "agent-evt-1", "session_registered", `{"mode":"cli"}`, ts)
	seedAgentEvent(t, db, "sess-evt-1", "agent-evt-1", "assignment_attached", `{"assignment_id":"assign-evt-1"}`, ts.Add(30*time.Second))

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/agent-events", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.AgentEventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != 2 || len(resp.Events) != 2 {
		t.Fatalf("events count = %d len=%d, want 2", resp.Count, len(resp.Events))
	}
	if resp.Events[0].EventType != "assignment_attached" {
		t.Fatalf("latest event_type = %q, want assignment_attached", resp.Events[0].EventType)
	}
}

/* ══════════════════════ TASK CONTEXT ═══════════════════════ */

func TestActiveSessions_TaskContextParsed(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-10 * time.Minute).UTC()
	lastSeen := time.Now().Add(-30 * time.Second).UTC()
	// Branch follows the feat/PROJ-ID-description pattern.
	seedAgentSession(t, db, "sess-task", "agent-task", "cli", startedAt, lastSeen)
	seedAgentAssignment(t, db, "assign-task", "sess-task", "agent-task", "acme/api", "feat/COD-042-fix-auth-token", "", "", startedAt)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.Sessions))
	}
	s := resp.Sessions[0]
	if s.Task == nil {
		t.Fatal("task context must not be nil")
	}
	if s.Task.ID != "COD-042" {
		t.Errorf("task.id = %q, want COD-042", s.Task.ID)
	}
	if s.Task.Title != "fix auth token" {
		t.Errorf("task.title = %q, want 'fix auth token'", s.Task.Title)
	}
	if s.Task.Phase == "" {
		t.Error("task.phase must not be empty")
	}
}

func TestActiveSessions_TaskContextFallback(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-5 * time.Minute).UTC()
	lastSeen := time.Now().Add(-20 * time.Second).UTC()
	// Branch does not follow the feat/PROJ-ID-description pattern.
	seedAgentSession(t, db, "sess-noid", "agent-noid", "cli", startedAt, lastSeen)
	seedAgentAssignment(t, db, "assign-noid", "sess-noid", "agent-noid", "acme/api", "hotfix", "", "", startedAt)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.Sessions))
	}
	// Task must be nil for branches that do not match the PROJ-NNN-desc pattern.
	if resp.Sessions[0].Task != nil {
		t.Fatalf("task must be nil for unrecognised branch, got %+v", resp.Sessions[0].Task)
	}
	if resp.Sessions[0].AttachmentState != "attached" {
		t.Errorf("attachment_state = %q, want attached", resp.Sessions[0].AttachmentState)
	}
	if resp.Sessions[0].ActivityState != "idle" {
		t.Errorf("activity_state = %q, want idle", resp.Sessions[0].ActivityState)
	}
}

func TestActiveSessions_UsesLaunchContextAttribution(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-8 * time.Minute).UTC()
	lastSeen := time.Now().Add(-30 * time.Second).UTC()
	seedAgentSession(t, db, "sess-launch", "codex-a", "coding", startedAt, lastSeen)
	seedSessionRuntimeAttribution(t, db, "sess-launch", "acme/api", "feat/COD-099-unify-runtime", "launch_context", "medium")

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.Sessions))
	}
	s := resp.Sessions[0]
	if s.Repo != "acme/api" || s.Branch != "feat/COD-099-unify-runtime" {
		t.Fatalf("repo/branch = %q/%q, want launch-context values", s.Repo, s.Branch)
	}
	if s.AttributionSource != "launch_context" || s.AttributionConfidence != "medium" {
		t.Fatalf("attribution = %q/%q, want launch_context/medium", s.AttributionSource, s.AttributionConfidence)
	}
	if s.AttachmentState != "inferred" {
		t.Fatalf("attachment_state = %q, want inferred", s.AttachmentState)
	}
	if s.LifecycleState != "attributed" {
		t.Fatalf("lifecycle_state = %q, want attributed", s.LifecycleState)
	}
	if s.Family != "codex" {
		t.Fatalf("family = %q, want codex", s.Family)
	}
}

func TestActiveSessions_RecoveredLifecycle(t *testing.T) {
	h, db := newTestHandler(t)
	startedAt := time.Now().Add(-12 * time.Minute).UTC()
	lastSeen := time.Now().Add(-10 * time.Second).UTC()
	seedAgentSession(t, db, "sess-recovered", "claude-pro", "coding", startedAt, lastSeen)
	seedAgentAssignment(t, db, "assign-recovered", "sess-recovered", "claude-pro", "acme/api", "feat/COD-101-recover", "", "COD-101", startedAt)
	seedSessionRecovered(t, db, "sess-recovered", lastSeen)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].LifecycleState != "recovered" {
		t.Fatalf("lifecycle_state = %q, want recovered", resp.Sessions[0].LifecycleState)
	}
}

/* ══════════════════════ DASHBOARD HEALTH ═══════════════════ */

func TestDashboardHealth_OK(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.DashboardHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Database.Status != "ok" {
		t.Errorf("database.status = %q, want ok", resp.Database.Status)
	}
	if resp.GeneratedAt.IsZero() {
		t.Error("generated_at must not be zero")
	}
	if resp.ReconciliationStatus == "" {
		t.Error("reconciliation_status must not be empty")
	}
	// Feeds must be populated (status may be "unavailable" in empty test DB).
	if resp.Feeds.ActiveSessions.Status == "" {
		t.Error("feeds.active_sessions.status must not be empty")
	}
	if resp.Feeds.GateChecks.Status == "" {
		t.Error("feeds.gate_checks.status must not be empty")
	}
}

func TestDashboardHealth_MethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodPost, "/api/v1/dashboard/health", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rec.Code)
	}
}

func TestDashboardHealth_ActiveAgentCount(t *testing.T) {
	h, db := newTestHandler(t)
	// Seed one visible fresh session and one ended session that should not be counted.
	lastSeen := time.Now().Add(-30 * time.Second).UTC()
	seedAgentSession(t, db, "sess-health-1", "agent-health-1", "cli", lastSeen, lastSeen)
	seedAgentSession(t, db, "sess-health-2", "agent-health-2", "cli", lastSeen, lastSeen)
	if _, err := db.Exec(`UPDATE agent_sessions SET ended_at = datetime('now') WHERE session_id = ?`, "sess-health-2"); err != nil {
		t.Fatalf("mark session ended: %v", err)
	}

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.DashboardHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ActiveAgentCount != 1 {
		t.Errorf("active_agent_count = %d, want 1", resp.ActiveAgentCount)
	}
	if resp.StaleSessionCount != 0 {
		t.Errorf("stale_session_count = %d, want 0", resp.StaleSessionCount)
	}
	if resp.ExpiredSessionCount != 0 {
		t.Errorf("expired_session_count = %d, want 0", resp.ExpiredSessionCount)
	}
	if resp.ReconciliationStatus != "ok" {
		t.Errorf("reconciliation_status = %q, want ok", resp.ReconciliationStatus)
	}
	// Sessions feed must be "ok" (fresh heartbeat within threshold).
	if resp.Feeds.ActiveSessions.Status != "ok" {
		t.Errorf("feeds.active_sessions.status = %q, want ok", resp.Feeds.ActiveSessions.Status)
	}
}

func TestDashboardHealth_GateCheckDirectoryUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CODERO_GATE_CHECK_REPORT_PATH", tmpDir)

	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.DashboardHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Feeds.GateChecks.Status != "unavailable" {
		t.Errorf("feeds.gate_checks.status = %q, want unavailable for directory path", resp.Feeds.GateChecks.Status)
	}
}

func TestDashboardHealth_StaleFeedDetected(t *testing.T) {
	h, db := newTestHandler(t)
	// Seed a session with a heartbeat older than the stale threshold (5 min).
	lastSeen := time.Now().Add(-10 * time.Minute).UTC()
	seedAgentSession(t, db, "sess-stale", "agent-stale", "cli", lastSeen, lastSeen)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.DashboardHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Sessions feed should be "stale" since the last heartbeat is > 5 min ago.
	if resp.Feeds.ActiveSessions.Status != "stale" {
		t.Errorf("feeds.active_sessions.status = %q, want stale", resp.Feeds.ActiveSessions.Status)
	}
	if resp.StaleSessionCount != 1 {
		t.Errorf("stale_session_count = %d, want 1", resp.StaleSessionCount)
	}
	if resp.ExpiredSessionCount != 0 {
		t.Errorf("expired_session_count = %d, want 0", resp.ExpiredSessionCount)
	}
	if resp.ReconciliationStatus != "stale" {
		t.Errorf("reconciliation_status = %q, want stale", resp.ReconciliationStatus)
	}
}

func TestDashboardHealth_GateCheckPathFromEnv(t *testing.T) {
	// The health endpoint must use the same report-path resolution as gate-checks:
	// honour CODERO_GATE_CHECK_REPORT_PATH when set.
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom-report.json")
	if err := os.WriteFile(customPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write custom report: %v", err)
	}

	t.Setenv("CODERO_GATE_CHECK_REPORT_PATH", customPath)

	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dashboard.DashboardHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The report file exists and was written just now, so feed should be "ok".
	if resp.Feeds.GateChecks.Status != "ok" {
		t.Errorf("feeds.gate_checks.status = %q, want ok (custom path %s)", resp.Feeds.GateChecks.Status, customPath)
	}
}

/* ══════════════════════ STATIC DASHBOARD UI ════════════════ */

func TestDashboardHTML_HasExpectedContent(t *testing.T) {
	subFS, err := fs.Sub(dashboard.Static, "static")
	if err != nil {
		t.Fatalf("sub FS: %v", err)
	}

	// Check HTML shell content.
	htmlData, err := fs.ReadFile(subFS, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(htmlData)
	for _, want := range []string{
		"codero", "Overview", "Sessions", "Pipeline",
		"Gate", "Chat", "Archives", "Settings",
		"health-bar", "./styles.css", "./js/main.js",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing %q", want)
		}
	}

	// Check JS entry point (now split into modules under js/).
	jsData, err := fs.ReadFile(subFS, "js/main.js")
	if err != nil {
		t.Fatalf("read js/main.js: %v", err)
	}
	js := string(jsData)
	for _, want := range []string{
		"store.js", "router.js", "sse.js",
		"startRouter", "connectSSE",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("js/main.js missing %q", want)
		}
	}

	// Check CSS content.
	cssData, err := fs.ReadFile(subFS, "styles.css")
	if err != nil {
		t.Fatalf("read styles.css: %v", err)
	}
	css := string(cssData)
	for _, want := range []string{
		"--bg-base", "--bg-surface-1", "--glass-bg",
		".status-chip", ".metric-card", ".data-table",
		".glass-card", ".kanban",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("styles.css missing %q", want)
		}
	}
}

func TestStaticFilesServedWithContentTypes(t *testing.T) {
	subFS, err := fs.Sub(dashboard.Static, "static")
	if err != nil {
		t.Fatalf("sub FS: %v", err)
	}
	fileServer := http.FileServer(http.FS(subFS))
	mux := http.NewServeMux()
	basePath := "/dashboard"
	mux.Handle(basePath+"/", http.StripPrefix(basePath, fileServer))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tests := []struct {
		path        string
		wantType    string
		wantContain string
	}{
		{"/dashboard/index.html", "text/html", "./styles.css"},
		{"/dashboard/styles.css", "text/css", "--bg-base"},
		{"/dashboard/js/main.js", "javascript", "startRouter"},
	}
	for _, tt := range tests {
		resp, err := http.Get(srv.URL + tt.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tt.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("GET %s: status %d", tt.path, resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, tt.wantType) {
			t.Errorf("GET %s: Content-Type %q, want substring %q", tt.path, ct, tt.wantType)
		}
		if !bytes.Contains(body, []byte(tt.wantContain)) {
			t.Errorf("GET %s: body missing %q", tt.path, tt.wantContain)
		}
		// For index.html, also verify the module script link.
		if tt.path == "/dashboard/index.html" {
			if !bytes.Contains(body, []byte("./js/main.js")) {
				t.Errorf("GET %s: body missing %q", tt.path, "./js/main.js")
			}
		}
	}
}
