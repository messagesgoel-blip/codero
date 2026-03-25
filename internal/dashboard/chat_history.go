package dashboard

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// ConversationEntry is a single exchange in a chat conversation.
type ConversationEntry struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	Reply     string    `json:"reply"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
}

// ConversationStore is an in-memory chat history store (LiteLLM Chat v1 §3.3).
// Persistent history is optional and controlled by CODERO_CHAT_PERSIST_HISTORY.
type ConversationStore struct {
	mu      sync.Mutex
	entries []ConversationEntry
	maxLen  int
}

// NewConversationStore creates a store capped at maxLen entries.
func NewConversationStore(maxLen int) *ConversationStore {
	if maxLen <= 0 {
		maxLen = 50
	}
	return &ConversationStore{maxLen: maxLen}
}

// Append adds an entry, evicting the oldest if maxLen is exceeded.
func (s *ConversationStore) Append(e ConversationEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	if len(s.entries) > s.maxLen {
		s.entries = s.entries[len(s.entries)-s.maxLen:]
	}
}

// List returns all entries oldest-first.
func (s *ConversationStore) List() []ConversationEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ConversationEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// Delete removes an entry by ID. Returns true if found.
func (s *ConversationStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return true
		}
	}
	return false
}

// Clear removes all entries.
func (s *ConversationStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = nil
}

// handleChatHistory serves GET/DELETE on /api/v1/chat/history (§2.2, §2.3).
func (h *Handler) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)

	// Check for /api/v1/chat/history/{id} suffix
	const prefix = "/api/v1/chat/history/"
	id := ""
	if strings.HasPrefix(r.URL.Path, prefix) {
		id = strings.TrimPrefix(r.URL.Path, prefix)
	}

	switch r.Method {
	case http.MethodGet:
		if id != "" {
			writeError(w, http.StatusBadRequest, "GET does not accept an ID path segment", "")
			return
		}
		if h.conversations == nil {
			writeJSON(w, http.StatusOK, []ConversationEntry{})
			return
		}
		writeJSON(w, http.StatusOK, h.conversations.List())
	case http.MethodDelete:
		if h.conversations == nil {
			writeError(w, http.StatusNotFound, "conversation not found", "not_found")
			return
		}
		if id == "" {
			h.conversations.Clear()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if h.conversations.Delete(id) {
			w.WriteHeader(http.StatusNoContent)
		} else {
			writeError(w, http.StatusNotFound, "conversation not found", "not_found")
		}
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// setChatHistory ensures the Handler has a conversation store initialised.
func (h *Handler) ensureConversations() {
	if h.conversations == nil {
		h.conversations = NewConversationStore(50)
	}
}
