package webhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codero/codero/internal/webhook"
)

// countProcessor counts how many times ProcessEvent is called.
type countProcessor struct {
	count int
}

func (p *countProcessor) ProcessEvent(_ context.Context, _ webhook.GitHubEvent) error {
	p.count++
	return nil
}

func TestAppHandler_ValidHMAC(t *testing.T) {
	db, client := setupWebhookDeps(t)
	secret := "test-secret"
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewAppHandler(secret, dedup, &webhook.NopProcessor{})

	body := makePayload(t, "owner/repo")
	sig := signPayload(secret, body)
	req := newTestRequest(t, body, "app-delivery-001", "pull_request", sig)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}
}

func TestAppHandler_InvalidHMAC(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewAppHandler("real-secret", dedup, &webhook.NopProcessor{})

	body := makePayload(t, "owner/repo")
	req := newTestRequest(t, body, "app-delivery-002", "push", "sha256=deadbeef")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// go-githubapp returns 400 for bad HMAC via DefaultErrorCallback
	if rr.Code == http.StatusOK {
		t.Errorf("expected non-200 for invalid HMAC, got %d", rr.Code)
	}
}

func TestAppHandler_Dedup(t *testing.T) {
	db, client := setupWebhookDeps(t)
	secret := "test-secret"
	dedup := webhook.NewDeduplicator(db, client)
	counter := &countProcessor{}
	handler := webhook.NewAppHandler(secret, dedup, counter)

	body := makePayload(t, "owner/repo")
	sig := signPayload(secret, body)

	// First request
	req1 := newTestRequest(t, body, "app-delivery-003", "push", sig)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	// Second request (duplicate)
	req2 := newTestRequest(t, body, "app-delivery-003", "push", sig)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	// Processor should have been called only once
	if counter.count != 1 {
		t.Errorf("processor called %d times, want 1 (dedup should prevent second)", counter.count)
	}
}

func TestUseGitHubApp_Default(t *testing.T) {
	t.Setenv("CODERO_WEBHOOK_USE_GITHUBAPP", "")
	if !webhook.UseGitHubApp() {
		t.Error("expected default true")
	}
}

func TestUseGitHubApp_Disabled(t *testing.T) {
	t.Setenv("CODERO_WEBHOOK_USE_GITHUBAPP", "false")
	if webhook.UseGitHubApp() {
		t.Error("expected false when disabled")
	}
}
