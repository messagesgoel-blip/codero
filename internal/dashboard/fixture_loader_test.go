package dashboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codero/codero/internal/dashboard"
)

// ── LoadFixtureDir ────────────────────────────────────────────────────────────

func TestLoadFixtureDir_EmptyDir(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	result, err := dashboard.LoadFixtureDir(context.Background(), db, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReportPath != "" {
		t.Errorf("expected empty ReportPath, got %q", result.ReportPath)
	}
}

func TestLoadFixtureDir_WithReportJSON(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	reportFile := filepath.Join(dir, "report.json")
	if err := os.WriteFile(reportFile, []byte(`{"schema_version":"1.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := dashboard.LoadFixtureDir(context.Background(), db, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReportPath != reportFile {
		t.Errorf("want ReportPath %q, got %q", reportFile, result.ReportPath)
	}
	t.Setenv("CODERO_GATE_CHECK_REPORT_PATH", result.ReportPath)
	h := dashboard.NewHandler(db, dashboard.NewSettingsStore(t.TempDir()))
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/gate-checks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"schema_version":"1.0.0"`) {
		t.Fatalf("expected gate-checks body to expose discovered report, got: %s", rec.Body.String())
	}
}

func TestLoadFixtureDir_WithSessions(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	sessions := []dashboard.FixtureSessionEntry{
		{SessionID: "sess-1", AgentID: "copilot", Repo: "acme/api", Branch: "feat/auth", State: "submitted"},
		{SessionID: "sess-2", AgentID: "opencode", Repo: "acme/api", Branch: "feat/db", State: "waiting"},
	}
	writeJSON(t, filepath.Join(dir, "sessions.json"), sessions)

	if _, err := dashboard.LoadFixtureDir(context.Background(), db, dir); err != nil {
		t.Fatalf("LoadFixtureDir: %v", err)
	}

	// Verify via the handler that active-sessions returns both entries.
	h := dashboard.NewHandler(db, dashboard.NewSettingsStore(t.TempDir()))
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/active-sessions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var resp dashboard.ActiveSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ActiveCount != 2 {
		t.Errorf("want 2 active sessions, got %d", resp.ActiveCount)
	}
	found := map[string]bool{}
	for _, s := range resp.Sessions {
		found[s.SessionID] = true
	}
	for _, id := range []string{"sess-1", "sess-2"} {
		if !found[id] {
			t.Errorf("session %q not found in response", id)
		}
	}
}

func TestLoadFixtureDir_WithActivity(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	events := []dashboard.FixtureActivityEntry{
		{Repo: "acme/api", Branch: "feat/auth", EventType: "state_transition", Payload: `{"from":"queued_cli","to":"submitted"}`},
		{Repo: "acme/api", Branch: "feat/auth", EventType: "system", Payload: `{"msg":"fixture event"}`},
	}
	writeJSON(t, filepath.Join(dir, "activity.json"), events)

	if _, err := dashboard.LoadFixtureDir(context.Background(), db, dir); err != nil {
		t.Fatalf("LoadFixtureDir: %v", err)
	}

	h := dashboard.NewHandler(db, dashboard.NewSettingsStore(t.TempDir()))
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/activity", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Events []dashboard.ActivityEvent `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 2 {
		t.Errorf("want 2 events, got %d", len(body.Events))
	}
}

func TestLoadFixtureDir_InvalidSessionsMissingSessionID(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	sessions := []dashboard.FixtureSessionEntry{
		{SessionID: "", Repo: "acme/api", Branch: "feat/x"},
	}
	writeJSON(t, filepath.Join(dir, "sessions.json"), sessions)

	if _, err := dashboard.LoadFixtureDir(context.Background(), db, dir); err == nil {
		t.Fatal("expected error for missing session_id, got nil")
	}
}

func TestLoadFixtureDir_InvalidActivityMalformedJSON(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "activity.json"), []byte(`not json`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := dashboard.LoadFixtureDir(context.Background(), db, dir); err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestSeedFixtureSessions_IdempotentOnReplace(t *testing.T) {
	db := openTestDB(t)
	entries := []dashboard.FixtureSessionEntry{
		{SessionID: "sess-abc", AgentID: "agent-abc", Repo: "acme/api", Branch: "feat/x", State: "submitted"},
	}
	// Seeding twice should not error (INSERT OR REPLACE).
	if err := dashboard.SeedFixtureSessions(context.Background(), db, entries); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := dashboard.SeedFixtureSessions(context.Background(), db, entries); err != nil {
		t.Fatalf("second seed (replace): %v", err)
	}
}

func TestSeedFixtureActivity_SeqIncrement(t *testing.T) {
	db := openTestDB(t)
	events := []dashboard.FixtureActivityEntry{
		{Repo: "r", Branch: "b", EventType: "system", Payload: "{}"},
		{Repo: "r", Branch: "b", EventType: "system", Payload: "{}"},
	}
	if err := dashboard.SeedFixtureActivity(db, events); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Ensure both rows were inserted.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM delivery_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("want 2 delivery_events rows, got %d", count)
	}
}

func TestLoadFixtureDir_WithRuns(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	runs := []dashboard.FixtureRunEntry{
		{ID: "run-1", Repo: "acme/api", Branch: "feat/auth", Provider: "litellm", Status: "completed", StartedAt: "2026-03-26T10:00:00Z", FinishedAt: "2026-03-26T10:15:00Z"},
		{ID: "run-2", Repo: "acme/api", Branch: "feat/db", Provider: "coderabbit", Status: "failed", StartedAt: "2026-03-26T11:00:00Z", FinishedAt: "2026-03-26T11:10:00Z"},
	}
	writeJSON(t, filepath.Join(dir, "runs.json"), runs)

	if _, err := dashboard.LoadFixtureDir(context.Background(), db, dir); err != nil {
		t.Fatalf("LoadFixtureDir: %v", err)
	}

	// Verify via the handler that overview returns runs_today > 0.
	h := dashboard.NewHandler(db, dashboard.NewSettingsStore(t.TempDir()))
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/overview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var resp dashboard.OverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RunsToday != 2 {
		t.Errorf("want 2 runs_today, got %d", resp.RunsToday)
	}
	// Pass rate: 1 completed out of 2 = 50%
	if resp.PassRate < 49.9 || resp.PassRate > 50.1 {
		t.Errorf("want pass_rate ~50.0, got %.2f", resp.PassRate)
	}
}

func TestLoadFixtureDir_InvalidRunsMissingID(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	runs := []dashboard.FixtureRunEntry{
		{ID: "", Repo: "acme/api", Branch: "feat/x", Provider: "litellm", Status: "completed", StartedAt: "2026-03-26T10:00:00Z"},
	}
	writeJSON(t, filepath.Join(dir, "runs.json"), runs)

	if _, err := dashboard.LoadFixtureDir(context.Background(), db, dir); err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
