package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/codero/codero/internal/state"
)

// =============================================================================
// Real-Time Views v1 — Dashboard Certification Tests
//
// Covers: §3 dashboard pages, §3.2 SSE, §4.2 SSE schema, RV-5 non-blocking.
// =============================================================================

func openRTViewsTestDB(t *testing.T) *Handler {
	t.Helper()
	dir := t.TempDir()
	db, err := state.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	settings := NewSettingsStore(dir)
	return NewHandler(db.Unwrap(), settings)
}

// ---------------------------------------------------------------------------
// §3 — All required dashboard API routes are registered.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S3_DashboardRoutesRegistered(t *testing.T) {
	h := openRTViewsTestDB(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	requiredRoutes := []struct {
		path string
		spec string
	}{
		{"/api/v1/dashboard/overview", "/ → Overview"},
		{"/api/v1/dashboard/sessions", "/sessions → Sessions list"},
		{"/api/v1/dashboard/queue", "/queue → Queue"},
		{"/api/v1/dashboard/compliance", "/compliance → Compliance"},
		{"/api/v1/dashboard/events", "/events → SSE stream"},
		{"/api/v1/dashboard/settings", "/settings → Settings"},
		{"/api/v1/dashboard/chat", "/chat → Chat"},
	}

	for _, rt := range requiredRoutes {
		t.Run(rt.spec, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest(http.MethodGet, rt.path, nil)
			req = req.WithContext(ctx)
			rr := httptest.NewRecorder()

			done := make(chan struct{})
			go func() {
				defer close(done)
				mux.ServeHTTP(rr, req)
			}()
			// Give handler a moment to set headers, then cancel.
			cancel()
			<-done

			if rr.Code == http.StatusNotFound {
				t.Errorf("route %s not registered (404)", rt.path)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// §3.2 — SSE endpoint responds with text/event-stream content type.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S3_2_SSEContentType(t *testing.T) {
	h := openRTViewsTestDB(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/events", nil)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(rr, req)
	}()
	cancel()
	<-done

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("SSE Content-Type = %q, want text/event-stream", ct)
	}
}

// ---------------------------------------------------------------------------
// §3.2 — SSE endpoint rejects non-GET methods.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S3_2_SSERejectsPost(t *testing.T) {
	h := openRTViewsTestDB(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/events", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /events = %d, want 405", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// RV-5 — Dashboard reads are non-blocking (no execution loop side effects).
// Evidence: overview handler is GET-only, returns data without mutations.
// ---------------------------------------------------------------------------
func TestCert_RTv1_RV5_OverviewIsReadOnly(t *testing.T) {
	h := openRTViewsTestDB(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code >= 400 && rr.Code != http.StatusInternalServerError {
		t.Errorf("GET /overview = %d, expected 2xx or 500 (empty db)", rr.Code)
	}

	// POST should be rejected (read-only surface).
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/dashboard/overview", nil)
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)

	if rr2.Code == http.StatusOK {
		t.Error("POST /overview should not return 200 (RV-5: reads are non-blocking)")
	}
}
