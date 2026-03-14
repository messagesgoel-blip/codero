// Package delivery implements the append-only feedback stream with monotonic
// sequence IDs. Redis INCR provides coordination; SQLite is the durable source.
//
// Seq numbers are monotonic but not necessarily contiguous: a crash between
// INCR and INSERT leaves a harmless gap. Replay consumers must tolerate gaps.
package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codero/codero/internal/normalizer"
	"github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/state"
	goredis "github.com/redis/go-redis/v9"
)

// EventType identifies what a delivery event represents.
type EventType string

const (
	EventTypeFindingBundle   EventType = "finding_bundle"
	EventTypeSystem          EventType = "system"
	EventTypeStateTransition EventType = "state_transition"
)

// FindingBundlePayload is the JSON payload for a finding_bundle event.
type FindingBundlePayload struct {
	RunID    string               `json:"run_id"`
	Provider string               `json:"provider"`
	Findings []normalizer.Finding `json:"findings"`
}

// SystemPayload is the JSON payload for a system event (e.g., lease expiry).
type SystemPayload struct {
	Reason  string `json:"reason"`
	Details string `json:"details,omitempty"`
}

// StateTransitionPayload is the JSON payload for a state_transition event.
type StateTransitionPayload struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Trigger string `json:"trigger"`
}

// Stream is the append-only delivery stream for a repo.
// Redis INCR assigns monotonic seq; SQLite persists events durably.
type Stream struct {
	db     *state.DB
	client *redis.Client
}

// NewStream creates a delivery Stream.
func NewStream(db *state.DB, client *redis.Client) *Stream {
	return &Stream{db: db, client: client}
}

// InitSeqFloor ensures the Redis seq counter is at least as large as the
// durable floor for a repo+branch. Must be called on daemon startup for each
// tracked branch to prevent seq regression after Redis restart.
func (s *Stream) InitSeqFloor(ctx context.Context, repo, branch string) error {
	floor, err := state.GetDeliverySeqFloor(s.db, repo, branch)
	if err != nil {
		return fmt.Errorf("delivery: init seq floor: %w", err)
	}
	if floor == 0 {
		return nil // no events yet; Redis will start from 1
	}

	key, err := seqKey(repo, branch)
	if err != nil {
		return err
	}

	rc := s.client.Unwrap()
	// Use a Lua script to set the counter only if it is below the durable floor.
	script := `
		local cur = redis.call('GET', KEYS[1])
		if cur == false or tonumber(cur) < tonumber(ARGV[1]) then
			redis.call('SET', KEYS[1], ARGV[1])
		end
		return redis.call('GET', KEYS[1])
	`
	if _, err := rc.Eval(ctx, script, []string{key}, floor).Result(); err != nil {
		return fmt.Errorf("delivery: init seq floor: redis: %w", err)
	}
	return nil
}

// Append assigns a monotonic seq number via Redis INCR and writes the event
// durably to SQLite. If Redis is temporarily unavailable, the append fails; the
// caller (runner, reconciler) should retry or log and continue—never silently
// drop.
func (s *Stream) Append(ctx context.Context, repo, branch, headHash string, evType EventType, payload any) (int64, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("delivery: marshal payload: %w", err)
	}

	// Step 1: increment seq counter in Redis.
	key, err := seqKey(repo, branch)
	if err != nil {
		return 0, err
	}
	rc := s.client.Unwrap()
	seq, err := rc.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("delivery: incr seq: redis: %w", err)
	}

	// Step 2: write durably to SQLite.
	ev := state.DeliveryEvent{
		Seq:       seq,
		Repo:      repo,
		Branch:    branch,
		HeadHash:  headHash,
		EventType: string(evType),
		Payload:   string(payloadJSON),
		CreatedAt: time.Now().UTC(),
	}
	if err := state.AppendDeliveryEvent(s.db, ev); err != nil {
		// Seq is already claimed—log the gap but don't fail the caller invisibly.
		return seq, fmt.Errorf("delivery: append to db (seq %d claimed, gap created): %w", seq, err)
	}

	return seq, nil
}

