// services/openclaw-adapter/handler.go
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// queryRequest is the JSON body for POST /query.
type queryRequest struct {
	Prompt         string `json:"prompt"`
	ConversationID string `json:"conversation_id,omitempty"`
}

// queryResponse is the JSON response from POST /query.
type queryResponse struct {
	Response       string `json:"response"`
	ConversationID string `json:"conversation_id"`
}

// auditEntry is a single line in the JSONL audit log.
type auditEntry struct {
	Timestamp      time.Time `json:"ts"`
	ConversationID string    `json:"conversation_id"`
	Prompt         string    `json:"prompt"`
	Response       string    `json:"response"`
	StateAvailable bool      `json:"state_available"`
	DurationMs     int64     `json:"duration_ms"`
	Error          string    `json:"error,omitempty"`
}

type handler struct {
	cfg        adapterConfig
	httpClient *http.Client
	llmClient  openai.Client
	auditMu    sync.Mutex
	auditFile  *os.File
}

func newHandler(cfg adapterConfig) *handler {
	opts := []option.RequestOption{
		option.WithBaseURL(strings.TrimRight(cfg.LiteLLMURL, "/") + "/v1"),
	}
	if cfg.LiteLLMKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.LiteLLMKey))
	}
	opts = append(opts, option.WithRequestTimeout(30*time.Second))

	h := &handler{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		llmClient:  openai.NewClient(opts...),
	}

	if cfg.AuditLogPath != "" {
		f, err := os.OpenFile(cfg.AuditLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("warning: cannot open audit log %s: %v", cfg.AuditLogPath, err)
		} else {
			h.auditFile = f
		}
	}
	return h
}

func (h *handler) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		setCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		setCORS(w)
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	setCORS(w)
	start := time.Now()

	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if req.ConversationID == "" {
		req.ConversationID = uuid.New().String()
	}

	// 1. Fetch Codero state.
	stateJSON, stateOK := h.fetchState(r.Context())

	// 2. Build system prompt.
	sysPrompt := buildSystemPrompt(stateJSON, stateOK)

	// 3. Call LiteLLM.
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(sysPrompt),
		openai.UserMessage(strings.TrimSpace(req.Prompt)),
	}

	llmResp, err := h.llmClient.Chat.Completions.New(r.Context(), openai.ChatCompletionNewParams{
		Model:       h.cfg.LiteLLMModel,
		Messages:    messages,
		Temperature: openai.Float(0.3),
		MaxTokens:   openai.Int(1024),
	})

	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		h.audit(auditEntry{
			Timestamp: time.Now().UTC(), ConversationID: req.ConversationID,
			Prompt: req.Prompt, StateAvailable: stateOK,
			DurationMs: elapsed, Error: err.Error(),
		})
		writeErr(w, http.StatusBadGateway, "LLM unavailable")
		return
	}

	reply := ""
	if len(llmResp.Choices) > 0 {
		reply = llmResp.Choices[0].Message.Content
	}

	h.audit(auditEntry{
		Timestamp: time.Now().UTC(), ConversationID: req.ConversationID,
		Prompt: req.Prompt, Response: reply,
		StateAvailable: stateOK, DurationMs: elapsed,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queryResponse{
		Response:       reply,
		ConversationID: req.ConversationID,
	})
}

// fetchState GETs the OCL-010 state endpoint. Returns raw JSON string and ok flag.
func (h *handler) fetchState(ctx context.Context) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.StateURL, nil)
	if err != nil {
		log.Printf("state fetch: build request: %v", err)
		return "", false
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("state fetch: %v", err)
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("state fetch: status %d", resp.StatusCode)
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		log.Printf("state fetch: read body: %v", err)
		return "", false
	}
	return string(body), true
}

func buildSystemPrompt(stateJSON string, stateOK bool) string {
	var b strings.Builder
	b.WriteString("You are OpenClaw, the Codero CI/CD assistant.\n")
	b.WriteString("Answer questions about sessions, PRs, pipeline status, gate health, and system metrics.\n")
	b.WriteString("Be concise. Use specific names, numbers, and statuses from the state data.\n")
	b.WriteString("If you cannot answer from the provided state, say so.\n\n")

	if stateOK && stateJSON != "" {
		b.WriteString("Current Codero system state (JSON):\n")
		b.WriteString(stateJSON)
		b.WriteString("\n")
	} else {
		b.WriteString("WARNING: Codero system state is currently unavailable. ")
		b.WriteString("Answer based on general knowledge but note that live data is not available.\n")
	}
	return b.String()
}

func (h *handler) audit(entry auditEntry) {
	if h.auditFile == nil {
		return
	}
	h.auditMu.Lock()
	defer h.auditMu.Unlock()
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("audit: marshal: %v", err)
		return
	}
	h.auditFile.Write(append(data, '\n'))
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
