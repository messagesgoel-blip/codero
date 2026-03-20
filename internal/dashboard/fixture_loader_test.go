package dashboard_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/codero/codero/internal/dashboard"
)

// ── LoadFixtureDir ────────────────────────────────────────────────────────────

func TestLoadFixtureDir_EmptyDir(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	result, err := dashboard.LoadFixtureDir(db, dir)
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
	result, err := dashboard.LoadFixtureDir(db, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReportPath != reportFile {
		t.Errorf("want ReportPath %q, got %q", reportFile, result.ReportPath)
	}
}

func TestLoadFixtureDir_WithSessions(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	sessions := []dashboard.FixtureSessionEntry{
		{SessionID: "sess-1", Repo: "acme/api", Branch: "feat/auth", State: "coding", OwnerAgent: "copilot"},
		{SessionID: "sess-2", Repo: "acme/api", Branch: "feat/db", State: "local_review"},
	}
	writeJSON(t, filepath.Join(dir, "sessions.json"), sessions)

	if _, err := dashboard.LoadFixtureDir(db, dir); err != nil {
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
		{Repo: "acme/api", Branch: "feat/auth", EventType: "state_transition", Payload: `{"from":"queued_cli","to":"coding"}`},
		{Repo: "acme/api", Branch: "feat/auth", EventType: "system", Payload: `{"msg":"fixture event"}`},
	}
	writeJSON(t, filepath.Join(dir, "activity.json"), events)

	if _, err := dashboard.LoadFixtureDir(db, dir); err != nil {
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

	if _, err := dashboard.LoadFixtureDir(db, dir); err == nil {
		t.Fatal("expected error for missing session_id, got nil")
	}
}

func TestLoadFixtureDir_InvalidActivityMalformedJSON(t *testing.T) {
	db := openTestDB(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "activity.json"), []byte(`not json`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := dashboard.LoadFixtureDir(db, dir); err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestSeedFixtureSessions_IdempotentOnReplace(t *testing.T) {
	db := openTestDB(t)
	entries := []dashboard.FixtureSessionEntry{
		{SessionID: "sess-abc", Repo: "acme/api", Branch: "feat/x", State: "coding"},
	}
	// Seeding twice should not error (INSERT OR REPLACE).
	if err := dashboard.SeedFixtureSessions(db, entries); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := dashboard.SeedFixtureSessions(db, entries); err != nil {
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