// AppendFindingBundle writes a finding_bundle event to the stream.
func (s *Stream) AppendFindingBundle(ctx context.Context, repo, branch, headHash string, payload FindingBundlePayload) (int64, error) {
	return s.Append(ctx, repo, branch, headHash, EventTypeFindingBundle, payload)
}

// AppendSystem writes a system event (lease expiry, retry, etc.) to the stream.
func (s *Stream) AppendSystem(ctx context.Context, repo, branch, headHash, reason, details string) (int64, error) {
	return s.Append(ctx, repo, branch, headHash, EventTypeSystem, SystemPayload{
		Reason:  reason,
		Details: details,
	})
}

// AppendStateTransition writes a state_transition event to the stream.
func (s *Stream) AppendStateTransition(ctx context.Context, repo, branch, headHash, from, to, trigger string) (int64, error) {
	return s.Append(ctx, repo, branch, headHash, EventTypeStateTransition, StateTransitionPayload{
		From:    from,
		To:      to,
		Trigger: trigger,
	})
}

// Replay returns all delivery events for repo+branch with seq > sinceSeq.
// The result is ordered by seq ascending. Idempotent: repeated calls with the
// same sinceSeq return the same events (append-only source).
// sinceSeq=0 returns all events.
func (s *Stream) Replay(ctx context.Context, repo, branch string, sinceSeq int64) ([]state.DeliveryEvent, error) {
	events, err := state.ListDeliveryEvents(s.db, repo, branch, sinceSeq)
	if err != nil {
		return nil, fmt.Errorf("delivery: replay: %w", err)
	}
	return events, nil
}

// CurrentSeq returns the current seq counter for a repo+branch from Redis.
// Falls back to the durable floor if Redis is unavailable.
func (s *Stream) CurrentSeq(ctx context.Context, repo, branch string) (int64, error) {
	key, err := seqKey(repo, branch)
	if err != nil {
		return 0, err
	}
	rc := s.client.Unwrap()
	val, err := rc.Get(ctx, key).Int64()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return state.GetDeliverySeqFloor(s.db, repo, branch)
		}
		// Redis unavailable—fall back to durable floor.
		floor, dbErr := state.GetDeliverySeqFloor(s.db, repo, branch)
		if dbErr != nil {
			return 0, fmt.Errorf("delivery: current seq: redis and db both failed: redis=%w db=%w", err, dbErr)
		}
		return floor, nil
	}
	return val, nil
}

// seqKey builds the Redis key for the seq counter.
// Format: codero:<repo>:seq:<branch> — uses branch as id since it's per-branch.
// For a global repo-level counter, we use a synthetic branch name.
func seqKey(repo, branch string) (string, error) {
	// Delivery seq is per-repo+branch. Redis key: codero:<repo>:seq:<branch>.
	// The branch may contain '/' (e.g. feature/foo) which is allowed in Redis keys.
	// redis.BuildKey validates only for ':' not '/'.
	k, err := redis.BuildKey(repo, "seq", encodeBranch(branch))
	if err != nil {
		return "", fmt.Errorf("delivery: build seq key: %w", err)
	}
	return k, nil
}

// encodeBranch replaces '/' in branch names with '_' to keep the Redis key
// safe. Branches with the same name after encoding are extremely rare in
// practice; collisions would result in a shared counter (seq still monotonic).
func encodeBranch(branch string) string {
	if branch == "" {
		return "default"
	}
	result := make([]byte, len(branch))
	for i := 0; i < len(branch); i++ {
		if branch[i] == '/' {
			result[i] = '_'
		} else {
			result[i] = branch[i]
		}
	}
	// Ensure no colons (Redis key part constraint).
	for i := 0; i < len(result); i++ {
		if result[i] == ':' {
			result[i] = '_'
		}
	}
	return string(result)
}
