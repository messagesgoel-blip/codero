package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	loglib "github.com/codero/codero/internal/log"
	"github.com/openai/openai-go"
)

const (
	dashboardChatTimeout      = 20 * time.Second
	dashboardChatMaxPromptLen = 4096
	dashboardChatActivityLen  = 8
	dashboardChatSessionLen   = 5
	dashboardChatBlockLen     = 5
)

type dashboardChatSnapshot struct {
	Focus         string           `json:"focus,omitempty"`
	Overview      dashboardFocus   `json:"overview"`
	Health        DashboardHealth  `json:"health"`
	ActiveSession []ActiveSession  `json:"active_sessions"`
	RecentEvents  []ActivityEvent  `json:"recent_events"`
	BlockReasons  []BlockReason    `json:"block_reasons"`
	GateChecks    *GateCheckReport `json:"gate_checks,omitempty"`
	GeneratedAt   time.Time        `json:"generated_at"`
}

type dashboardFocus struct {
	RunsToday    int     `json:"runs_today"`
	PassRate     float64 `json:"pass_rate"`
	BlockedCount int     `json:"blocked_count"`
	AvgGateSec   float64 `json:"avg_gate_sec"`
}

type liteLLMChatRequest struct {
	Model       string               `json:"model"`
	Messages    []liteLLMChatMessage `json:"messages"`
	Temperature float64              `json:"temperature,omitempty"`
	MaxTokens   int                  `json:"max_tokens,omitempty"`
	Stream      bool                 `json:"stream,omitempty"`
}

type liteLLMChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type liteLLMChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Text string `json:"text"`
	} `json:"choices"`
}

type dashboardChatPromptSnapshot struct {
	Focus         string                        `json:"focus,omitempty"`
	Overview      dashboardChatPromptOverview   `json:"overview"`
	Health        dashboardChatPromptHealth     `json:"health"`
	ActiveSession []dashboardChatPromptSession  `json:"active_sessions"`
	RecentEvents  []dashboardChatPromptEvent    `json:"recent_events"`
	BlockReasons  []dashboardChatPromptBlocker  `json:"block_reasons"`
	GateChecks    *dashboardChatPromptGateCheck `json:"gate_checks,omitempty"`
	GeneratedAt   time.Time                     `json:"generated_at"`
}

type dashboardChatPromptOverview struct {
	RunsToday    int     `json:"runs_today"`
	PassRate     float64 `json:"pass_rate"`
	BlockedCount int     `json:"blocked_count"`
	AvgGateSec   float64 `json:"avg_gate_sec"`
}

type dashboardChatPromptHealth struct {
	Database         dashboardChatPromptServiceStatus `json:"database"`
	Feeds            dashboardChatPromptFeeds         `json:"feeds"`
	ActiveAgentCount int                              `json:"active_agent_count"`
	SecurityScore    *SecurityScoreStats              `json:"security_score,omitempty"`
	CoveragePct      *float64                         `json:"coverage_pct,omitempty"`
	ETAMin           *int                             `json:"eta_min,omitempty"`
	GeneratedAt      time.Time                        `json:"generated_at"`
}

type dashboardChatPromptServiceStatus struct {
	Status string `json:"status"`
}

type dashboardChatPromptFeeds struct {
	ActiveSessions dashboardChatPromptFeedStatus `json:"active_sessions"`
	GateChecks     dashboardChatPromptFeedStatus `json:"gate_checks"`
}

type dashboardChatPromptFeedStatus struct {
	Status       string    `json:"status"`
	LastRefresh  time.Time `json:"last_refresh,omitempty"`
	FreshnessSec int64     `json:"freshness_sec"`
}

type dashboardChatPromptTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Phase string `json:"phase"`
}

type dashboardChatPromptSession struct {
	SessionID       string                   `json:"session_id"`
	Repo            string                   `json:"repo"`
	Branch          string                   `json:"branch"`
	PRNumber        int                      `json:"pr_number"`
	OwnerAgent      string                   `json:"owner_agent"`
	ActivityState   string                   `json:"activity_state"`
	Task            *dashboardChatPromptTask `json:"task,omitempty"`
	StartedAt       time.Time                `json:"started_at"`
	LastHeartbeatAt time.Time                `json:"last_heartbeat_at"`
	ElapsedSec      int64                    `json:"elapsed_sec"`
}

