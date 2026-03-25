package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// handleChatHistory serves GET /api/v1/chat/history.
// Returns conversation history filtered by optional conversation_id, limit, offset.
func (h *Handler) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	switch r.Method {
	case http.MethodGet:
		h.getChatHistory(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (h *Handler) getChatHistory(w http.ResponseWriter, r *http.Request) {
	convID := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			offset = n
		}
	}

	if convID == "" {
		// Return list of all conversations
		convos := h.convos.List()
		type convoSummary struct {
			ConversationID string `json:"conversation_id"`
			MessageCount   int    `json:"message_count"`
			CreatedAt      string `json:"created_at"`
			UpdatedAt      string `json:"updated_at"`
		}
		summaries := make([]convoSummary, 0, len(convos))
		for _, c := range convos {
			summaries = append(summaries, convoSummary{
				ConversationID: c.ID,
				MessageCount:   len(c.Messages),
				CreatedAt:      c.CreatedAt.Format("2006-01-02T15:04:05Z"),
				UpdatedAt:      c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"conversations": summaries,
			"count":         len(summaries),
		})
		return
	}

	msgs := h.convos.History(convID, limit, offset)
	if msgs == nil {
		writeError(w, http.StatusNotFound, "conversation not found", "not_found")
		return
	}

	entries := make([]ChatHistoryEntry, 0, len(msgs))
	for _, m := range msgs {
		entries = append(entries, ChatHistoryEntry{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		})
	}
	writeJSON(w, http.StatusOK, ChatHistoryResponse{
		ConversationID: convID,
		Messages:       entries,
		Count:          len(entries),
	})
}

// handleChatHistoryByID serves DELETE /api/v1/chat/history/{conversation_id}.
func (h *Handler) handleChatHistoryByID(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	switch r.Method {
	case http.MethodDelete:
		h.deleteChatHistory(w, r)
	case http.MethodGet:
		h.getChatHistoryByID(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (h *Handler) getChatHistoryByID(w http.ResponseWriter, r *http.Request) {
	convID := extractChatHistoryID(r.URL.Path)
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required", "validation_error")
		return
	}

	msgs := h.convos.History(convID, 0, 0)
	if msgs == nil {
		writeError(w, http.StatusNotFound, "conversation not found", "not_found")
		return
	}

	entries := make([]ChatHistoryEntry, 0, len(msgs))
	for _, m := range msgs {
		entries = append(entries, ChatHistoryEntry{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		})
	}
	writeJSON(w, http.StatusOK, ChatHistoryResponse{
		ConversationID: convID,
		Messages:       entries,
		Count:          len(entries),
	})
}

func (h *Handler) deleteChatHistory(w http.ResponseWriter, r *http.Request) {
	convID := extractChatHistoryID(r.URL.Path)
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required", "validation_error")
		return
	}

	if !h.convos.Delete(convID) {
		writeError(w, http.StatusNotFound, "conversation not found", "not_found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":          "deleted",
		"conversation_id": convID,
	})
}

func extractChatHistoryID(path string) string {
	const prefix = "/api/v1/chat/history/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(path, prefix))
}
