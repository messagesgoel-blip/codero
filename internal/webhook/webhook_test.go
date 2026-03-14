package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/webhook"
)

func setupWebhookDeps(t *testing.T) (*state.DB, *redislib.Client) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	client := redislib.New(mr.Addr(), "")
	t.Cleanup(func() { _ = client.Close() })

	return db, client
}

func makePayload(t *testing.T, repo string) []byte {
	t.Helper()
	payload := map[string]any{
		"repository": map[string]any{
			"full_name": repo,
		},
		"action": "opened",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}

func signPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newTestRequest(t *testing.T, body []byte, deliveryID, eventType, sig string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	req.Header.Set("X-GitHub-Event", eventType)
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	return req
}

func TestHandler_ValidWebhook(t *testing.T) {
	db, client := setupWebhookDeps(t)
	secret := "test-secret"
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewHandler(secret, dedup, &webhook.NopProcessor{})

	body := makePayload(t, "owner/repo")
	sig := signPayload(secret, body)
	req := newTestRequest(t, body, "delivery-001", "pull_request", sig)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandler_InvalidSignature(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewHandler("real-secret", dedup, &webhook.NopProcessor{})

	body := makePayload(t, "owner/repo")
	req := newTestRequest(t, body, "delivery-002", "push", "sha256=deadbeef")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d (invalid signature)", rr.Code, http.StatusUnauthorized)
	}
}

func TestHandler_DuplicateDelivery_Redis(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewHandler("", dedup, &webhook.NopProcessor{})

	body := makePayload(t, "owner/repo")
	deliveryID := "delivery-003"

	// First request.
	req1 := newTestRequest(t, body, deliveryID, "push", "")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", rr1.Code)
	}

	// Second request with same delivery ID → duplicate.
	req2 := newTestRequest(t, body, deliveryID, "push", "")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("duplicate: got %d, want 200 (duplicates accepted gracefully)", rr2.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "duplicate" {
		t.Errorf("response status: got %q, want %q", resp["status"], "duplicate")
	}
}

func TestHandler_DuplicateDelivery_DBFallback(t *testing.T) {
	db, client := setupWebhookDeps(t)

	// Pre-seed the DB with a processed delivery.
	inserted, err := state.MarkWebhookDelivery(db, "delivery-004", "push", "owner/repo")
	if err != nil {
		t.Fatalf("seed db: %v", err)
	}
	if !inserted {
		t.Fatal("expected new insert")
	}

	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewHandler("", dedup, &webhook.NopProcessor{})

	body := makePayload(t, "owner/repo")
	req := newTestRequest(t, body, "delivery-004", "push", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rr.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "duplicate" {
		t.Errorf("response status: got %q, want %q", resp["status"], "duplicate")
	}
}

func TestHandler_MissingDeliveryID(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewHandler("", dedup, &webhook.NopProcessor{})

	body := makePayload(t, "owner/repo")
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	// No X-GitHub-Delivery header.

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400 (missing delivery ID)", rr.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewHandler("", dedup, &webhook.NopProcessor{})

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", rr.Code)
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	handler := webhook.NewHandler("", dedup, &webhook.NopProcessor{})

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte("not-json")))
	req.Header.Set("X-GitHub-Delivery", "del-005")
	req.Header.Set("X-GitHub-Event", "push")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400 (invalid JSON)", rr.Code)
	}
}

func TestHandler_ProcessorError(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	failProc := &failProcessor{}
	handler := webhook.NewHandler("", dedup, failProc)

	body := makePayload(t, "owner/repo")
	req := newTestRequest(t, body, "delivery-006", "push", "")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500 (processor error)", rr.Code)
	}
}

func TestDeduplicator_CheckIdempotent(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	ctx := context.Background()

	// First check: should succeed.
	if err := dedup.Check(ctx, "del-007", "push", "owner/repo"); err != nil {
		t.Fatalf("first check: %v", err)
	}

	// Second check: should return ErrDuplicate.
	err := dedup.Check(ctx, "del-007", "push", "owner/repo")
	if err == nil {
		t.Error("expected ErrDuplicate on second check, got nil")
	}
}

func TestDeduplicator_UniqueDeliveries(t *testing.T) {
	db, client := setupWebhookDeps(t)
	dedup := webhook.NewDeduplicator(db, client)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("delivery-%d", i)
		if err := dedup.Check(ctx, id, "push", "owner/repo"); err != nil {
			t.Errorf("delivery %q: unexpected error: %v", id, err)
		}
	}
}

// failProcessor always returns an error.
type failProcessor struct{}

func (f *failProcessor) ProcessEvent(_ context.Context, _ webhook.GitHubEvent) error {
	return fmt.Errorf("processor always fails")
}
