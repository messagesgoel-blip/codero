package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	loglib "github.com/codero/codero/internal/log"
)

const (
	// maxBodySize is the maximum webhook body size accepted (10 MB).
	maxBodySize = 10 * 1024 * 1024

	// responseTimeout is the maximum time a webhook handler may take.
	// Appendix C.5: "webhook receiver must return 2XX within 10 seconds."
	responseTimeout = 10 * time.Second
)

// GitHubEvent is a parsed GitHub webhook event.
type GitHubEvent struct {
	DeliveryID string
	EventType  string
	Repo       string
	Payload    map[string]any
}

// Handler is an HTTP handler for GitHub webhook events. It performs:
//  1. HMAC-SHA256 signature verification.
//  2. Delivery deduplication via Deduplicator.
//  3. Event dispatch to the Processor.
type Handler struct {
	secret string
	dedup  *Deduplicator
	proc   Processor
}

// Processor handles a parsed, deduplicated webhook event.
type Processor interface {
	ProcessEvent(ctx context.Context, ev GitHubEvent) error
}

// NewHandler creates a Handler. secret is the webhook HMAC secret (may be empty
// to disable signature verification, e.g., in polling-only mode testing).
func NewHandler(secret string, dedup *Deduplicator, proc Processor) *Handler {
	return &Handler{
		secret: secret,
		dedup:  dedup,
		proc:   proc,
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), responseTimeout)
	defer cancel()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body with size limit.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if len(body) > maxBodySize {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Signature verification (skip if no secret configured).
	if h.secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if err := verifySignature(h.secret, body, sig); err != nil {
			loglib.Warn("webhook: signature verification failed",
				loglib.FieldComponent, "webhook",
				"error", err,
			)
			http.Error(w, "signature verification failed", http.StatusUnauthorized)
			return
		}
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	eventType := r.Header.Get("X-GitHub-Event")

	if deliveryID == "" {
		http.Error(w, "missing X-GitHub-Delivery header", http.StatusBadRequest)
		return
	}
	if eventType == "" {
		http.Error(w, "missing X-GitHub-Event header", http.StatusBadRequest)
		return
	}

	// Parse payload for repo extraction.
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	repo := extractRepo(payload)

	// Deduplication: Redis hot path + durable DB backstop.
	if err := h.dedup.Check(ctx, deliveryID, eventType, repo); err != nil {
		if errors.Is(err, ErrDuplicate) {
			loglib.Info("webhook: duplicate delivery dropped",
				loglib.FieldComponent, "webhook",
				"delivery_id", deliveryID,
				"event_type", eventType,
			)
			// Return 200 to prevent GitHub from retrying a known duplicate.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"duplicate"}`))
			return
		}
		loglib.Error("webhook: dedup check failed",
			loglib.FieldComponent, "webhook",
			"delivery_id", deliveryID,
			"error", err,
		)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	ev := GitHubEvent{
		DeliveryID: deliveryID,
		EventType:  eventType,
		Repo:       repo,
		Payload:    payload,
	}

	// Dispatch to processor.
	if err := h.proc.ProcessEvent(ctx, ev); err != nil {
		loglib.Error("webhook: event processing failed",
			loglib.FieldComponent, "webhook",
			"delivery_id", deliveryID,
			"event_type", eventType,
			"error", err,
		)
		// Return 500 to allow GitHub to retry non-duplicate failures.
		http.Error(w, "processing error", http.StatusInternalServerError)
		return
	}

	loglib.Info("webhook: event processed",
		loglib.FieldComponent, "webhook",
		"delivery_id", deliveryID,
		"event_type", eventType,
		loglib.FieldRepo, repo,
	)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// verifySignature checks the HMAC-SHA256 signature on the webhook body.
// GitHub sends the signature in the format: "sha256=<hex>".
func verifySignature(secret string, body []byte, sig string) error {
	if !strings.HasPrefix(sig, "sha256=") {
		return fmt.Errorf("signature missing sha256= prefix")
	}
	gotHex := strings.TrimPrefix(sig, "sha256=")
	got, err := hex.DecodeString(gotHex)
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(got, expected) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// extractRepo extracts "owner/repo" from a GitHub webhook payload.
// GitHub embeds this in payload.repository.full_name.
func extractRepo(payload map[string]any) string {
	if repo, ok := payload["repository"]; ok {
		if repoMap, ok := repo.(map[string]any); ok {
			if fullName, ok := repoMap["full_name"].(string); ok {
				return fullName
			}
		}
	}
	return ""
}

// Server wraps an http.Server for the webhook receiver.
type Server struct {
	srv *http.Server
}

// NewServer creates a webhook HTTP server on the given address.
func NewServer(addr string, handler http.Handler) *Server {
	mux := http.NewServeMux()
	mux.Handle("/webhook", handler)
	mux.Handle("/webhook/github", handler)

	return &Server{
		srv: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}
}

// Start begins listening for webhook events. It blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}
