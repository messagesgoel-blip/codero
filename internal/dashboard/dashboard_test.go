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

	"github.com/codero/codero/internal/dashboard"
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
	return dashboard.NewHandler(db, store), db
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
	seedBranch(t, db, "acme/api", "feat/x", "coding")

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
	seedBranch(t, db, "acme/api", "main", "coding")
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
	seedDeliveryEvent(t, db, 1, "acme/api", "main", "state_transition", `{"to_state":"reviewed"}`)
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

/* ══════════════════════ STATIC EMBED ═══════════════════════ */

func TestStaticEmbedHasIndexHTML(t *testing.T) {
	subFS, err := fs.Sub(dashboard.Static, "static")
	if err != nil {
		t.Fatalf("sub FS: %v", err)
	}
	f, err := subFS.Open("index.html")
	if err != nil {
		t.Fatalf("index.html not embedded: %v", err)
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	if !bytes.Contains(data, []byte("codero")) {
		t.Error("index.html does not contain expected content")
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
