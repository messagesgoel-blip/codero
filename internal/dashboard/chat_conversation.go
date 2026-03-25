package dashboard

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// ConversationMessage is one turn in a conversation.
type ConversationMessage struct {
	Role      string    `json:"role"`      // "user" or "assistant"
	Content   string    `json:"content"`   // message text
	Timestamp time.Time `json:"timestamp"` // when the message was added
}

// Conversation is a single multi-turn chat thread.
type Conversation struct {
	ID        string                `json:"conversation_id"`
	Messages  []ConversationMessage `json:"messages"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
}

// ConversationStore is a thread-safe in-memory store for chat conversations
// with TTL expiry and per-conversation message caps.
type ConversationStore struct {
	mu          sync.Mutex
	convos      map[string]*Conversation
	maxHistory  int
	ttlSeconds  int
	lastCleanup time.Time
}

// NewConversationStore creates a store with the given max messages per
// conversation and TTL in seconds.
func NewConversationStore(maxHistory, ttlSeconds int) *ConversationStore {
	if maxHistory <= 0 {
		maxHistory = 50
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	return &ConversationStore{
		convos:      make(map[string]*Conversation),
		maxHistory:  maxHistory,
		ttlSeconds:  ttlSeconds,
		lastCleanup: time.Now(),
	}
}

// GetOrCreate returns the conversation for the given ID, creating it if absent.
// If id is empty, a new conversation is created with a generated ID.
func (s *ConversationStore) GetOrCreate(id string) *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()

	if id == "" {
		id = uuid.New().String()
	}
	c, ok := s.convos[id]
	if !ok {
		c = &Conversation{
			ID:        id,
			Messages:  nil,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		s.convos[id] = c
	}
	return c
}

// Append adds a message to the conversation, enforcing the max-history cap.
func (s *ConversationStore) Append(id, role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.convos[id]
	if !ok {
		return
	}
	c.Messages = append(c.Messages, ConversationMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UTC(),
	})
	if len(c.Messages) > s.maxHistory {
		c.Messages = c.Messages[len(c.Messages)-s.maxHistory:]
	}
	c.UpdatedAt = time.Now().UTC()
}

// Get returns the conversation for the given ID, or nil if not found.
func (s *ConversationStore) Get(id string) *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	return s.convos[id]
}

// Delete removes a conversation by ID. Returns true if it existed.
func (s *ConversationStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.convos[id]
	delete(s.convos, id)
	return ok
}

// List returns all non-expired conversations sorted by last update (newest first).
func (s *ConversationStore) List() []Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()

	out := make([]Conversation, 0, len(s.convos))
	for _, c := range s.convos {
		out = append(out, *c)
	}
	return out
}

// History returns message history for a conversation with optional limit/offset.
func (s *ConversationStore) History(id string, limit, offset int) []ConversationMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()

	c, ok := s.convos[id]
	if !ok {
		return nil
	}
	msgs := c.Messages
	if offset > 0 {
		if offset >= len(msgs) {
			return nil
		}
		msgs = msgs[offset:]
	}
	if limit > 0 && limit < len(msgs) {
		msgs = msgs[:limit]
	}
	return msgs
}

// Len returns the number of active conversations.
func (s *ConversationStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.convos)
}

// expireLocked removes conversations older than TTL. Must be called with lock held.
func (s *ConversationStore) expireLocked() {
	now := time.Now()
	if now.Sub(s.lastCleanup) < 30*time.Second {
		return
	}
	s.lastCleanup = now
	cutoff := now.Add(-time.Duration(s.ttlSeconds) * time.Second)
	for id, c := range s.convos {
		if c.UpdatedAt.Before(cutoff) {
			delete(s.convos, id)
		}
	}
}
