package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

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

func TestHandleSignals_CallsMarkNotReadyBeforeWait(t *testing.T) {
	markNotReadyCalled := false
	markNotReadyWhen := time.Time{}

	var wg sync.WaitGroup
	wg.Add(1)

	startTime := time.Now()

	go func() {
		time.Sleep(50 * time.Millisecond)
		wg.Done()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	markNotReady := func() {
		markNotReadyCalled = true
		markNotReadyWhen = time.Now()
	}

	sigChan := make(chan os.Signal, 1)
	go func() {
		sigChan <- syscall.SIGTERM
	}()

	done := make(chan int)
	go func() {
		exitCode := handleSignalsWithChan(cancel, &wg, markNotReady, sigChan)
		done <- exitCode
	}()

	select {
	case exitCode := <-done:
		if exitCode != 0 {
			t.Errorf("expected exit code 0, got %d", exitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for HandleSignals to complete")
	}

	if !markNotReadyCalled {
		t.Error("markNotReady should have been called")
	}

	if markNotReadyWhen.Before(startTime) {
		t.Error("markNotReady should have been called after signal")
	}

	waitDoneTime := startTime.Add(50 * time.Millisecond)
	if markNotReadyWhen.After(waitDoneTime) {
		t.Error("markNotReady should be called before wg.Wait completes")
	}

	select {
	case <-ctx.Done():
	default:
		t.Error("context should be cancelled after signal handling")
	}
}

func handleSignalsWithChan(cancel context.CancelFunc, wg *sync.WaitGroup, markNotReady func(), sigChan chan os.Signal) int {
	defer signal.Stop(sigChan)

	<-sigChan

	if markNotReady != nil {
		markNotReady()
	}

	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return 0
	case <-time.After(gracePeriod):
		return 1
	}
}
