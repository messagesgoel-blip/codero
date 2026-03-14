// Package webhook handles GitHub webhook ingestion, signature verification,
// deduplication, and state reconciliation.
package webhook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/state"
	goredis "github.com/redis/go-redis/v9"
)

const (
	// dedupTTL is how long a webhook delivery ID is remembered in Redis.
	// 86400s = 24 hours, per roadmap Appendix B.
	dedupTTL = 86400 * time.Second
)

// Deduplicator deduplicates webhook deliveries using a two-layer approach:
//  1. Redis SET NX EX hot path (fast, ephemeral).
//  2. Durable DB secondary check (correctness backstop).
//
// Redis loss does not cause durable state corruption because the DB layer
// always runs after a Redis miss.
type Deduplicator struct {
	db     *state.DB
	client *redis.Client
}

// NewDeduplicator creates a Deduplicator.
func NewDeduplicator(db *state.DB, client *redis.Client) *Deduplicator {
	return &Deduplicator{db: db, client: client}
}

// ErrDuplicate is returned when a delivery is a known duplicate.
var ErrDuplicate = errors.New("duplicate webhook delivery")

// Check returns ErrDuplicate if deliveryID has already been seen.
// On the first sighting: records it in both Redis and the durable DB.
// Thread-safe; Redis NX guarantees atomicity for concurrent processes.
func (d *Deduplicator) Check(ctx context.Context, deliveryID, eventType, repo string) error {
	if deliveryID == "" {
		return fmt.Errorf("dedup: delivery_id must not be empty")
	}

	// Step 1: Redis hot path.
	key, err := dedupKey(repo, deliveryID)
	if err != nil {
		// Key build failure (invalid repo/id chars) — fall through to DB only.
		// Log is omitted here; callers log the error.
	} else {
		rc := d.client.Unwrap()
		set, redisErr := rc.SetNX(ctx, key, "1", dedupTTL).Result()
		if redisErr != nil && !errors.Is(redisErr, goredis.Nil) {
			// Redis unavailable: fall through to DB secondary check.
			// Per roadmap: "Loss of Redis dedup cannot cause durable corruption."
		} else if !set {
			// Key already exists in Redis → duplicate fast path.
			return ErrDuplicate
		}
	}

	// Step 2: Durable DB secondary idempotency check + insert.
	inserted, err := state.MarkWebhookDelivery(d.db, deliveryID, eventType, repo)
	if err != nil {
		return fmt.Errorf("dedup: db mark: %w", err)
	}
	if !inserted {
		return ErrDuplicate
	}

	return nil
}

// IsKnown returns true if a delivery ID has been processed (DB check only).
// Used for diagnostic purposes; normal dedup path uses Check.
func (d *Deduplicator) IsKnown(ctx context.Context, deliveryID string) (bool, error) {
	return state.IsWebhookProcessed(d.db, deliveryID)
}

// dedupKey builds the Redis key for webhook dedup.
// Format: codero:<repo>:webhook:<deliveryID>
func dedupKey(repo, deliveryID string) (string, error) {
	// deliveryID is a GitHub UUID like "72d3162e-cc78-11e3-81ab-4c9367dc0958".
	// Hyphens are fine in Redis keys; colons are not (redis.BuildKey validates).
	// We use a sanitized ID to avoid key construction errors.
	safe := sanitizeForKey(deliveryID)
	return redis.BuildKey(repo, "webhook", safe)
}

// sanitizeForKey replaces colon characters in a string with underscores so it
// can be safely embedded in a Redis key.
func sanitizeForKey(s string) string {
	if s == "" {
		return "empty"
	}
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			result[i] = '_'
		} else {
			result[i] = s[i]
		}
	}
	return string(result)
}
