package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/scheduler"
)

// newTestObsServer builds a minimal ObservabilityServer for readiness tests.
func newTestObsServer(t *testing.T) *ObservabilityServer {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.New(mr.Addr(), "")
	queue := scheduler.NewQueue(client)
	slotCounter := scheduler.NewSlotCounter(client)
	return NewObservabilityServer(client, queue, slotCounter, nil, "127.0.0.1", "0", "", "test")
}

func TestReady_NotReadyBeforeMarkReady(t *testing.T) {
	obs := newTestObsServer(t)
	// Reset degraded state for test isolation.
	SetDegraded(false)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	obs.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/ready before MarkReady: got %d, want 503", rec.Code)
	}
}

func TestReady_OKAfterMarkReady(t *testing.T) {
	obs := newTestObsServer(t)
	SetDegraded(false)
	obs.MarkReady()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	obs.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/ready after MarkReady: got %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Errorf("/ready body: got %q, want %q", body, "ok")
	}
}

func TestReady_DegradedAfterMarkReady(t *testing.T) {
	obs := newTestObsServer(t)
	obs.MarkReady()
	SetDegraded(true)
	defer SetDegraded(false)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	obs.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/ready degraded: got %d, want 200 (degraded but ready)", rec.Code)
	}
	if body := rec.Body.String(); body != "degraded" {
		t.Errorf("/ready body: got %q, want %q", body, "degraded")
	}
}

func TestReady_503AfterMarkNotReady(t *testing.T) {
	obs := newTestObsServer(t)
	SetDegraded(false)
	obs.MarkReady()
	obs.MarkNotReady() // Simulate shutdown phase.

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	obs.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/ready after MarkNotReady: got %d, want 503", rec.Code)
	}
}

func TestHealth_IncludesReadyField(t *testing.T) {
	obs := newTestObsServer(t)
	SetDegraded(false)

	// Before MarkReady: ready should be false.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	obs.server.Handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Errorf("/health status: got %d, want 200", rec.Code)
	}
	// JSON should contain "ready":false before MarkReady.
	if !contains(body, `"ready":false`) {
		t.Errorf("/health body should contain ready:false; got: %s", body)
	}

	// After MarkReady: ready should be true.
	obs.MarkReady()
	rec2 := httptest.NewRecorder()
	obs.server.Handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/health", nil))
	body2 := rec2.Body.String()
	if !contains(body2, `"ready":true`) {
		t.Errorf("/health body after MarkReady should contain ready:true; got: %s", body2)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) >= len(substr) && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
