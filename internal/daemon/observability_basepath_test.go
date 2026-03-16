package daemon

// observability_basepath_test.go — tests for COD-026 web port/routing hardening.
// Verifies that NewObservabilityServer correctly normalises the dashboard base path
// and serves the SPA and redirect under the configured path.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/scheduler"
)

// newTestServer builds a minimal ObservabilityServer backed by nil DB and a stub
// Redis client, then wraps the mux in an httptest.Server for request testing.
// Only static-file and routing behaviour is verified; endpoints that require DB
// or Redis are not exercised here.
func newTestMux(t *testing.T, basePath string) http.Handler {
	t.Helper()
	// Use a stub redis client (no real Redis needed for routing tests).
	client := redis.New("127.0.0.1:0", "")
	queue := scheduler.NewQueue(client)
	slotCounter := scheduler.NewSlotCounter(client)

	obs := NewObservabilityServer(client, queue, slotCounter, nil, "127.0.0.1", "0", basePath, "test")
	return obs.server.Handler
}

func TestObservabilityServer_DefaultBasePath(t *testing.T) {
	mux := newTestMux(t, "")

	// Bare /dashboard should redirect to /dashboard/.
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("bare /dashboard: got %d, want 301", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasSuffix(loc, "/dashboard/") {
		t.Errorf("redirect Location: got %q, want suffix /dashboard/", loc)
	}
}

func TestObservabilityServer_DefaultBasePath_IndexServed(t *testing.T) {
	mux := newTestMux(t, "")

	req := httptest.NewRequest(http.MethodGet, "/dashboard/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// The embedded index.html should be served (200 or at worst 404 for sub-path
	// in file server, but 200 is expected for the root).
	if rec.Code != http.StatusOK {
		t.Errorf("/dashboard/: got %d, want 200", rec.Code)
	}
}

func TestObservabilityServer_CustomBasePath(t *testing.T) {
	mux := newTestMux(t, "/codero/ui")

	// Bare /codero/ui should redirect to /codero/ui/.
	req := httptest.NewRequest(http.MethodGet, "/codero/ui", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("/codero/ui redirect: got %d, want 301", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasSuffix(loc, "/codero/ui/") {
		t.Errorf("redirect Location: got %q, want suffix /codero/ui/", loc)
	}
}

func TestObservabilityServer_CustomBasePath_IndexServed(t *testing.T) {
	mux := newTestMux(t, "/codero/ui")

	req := httptest.NewRequest(http.MethodGet, "/codero/ui/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/codero/ui/: got %d, want 200", rec.Code)
	}
}

func TestObservabilityServer_CustomBasePath_OldPathNotFound(t *testing.T) {
	mux := newTestMux(t, "/codero/ui")

	// /dashboard should no longer be registered when base path is /codero/ui.
	req := httptest.NewRequest(http.MethodGet, "/dashboard/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("/dashboard/ should not be served when base path is /codero/ui")
	}
}

func TestObservabilityServer_BindAddress(t *testing.T) {
	client := redis.New("127.0.0.1:0", "")
	queue := scheduler.NewQueue(client)
	slotCounter := scheduler.NewSlotCounter(client)

	obs := NewObservabilityServer(client, queue, slotCounter, nil, "127.0.0.1", "9876", "/dashboard", "test")
	if obs.server.Addr != "127.0.0.1:9876" {
		t.Errorf("server.Addr: got %q, want 127.0.0.1:9876", obs.server.Addr)
	}
}

func TestObservabilityServer_AllInterfacesBind(t *testing.T) {
	client := redis.New("127.0.0.1:0", "")
	queue := scheduler.NewQueue(client)
	slotCounter := scheduler.NewSlotCounter(client)

	// Empty host → binds all interfaces.
	obs := NewObservabilityServer(client, queue, slotCounter, nil, "", "8080", "/dashboard", "test")
	if obs.server.Addr != ":8080" {
		t.Errorf("server.Addr: got %q, want :8080", obs.server.Addr)
	}
}