type dashboardChatPromptEvent struct {
	Seq       int64     `json:"seq"`
	Repo      string    `json:"repo"`
	Branch    string    `json:"branch"`
	EventType string    `json:"event_type"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type dashboardChatPromptBlocker struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
}

type dashboardChatPromptGateCheck struct {
	Summary     GateCheckSummary           `json:"summary"`
	Checks      []dashboardChatPromptCheck `json:"checks"`
	RunAt       time.Time                  `json:"run_at"`
	GeneratedAt time.Time                  `json:"generated_at"`
}

type dashboardChatPromptCheck struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Group      string `json:"group"`
	Required   bool   `json:"required"`
	Enabled    bool   `json:"enabled"`
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// handleChat serves POST /api/v1/dashboard/chat (and the legacy /comments alias).
func (h *Handler) handleChat(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	switch r.Method {
	case http.MethodPost:
		h.postChat(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (h *Handler) postChat(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	cfg := h.runtimeChatConfig()
	if !cfg.Enabled {
		writeError(w, http.StatusServiceUnavailable, "chat is disabled", "chat_disabled")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body", "read_error")
		return
	}

	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "parse_error")
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Context = strings.TrimSpace(req.Context)
	req.Tab = strings.TrimSpace(req.Tab)
	req.ConversationID = strings.TrimSpace(req.ConversationID)
	req.ContextScope = strings.TrimSpace(req.ContextScope)
	if req.ContextScope == "" {
		req.ContextScope = cfg.ContextScopeDefault
	}
	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt must not be empty", "validation_error")
		return
	}
	if len(req.Prompt) > dashboardChatMaxPromptLen {
		req.Prompt = req.Prompt[:dashboardChatMaxPromptLen]
	}

	// Expand quick queries (§4.4)
	if cfg.QuickQueriesEnabled {
		if expanded, ok := ExpandQuickQuery(req.Prompt); ok {
			req.Prompt = expanded
		}
	}

	// Get or create conversation for multi-turn.
	convo := h.convos.GetOrCreate(req.ConversationID)
	req.ConversationID = convo.ID

	contextMarkdown := h.assembleChatContextMarkdown(r.Context(), req.ContextScope)
	snapshot := h.collectChatSnapshot(r.Context(), req.Tab)
	if req.Stream {
		h.streamChat(w, r, req, snapshot, contextMarkdown, cfg)
		return
	}

	resp, err := h.chatResponse(r.Context(), req, snapshot, contextMarkdown, cfg)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "chat backend unavailable", "chat_backend_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) collectChatSnapshot(ctx context.Context, focus string) dashboardChatSnapshot {
	snap := dashboardChatSnapshot{
		Focus:       focus,
		GeneratedAt: time.Now().UTC(),
	}

	if h.db != nil {
		if runsToday, passedToday, blockedCount, avgGateSec, err := queryOverview(ctx, h.db); err == nil {
			passRate := -1.0
			if runsToday > 0 {
				passRate = float64(passedToday) / float64(runsToday) * 100
			}
			snap.Overview = dashboardFocus{
				RunsToday:    runsToday,
				PassRate:     passRate,
				BlockedCount: blockedCount,
				AvgGateSec:   avgGateSec,
			}
		} else {
			loglib.Warn("dashboard: chat overview query failed",
				loglib.FieldComponent, "dashboard", "error", err)
		}

		if health, err := queryDashboardHealth(ctx, h.db); err == nil {
			snap.Health = health
		} else {
			loglib.Warn("dashboard: chat health query failed",
				loglib.FieldComponent, "dashboard", "error", err)
			snap.Health = DashboardHealth{
				Database:    ServiceStatus{Status: "unavailable", Message: err.Error()},
				GeneratedAt: time.Now().UTC(),
			}
		}

		if sessions, err := queryActiveSessions(ctx, h.db, dashboardChatSessionLen); err == nil {
			snap.ActiveSession = sessions
		} else {
			loglib.Warn("dashboard: chat sessions query failed",
				loglib.FieldComponent, "dashboard", "error", err)
		}

		if events, err := queryActivity(ctx, h.db, dashboardChatActivityLen); err == nil {
			snap.RecentEvents = events
		} else {
			loglib.Warn("dashboard: chat activity query failed",
				loglib.FieldComponent, "dashboard", "error", err)
		}

		if reasons, err := queryBlockReasons(ctx, h.db); err == nil {
			if len(reasons) > dashboardChatBlockLen {
				reasons = reasons[:dashboardChatBlockLen]
			}
			snap.BlockReasons = reasons
		} else {
			loglib.Warn("dashboard: chat block-reasons query failed",
				loglib.FieldComponent, "dashboard", "error", err)
		}
	}

	if rpt := loadGateCheckSnapshot(); rpt != nil {
		snap.GateChecks = rpt
	}

	return snap
}

func (h *Handler) chatResponse(ctx context.Context, req ChatRequest, snapshot dashboardChatSnapshot, contextMarkdown string, cfg ChatConfig) (ChatResponse, error) {
	reply, provider, model, err := h.askLiteLLM(ctx, req, snapshot, contextMarkdown, cfg)
	if err != nil {
		return ChatResponse{}, err
	}

	return ChatResponse{
		Reply:          reply,
		Provider:       provider,
		Model:          model,
		ConversationID: req.ConversationID,
		Suggestions:    dashboardChatSuggestions(req.Tab, req.Prompt, snapshot),
		Actions:        dashboardChatActions(req.Tab, snapshot),
		GeneratedAt:    time.Now().UTC(),
	}, nil
}

func loadGateCheckSnapshot() *GateCheckReport {
	reportPath := gateCheckReportPath()
	data, err := os.ReadFile(reportPath) //nolint:gosec
	if err != nil {
		return nil
	}
	var rpt GateCheckReport
	if err := json.Unmarshal(data, &rpt); err != nil {
		return nil
	}
	if len(rpt.Checks) > dashboardChatBlockLen {
		rpt.Checks = rpt.Checks[:dashboardChatBlockLen]
	}
	return &rpt
}

func (h *Handler) streamChat(w http.ResponseWriter, r *http.Request, req ChatRequest, snapshot dashboardChatSnapshot, contextMarkdown string, cfg ChatConfig) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		resp, err := h.chatResponse(r.Context(), req, snapshot, contextMarkdown, cfg)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "chat backend unavailable", "chat_backend_unavailable")
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	setCORSHeaders(w)

	if final, streamed, err := h.streamLiteLLM(w, flusher, r.Context(), req, snapshot, contextMarkdown, cfg); err != nil {
		writeError(w, http.StatusServiceUnavailable, "chat backend unavailable", "chat_backend_unavailable")
		return
	} else if streamed {
		writeSSEEvent(w, flusher, "done", final)
		return
	}

	final, err := h.chatResponse(r.Context(), req, snapshot, contextMarkdown, cfg)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "chat backend unavailable", "chat_backend_unavailable")
		return
	}
	streamLocalReply(w, flusher, final.Reply)
	writeSSEEvent(w, flusher, "done", final)
}

type liteLLMStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Text string `json:"text"`
	} `json:"choices"`
}

func (h *Handler) streamLiteLLM(w http.ResponseWriter, flusher http.Flusher, ctx context.Context, req ChatRequest, snapshot dashboardChatSnapshot, contextMarkdown string, cfg ChatConfig) (ChatResponse, bool, error) {
	if !h.chatBackendAvailable(cfg) {
		return ChatResponse{}, false, nil
	}

	history := h.chatHistoryForConversation(req.ConversationID)
	messages := h.buildOpenAIChatMessages(req, history, contextMarkdown, cfg)

	client := h.buildOpenAIClient(cfg)
	reqCtx, cancel := h.chatTimeoutContext(ctx, cfg)
	defer cancel()

	stream := client.Chat.Completions.NewStreaming(reqCtx, openai.ChatCompletionNewParams{
		Model:       cfg.LiteLLMModel,
		Messages:    messages,
		Temperature: openai.Float(cfg.LiteLLMTemperature),
		MaxTokens:   openai.Int(int64(cfg.LiteLLMMaxTokens)),
	})
	if stream == nil {
		return ChatResponse{}, false, fmt.Errorf("openai streaming client unavailable")
	}
	defer stream.Close()

	var reply strings.Builder
	var emitted bool
	for stream.Next() {
		chunk := stream.Current()
		for _, choice := range chunk.Choices {
			if content := strings.TrimSpace(choice.Delta.Content); content != "" {
				emitted = true
				reply.WriteString(choice.Delta.Content)
				writeSSEEvent(w, flusher, "delta", map[string]string{"delta": choice.Delta.Content})
			}
		}
	}
	if err := stream.Err(); err != nil {
		return ChatResponse{}, false, err
	}
	if !emitted {
		return ChatResponse{}, false, fmt.Errorf("empty streaming response")
	}

	final := ChatResponse{
		Reply:          reply.String(),
		Provider:       "litellm",
		Model:          cfg.LiteLLMModel,
		ConversationID: req.ConversationID,
		Suggestions:    dashboardChatSuggestions(req.Tab, req.Prompt, snapshot),
		Actions:        dashboardChatActions(req.Tab, snapshot),
		GeneratedAt:    time.Now().UTC(),
	}
	h.appendConversationTurn(req.ConversationID, req.Prompt, final.Reply)
	return final, true, nil
}

func dashboardChatStreamChunk(ev liteLLMStreamResponse) string {
	if len(ev.Choices) == 0 {
		return ""
	}
	for _, choice := range ev.Choices {
		if content := choice.Delta.Content; content != "" {
			return content
		}
		if content := choice.Message.Content; content != "" {
			return content
		}
		if content := choice.Text; content != "" {
			return content
		}
	}
	return ""
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(`{"error":"failed to encode event"}`)
	}
	_, _ = io.Copy(w, strings.NewReader("event: "+event+"\n"))
	_, _ = io.Copy(w, strings.NewReader("data: "+string(data)+"\n\n"))
	flusher.Flush()
}

func streamLocalReply(w http.ResponseWriter, flusher http.Flusher, reply string) {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return
	}
	parts := splitChatChunks(reply)
	if len(parts) == 0 {
		return
	}
	for _, part := range parts {
		writeSSEEvent(w, flusher, "delta", map[string]string{"delta": part})
	}
}

func splitChatChunks(reply string) []string {
	var parts []string
	fields := strings.Fields(reply)
	if len(fields) == 0 {
		return nil
	}
	var b strings.Builder
	for i, field := range fields {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(field)
		if strings.HasSuffix(field, ".") || strings.HasSuffix(field, "!") || strings.HasSuffix(field, "?") || b.Len() > 80 {
			parts = append(parts, b.String())
			b.Reset()
		}
		if i == len(fields)-1 && b.Len() > 0 {
			parts = append(parts, b.String())
		}
	}
	return parts
}

func (h *Handler) askLiteLLM(ctx context.Context, req ChatRequest, snapshot dashboardChatSnapshot, contextMarkdown string, cfg ChatConfig) (reply, provider, model string, err error) {
	if !h.chatBackendAvailable(cfg) {
		reply = dashboardChatFallbackReply(req, snapshot)
		h.appendConversationTurn(req.ConversationID, req.Prompt, reply)
		return reply, "fallback", "local-summary", nil
	}

	history := h.chatHistoryForConversation(req.ConversationID)
	messages := h.buildOpenAIChatMessages(req, history, contextMarkdown, cfg)

	client := h.buildOpenAIClient(cfg)
	reqCtx, cancel := h.chatTimeoutContext(ctx, cfg)
	defer cancel()

	resp, err := client.Chat.Completions.New(reqCtx, openai.ChatCompletionNewParams{
		Model:       cfg.LiteLLMModel,
		Messages:    messages,
		Temperature: openai.Float(cfg.LiteLLMTemperature),
		MaxTokens:   openai.Int(int64(cfg.LiteLLMMaxTokens)),
	})
	if err != nil {
		return "", "", "", err
	}

	for _, choice := range resp.Choices {
		if content := strings.TrimSpace(choice.Message.Content); content != "" {
			reply = content
			break
		}
	}
	if strings.TrimSpace(reply) == "" {
		return "", "", "", fmt.Errorf("empty chat completion")
	}

	h.appendConversationTurn(req.ConversationID, req.Prompt, reply)
	return reply, "litellm", cfg.LiteLLMModel, nil
}

// buildChatMessages constructs the LiteLLM messages array including
// system prompt, prior conversation history, and current user prompt with context.
func (h *Handler) buildChatMessages(req ChatRequest, snapshot dashboardChatSnapshot) []liteLLMChatMessage {
	messages := []liteLLMChatMessage{
		{Role: "system", Content: dashboardChatSystemPrompt()},
	}

	// Include prior conversation history (excluding the current turn which was just appended)
	if req.ConversationID != "" {
		history := h.convos.History(req.ConversationID, 0, 0)
		// Exclude the last message (the current user prompt) since we add it with snapshot context
		if len(history) > 1 {
			for _, msg := range history[:len(history)-1] {
				messages = append(messages, liteLLMChatMessage{
					Role:    msg.Role,
					Content: msg.Content,
				})
			}
		}
	}

	// Current user prompt with fresh snapshot context
	payload := dashboardChatPrompt(req, snapshot)
	messages = append(messages, liteLLMChatMessage{Role: "user", Content: payload})
	return messages
}

func dashboardChatModel() string {
	if v := strings.TrimSpace(os.Getenv("CODERO_LITELLM_MODEL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("LITELLM_MODEL")); v != "" {
		return v
	}
	return "qwen3-coder-plus"
}

func dashboardChatEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("CODERO_LITELLM_URL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("LITELLM_PROXY_URL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("LITELLM_URL")); v != "" {
		return v
	}
	return "http://localhost:4000/v1/chat/completions"
}

func dashboardChatKey() string {
	keys := []string{
		"CODERO_LITELLM_MASTER_KEY",
		"CODERO_LITELLM_API_KEY",
		"LITELLM_MASTER_KEY",
		"LITELLM_API_KEY",
		"OPENAI_API_KEY",
	}
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func dashboardChatSystemPrompt() string {
	return strings.TrimSpace(`
You are Codero Review Assistant.
You are advisory only and must not change state, execute commands, or imply that the dashboard can mutate data.
You must only discuss the review process: queue, gate checks, findings, active sessions, activity, and merge readiness.
If the user asks about anything outside the review process, briefly redirect them back to review workflow context.
Use only the supplied snapshot. If something is missing, say so explicitly.
Answer in concise plain text with:
1. the direct answer
2. the evidence from the snapshot
3. a safe next step, if useful
Prefer concrete names, counts, and statuses over vague summaries.
`)
}

func dashboardChatPrompt(req ChatRequest, snapshot dashboardChatSnapshot) string {
	sanitized := sanitizeDashboardChatSnapshot(snapshot)
	snapshotJSON, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		snapshotJSON = []byte(`{"error":"failed to serialize snapshot"}`)
	}

	var b strings.Builder
	if req.Tab != "" {
		fmt.Fprintf(&b, "Current review tab: %s\n", req.Tab)
	}
	if req.Context != "" {
		fmt.Fprintf(&b, "User context: %s\n", req.Context)
	}
	fmt.Fprintf(&b, "User prompt: %s\n\n", req.Prompt)
	b.WriteString("Review-process snapshot:\n")
	b.Write(snapshotJSON)
	return b.String()
}

func sanitizeDashboardChatSnapshot(snapshot dashboardChatSnapshot) dashboardChatPromptSnapshot {
	sanitized := dashboardChatPromptSnapshot{
		Focus: snapshot.Focus,
		Overview: dashboardChatPromptOverview{
			RunsToday:    snapshot.Overview.RunsToday,
			PassRate:     snapshot.Overview.PassRate,
			BlockedCount: snapshot.Overview.BlockedCount,
			AvgGateSec:   snapshot.Overview.AvgGateSec,
		},
		Health: dashboardChatPromptHealth{
			Database: dashboardChatPromptServiceStatus{Status: snapshot.Health.Database.Status},
			Feeds: dashboardChatPromptFeeds{
				ActiveSessions: dashboardChatPromptFeedStatus{
					Status:       snapshot.Health.Feeds.ActiveSessions.Status,
					LastRefresh:  snapshot.Health.Feeds.ActiveSessions.LastRefresh,
					FreshnessSec: snapshot.Health.Feeds.ActiveSessions.FreshnessSec,
				},
				GateChecks: dashboardChatPromptFeedStatus{
					Status:       snapshot.Health.Feeds.GateChecks.Status,
					LastRefresh:  snapshot.Health.Feeds.GateChecks.LastRefresh,
					FreshnessSec: snapshot.Health.Feeds.GateChecks.FreshnessSec,
				},
			},
			ActiveAgentCount: snapshot.Health.ActiveAgentCount,
			SecurityScore:    snapshot.Health.SecurityScore,
			CoveragePct:      snapshot.Health.CoveragePct,
			ETAMin:           snapshot.Health.ETAMin,
			GeneratedAt:      snapshot.Health.GeneratedAt,
		},
		GeneratedAt: snapshot.GeneratedAt,
	}

	if len(snapshot.ActiveSession) > 0 {
		sanitized.ActiveSession = make([]dashboardChatPromptSession, 0, len(snapshot.ActiveSession))
		for _, s := range snapshot.ActiveSession {
			var task *dashboardChatPromptTask
			if s.Task != nil {
				task = &dashboardChatPromptTask{
					ID:    s.Task.ID,
					Title: s.Task.Title,
					Phase: s.Task.Phase,
				}
			}
			sanitized.ActiveSession = append(sanitized.ActiveSession, dashboardChatPromptSession{
				SessionID:       s.SessionID,
				Repo:            s.Repo,
				Branch:          s.Branch,
				PRNumber:        s.PRNumber,
				OwnerAgent:      s.OwnerAgent,
				ActivityState:   s.ActivityState,
				Task:            task,
				StartedAt:       s.StartedAt,
				LastHeartbeatAt: s.LastHeartbeatAt,
				ElapsedSec:      s.ElapsedSec,
			})
		}
	}

	if len(snapshot.RecentEvents) > 0 {
		sanitized.RecentEvents = make([]dashboardChatPromptEvent, 0, len(snapshot.RecentEvents))
		for _, ev := range snapshot.RecentEvents {
			sanitized.RecentEvents = append(sanitized.RecentEvents, dashboardChatPromptEvent{
				Seq:       ev.Seq,
				Repo:      ev.Repo,
				Branch:    ev.Branch,
				EventType: ev.EventType,
				Summary:   dashboardActivityPromptSummary(ev),
				CreatedAt: ev.CreatedAt,
			})
		}
	}

	if len(snapshot.BlockReasons) > 0 {
		sanitized.BlockReasons = make([]dashboardChatPromptBlocker, 0, len(snapshot.BlockReasons))
		for _, r := range snapshot.BlockReasons {
			sanitized.BlockReasons = append(sanitized.BlockReasons, dashboardChatPromptBlocker{
				Source: r.Source,
				Count:  r.Count,
			})
		}
	}

	if snapshot.GateChecks != nil {
		sanitized.GateChecks = &dashboardChatPromptGateCheck{
			Summary:     snapshot.GateChecks.Summary,
			RunAt:       snapshot.GateChecks.RunAt,
			GeneratedAt: snapshot.GateChecks.GeneratedAt,
		}
		if len(snapshot.GateChecks.Checks) > 0 {
			sanitized.GateChecks.Checks = make([]dashboardChatPromptCheck, 0, len(snapshot.GateChecks.Checks))
			for _, check := range snapshot.GateChecks.Checks {
				sanitized.GateChecks.Checks = append(sanitized.GateChecks.Checks, dashboardChatPromptCheck{
					ID:         check.ID,
					Name:       check.Name,
					Group:      check.Group,
					Required:   check.Required,
					Enabled:    check.Enabled,
					Status:     check.Status,
					ReasonCode: check.ReasonCode,
					ToolName:   check.ToolName,
					DurationMS: check.DurationMS,
				})
			}
		}
	}

	return sanitized
}

func dashboardChatReplyFromLLM(resp liteLLMChatResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	for _, choice := range resp.Choices {
		if content := strings.TrimSpace(choice.Message.Content); content != "" {
			return content
		}
		if content := strings.TrimSpace(choice.Text); content != "" {
			return content
		}
	}
	return ""
}

func dashboardActivityPromptSummary(ev ActivityEvent) string {
	payload := strings.TrimSpace(ev.EventType)
	if payload == "" {
		payload = "activity"
	}
	if ev.Repo != "" {
		if ev.Branch != "" {
			return fmt.Sprintf("%s on %s/%s", payload, ev.Repo, ev.Branch)
		}
		return fmt.Sprintf("%s on %s", payload, ev.Repo)
	}
	return payload
}

func dashboardChatFallbackReply(req ChatRequest, snapshot dashboardChatSnapshot) string {
	var b strings.Builder
	if req.Tab != "" {
		fmt.Fprintf(&b, "Review focus: %s\n", req.Tab)
	}
	if snapshot.Health.Database.Status != "" {
		fmt.Fprintf(&b, "Review engine health: %s\n", snapshot.Health.Database.Status)
	}
	if snapshot.Health.Feeds.ActiveSessions.Status != "" {
		fmt.Fprintf(&b, "Active sessions feed: %s\n", snapshot.Health.Feeds.ActiveSessions.Status)
	}
	if snapshot.Health.Feeds.GateChecks.Status != "" {
		fmt.Fprintf(&b, "Gate-check feed: %s\n", snapshot.Health.Feeds.GateChecks.Status)
	}
	if snapshot.Overview.RunsToday > 0 {
		fmt.Fprintf(&b, "Review runs today: %d, pass rate: %.0f%%, blocked: %d, avg gate: %.1fs\n",
			snapshot.Overview.RunsToday, snapshot.Overview.PassRate, snapshot.Overview.BlockedCount, snapshot.Overview.AvgGateSec)
	}
	if len(snapshot.ActiveSession) > 0 {
		s := snapshot.ActiveSession[0]
		fmt.Fprintf(&b, "Top active session: %s on %s/%s (%s)\n", s.SessionID, s.Repo, s.Branch, s.ActivityState)
	}
	if len(snapshot.BlockReasons) > 0 {
		r := snapshot.BlockReasons[0]
		fmt.Fprintf(&b, "Top review blocker: %s (%d)\n", r.Source, r.Count)
	}
	if len(snapshot.RecentEvents) > 0 {
		ev := snapshot.RecentEvents[0]
		fmt.Fprintf(&b, "Latest review activity: %s\n", dashboardActivitySummary(ev))
	}
	if snapshot.GateChecks != nil {
		fmt.Fprintf(&b, "Gate status: %s (%d checks)\n",
			snapshot.GateChecks.Summary.OverallStatus, snapshot.GateChecks.Summary.Total)
	}
	if b.Len() == 0 {
		return "The review snapshot is empty right now. There is no live review data to summarize yet."
	}
	b.WriteString("\nSafe next step: inspect the relevant review tab and use the live data above to decide whether a gate rerun or branch investigation is needed.")
	return strings.TrimSpace(b.String())
}

func dashboardChatSuggestions(tab, prompt string, snapshot dashboardChatSnapshot) []ChatSuggestion {
	prompt = strings.ToLower(strings.TrimSpace(prompt))
	tab = strings.ToLower(strings.TrimSpace(tab))
	switch {
	case strings.Contains(prompt, "help"):
		return []ChatSuggestion{
			{Label: "status", Prompt: "Give me the current review status."},
			{Label: "gate", Prompt: "Summarize the gate checks and blockers."},
			{Label: "findings", Prompt: "Show the main findings and what is blocking the review."},
			{Label: "queue", Prompt: "What is happening in the review queue right now?"},
		}
	case strings.Contains(prompt, "queue"):
		return []ChatSuggestion{
			{Label: "status", Prompt: "Summarize the current review status."},
			{Label: "gate", Prompt: "What gate checks are blocking progress?"},
			{Label: "findings", Prompt: "Which findings need attention first?"},
			{Label: "merge", Prompt: "Is the branch ready to merge?"},
		}
	case strings.Contains(prompt, "gate"):
		return []ChatSuggestion{
			{Label: "status", Prompt: "Give me the current review status."},
			{Label: "queue", Prompt: "What is in the review queue right now?"},
			{Label: "findings", Prompt: "Summarize the open findings."},
			{Label: "merge", Prompt: "Is the branch ready to merge?"},
		}
	case tab == "findings":
		return []ChatSuggestion{
			{Label: "top blocker", Prompt: "What is the top blocker in the review findings?"},
			{Label: "gate", Prompt: "Summarize the gate checks and blockers."},
			{Label: "merge", Prompt: "Is the branch ready to merge?"},
			{Label: "status", Prompt: "Summarize the current review status."},
		}
	case tab == "eventlogs":
		return []ChatSuggestion{
			{Label: "latest activity", Prompt: "Summarize the latest review activity."},
			{Label: "status", Prompt: "Summarize the current review status."},
			{Label: "gate", Prompt: "Summarize the gate checks and blockers."},
			{Label: "findings", Prompt: "Show the main findings from the review."},
		}
	case tab == "processes":
		return []ChatSuggestion{
			{Label: "status", Prompt: "Summarize the current review status."},
			{Label: "queue", Prompt: "What is happening in the review queue right now?"},
			{Label: "gate", Prompt: "What gate checks are blocking progress?"},
			{Label: "findings", Prompt: "Summarize the open findings."},
		}
	default:
		_ = snapshot
		return []ChatSuggestion{
			{Label: "status", Prompt: "Give me the current review status."},
			{Label: "help", Prompt: "Show me the useful review commands."},
			{Label: "gate", Prompt: "Summarize the gate checks and blockers."},
			{Label: "queue", Prompt: "What is happening in the review queue right now?"},
		}
	}
}

func dashboardChatActions(tab string, snapshot dashboardChatSnapshot) []ChatAction {
	tab = strings.ToLower(strings.TrimSpace(tab))
	switch tab {
	case "findings":
		return []ChatAction{
			{Title: "Review top blocker", Detail: "Use the current findings to identify the highest-priority blocker.", Prompt: "What is the top blocker in the review findings?", Tab: "findings"},
			{Title: "Check merge readiness", Detail: "Confirm whether the gate and findings allow a merge-ready state.", Prompt: "Is the branch ready to merge?", Tab: "findings"},
		}
	case "eventlogs":
		return []ChatAction{
			{Title: "Summarize latest activity", Detail: "Convert the newest event log entries into a review-process summary.", Prompt: "Summarize the latest review activity.", Tab: "eventlogs"},
			{Title: "Trace review blockers", Detail: "Follow the event trail that led to the current review state.", Prompt: "Trace the events that led to the current review blockers.", Tab: "eventlogs"},
		}
	case "processes":
		return []ChatAction{
			{Title: "Inspect active review", Detail: "Focus on the currently active sessions and their review phase.", Prompt: "Summarize the active review sessions and their phase.", Tab: "processes"},
			{Title: "Check queue pressure", Detail: "Describe whether the review queue is growing or clearing.", Prompt: "What is happening in the review queue right now?", Tab: "processes"},
		}
	default:
		_ = snapshot
		return []ChatAction{
			{Title: "Review status", Detail: "Ask for the current review state and blockers.", Prompt: "Summarize the current review status.", Tab: tab},
			{Title: "Gate checks", Detail: "Ask for the current gate status and blocking checks.", Prompt: "Summarize the gate checks and blockers.", Tab: tab},
		}
	}
}

func truncateForLog(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 256 {
		return s
	}
	return s[:256] + "…"
}

func dashboardActivitySummary(ev ActivityEvent) string {
	payload := strings.TrimSpace(ev.Payload)
	if payload == "" {
		payload = ev.EventType
	}
	if ev.Repo != "" {
		if ev.Branch != "" {
			return fmt.Sprintf("%s on %s/%s", payload, ev.Repo, ev.Branch)
		}
		return fmt.Sprintf("%s on %s", payload, ev.Repo)
	}
	return payload
}
