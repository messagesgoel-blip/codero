package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/palantir/go-githubapp/githubapp"
)

// UseGitHubApp returns true if the CODERO_WEBHOOK_USE_GITHUBAPP flag is "true"
// or unset (default is true).
func UseGitHubApp() bool {
	v := os.Getenv("CODERO_WEBHOOK_USE_GITHUBAPP")
	return v == "" || v == "true" || v == "1"
}

// AppHandler wraps go-githubapp's event dispatcher while preserving
// Codero's Redis dedup and processor pipeline.
type AppHandler struct {
	dispatcher http.Handler
}

// NewAppHandler creates a webhook handler backed by go-githubapp.
// HMAC validation is handled by go-githubapp; dedup and processing
// use the existing Codero pipeline.
func NewAppHandler(secret string, dedup *Deduplicator, proc Processor) *AppHandler {
	bridge := &eventBridge{dedup: dedup, proc: proc}
	dispatcher := githubapp.NewEventDispatcher(
		[]githubapp.EventHandler{bridge},
		secret,
	)
	return &AppHandler{dispatcher: dispatcher}
}

func (h *AppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.dispatcher.ServeHTTP(w, r)
}

// eventBridge implements githubapp.EventHandler, bridging go-githubapp's
// dispatch to Codero's existing dedup + processor pipeline.
type eventBridge struct {
	dedup *Deduplicator
	proc  Processor
}

// Handles returns all GitHub event types we process.
func (b *eventBridge) Handles() []string {
	return []string{
		"pull_request",
		"pull_request_review",
		"check_run",
		"push",
		"create",
		"delete",
	}
}

// Handle is called by go-githubapp after HMAC validation passes.
func (b *eventBridge) Handle(ctx context.Context, eventType, deliveryID string, payload []byte) error {
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return err
	}

	repo := extractRepo(payloadMap)

	// Deduplication: same Redis hot path + durable DB backstop.
	if err := b.dedup.Check(ctx, deliveryID, eventType, repo); err != nil {
		if errors.Is(err, ErrDuplicate) {
			return nil // silently drop duplicates
		}
		return err
	}

	ev := GitHubEvent{
		DeliveryID: deliveryID,
		EventType:  eventType,
		Repo:       repo,
		Payload:    payloadMap,
	}
	return b.proc.ProcessEvent(ctx, ev)
}

// NewWebhookHandler returns the appropriate webhook handler based on
// the CODERO_WEBHOOK_USE_GITHUBAPP feature flag.
func NewWebhookHandler(secret string, dedup *Deduplicator, proc Processor) http.Handler {
	if UseGitHubApp() {
		return NewAppHandler(secret, dedup, proc)
	}
	return NewHandler(secret, dedup, proc)
}
