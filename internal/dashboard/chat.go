package dashboard

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	loglib "github.com/codero/codero/internal/log"
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
	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt must not be empty", "validation_error")
		return
	}
	if len(req.Prompt) > dashboardChatMaxPromptLen {
		req.Prompt = req.Prompt[:dashboardChatMaxPromptLen]
	}

	snapshot := h.collectChatSnapshot(r.Context(), req.Tab)
	if req.Stream {
		h.streamChat(w, r, req, snapshot)
		return
	}
	writeJSON(w, http.StatusOK, h.chatResponse(r.Context(), req, snapshot))
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

func (h *Handler) chatResponse(ctx context.Context, req ChatRequest, snapshot dashboardChatSnapshot) ChatResponse {
	reply, provider, model := h.askLiteLLM(ctx, req, snapshot)
	return ChatResponse{
		Reply:       reply,
		Provider:    provider,
		Model:       model,
		Suggestions: dashboardChatSuggestions(req.Tab, req.Prompt, snapshot),
		Actions:     dashboardChatActions(req.Tab, snapshot),
		GeneratedAt: time.Now().UTC(),
	}
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

func (h *Handler) streamChat(w http.ResponseWriter, r *http.Request, req ChatRequest, snapshot dashboardChatSnapshot) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusOK, h.chatResponse(r.Context(), req, snapshot))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	setCORSHeaders(w)

	if final, streamed := h.streamLiteLLM(w, flusher, r.Context(), req, snapshot); streamed {
		writeSSEEvent(w, flusher, "done", final)
		return
	}

	final := h.chatResponse(r.Context(), req, snapshot)
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

func (h *Handler) streamLiteLLM(w http.ResponseWriter, flusher http.Flusher, ctx context.Context, req ChatRequest, snapshot dashboardChatSnapshot) (ChatResponse, bool) {
	model := dashboardChatModel()
	endpoint := dashboardChatEndpoint()
	key := dashboardChatKey()
	if endpoint == "" || model == "" || key == "" {
		return ChatResponse{}, false
	}

	payload := dashboardChatPrompt(req, snapshot)
	body, err := json.Marshal(liteLLMChatRequest{
		Model:       model,
		Messages:    []liteLLMChatMessage{{Role: "system", Content: dashboardChatSystemPrompt()}, {Role: "user", Content: payload}},
		Temperature: 0.2,
		MaxTokens:   700,
		Stream:      true,
	})
	if err != nil {
		loglib.Warn("dashboard: chat marshal failed",
			loglib.FieldComponent, "dashboard", "error", err)
		return ChatResponse{}, false
	}

	reqCtx, cancel := context.WithTimeout(ctx, dashboardChatTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		loglib.Warn("dashboard: chat request build failed",
			loglib.FieldComponent, "dashboard", "error", err)
		return ChatResponse{}, false
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		loglib.Warn("dashboard: chat request failed",
			loglib.FieldComponent, "dashboard", "error", err)
		return ChatResponse{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		loglib.Warn("dashboard: chat non-2xx response",
			loglib.FieldComponent, "dashboard", "status", resp.StatusCode, "body", truncateForLog(string(data)))
		return ChatResponse{}, false
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var reply strings.Builder
	var emitted bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var ev liteLLMStreamResponse
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		chunk := dashboardChatStreamChunk(ev)
		if chunk == "" {
			continue
		}
		emitted = true
		reply.WriteString(chunk)
		writeSSEEvent(w, flusher, "delta", map[string]string{"delta": chunk})
	}
	if err := scanner.Err(); err != nil {
		loglib.Warn("dashboard: chat stream scan failed",
			loglib.FieldComponent, "dashboard", "error", err)
	}
	if !emitted {
		return ChatResponse{}, false
	}

	final := ChatResponse{
		Reply:       strings.TrimSpace(reply.String()),
		Provider:    "litellm",
		Model:       model,
		Suggestions: dashboardChatSuggestions(req.Tab, req.Prompt, snapshot),
		Actions:     dashboardChatActions(req.Tab, snapshot),
		GeneratedAt: time.Now().UTC(),
	}
	return final, true
}

func dashboardChatStreamChunk(ev liteLLMStreamResponse) string {
	if len(ev.Choices) == 0 {
		return ""
	}
	for _, choice := range ev.Choices {
		if content := strings.TrimSpace(choice.Delta.Content); content != "" {
			return content
		}
		if content := strings.TrimSpace(choice.Message.Content); content != "" {
			return content
		}
		if content := strings.TrimSpace(choice.Text); content != "" {
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

func (h *Handler) askLiteLLM(ctx context.Context, req ChatRequest, snapshot dashboardChatSnapshot) (reply, provider, model string) {
	model = dashboardChatModel()
	endpoint := dashboardChatEndpoint()
	key := dashboardChatKey()
	if endpoint == "" || model == "" || key == "" {
		return dashboardChatFallbackReply(req, snapshot), "fallback", "local-summary"
	}

	payload := dashboardChatPrompt(req, snapshot)
	body, err := json.Marshal(liteLLMChatRequest{
		Model:       model,
		Messages:    []liteLLMChatMessage{{Role: "system", Content: dashboardChatSystemPrompt()}, {Role: "user", Content: payload}},
		Temperature: 0.2,
		MaxTokens:   700,
		Stream:      false,
	})
	if err != nil {
		loglib.Warn("dashboard: chat marshal failed",
			loglib.FieldComponent, "dashboard", "error", err)
		return dashboardChatFallbackReply(req, snapshot), "fallback", "local-summary"
	}

	reqCtx, cancel := context.WithTimeout(ctx, dashboardChatTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		loglib.Warn("dashboard: chat request build failed",
			loglib.FieldComponent, "dashboard", "error", err)
		return dashboardChatFallbackReply(req, snapshot), "fallback", "local-summary"
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		loglib.Warn("dashboard: chat request failed",
			loglib.FieldComponent, "dashboard", "error", err)
		return dashboardChatFallbackReply(req, snapshot), "fallback", "local-summary"
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		loglib.Warn("dashboard: chat response read failed",
			loglib.FieldComponent, "dashboard", "error", err)
		return dashboardChatFallbackReply(req, snapshot), "fallback", "local-summary"
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		loglib.Warn("dashboard: chat non-2xx response",
			loglib.FieldComponent, "dashboard", "status", resp.StatusCode, "body", truncateForLog(string(data)))
		return dashboardChatFallbackReply(req, snapshot), "fallback", "local-summary"
	}

	var parsed liteLLMChatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		loglib.Warn("dashboard: chat response parse failed",
			loglib.FieldComponent, "dashboard", "error", err)
		return dashboardChatFallbackReply(req, snapshot), "fallback", "local-summary"
	}

	reply = dashboardChatReplyFromLLM(parsed)
	if strings.TrimSpace(reply) == "" {
		return dashboardChatFallbackReply(req, snapshot), "fallback", "local-summary"
	}
	return reply, "litellm", model
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
	snapshotJSON, err := json.MarshalIndent(snapshot, "", "  ")
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
