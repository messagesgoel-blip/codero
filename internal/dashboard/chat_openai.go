package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func (h *Handler) runtimeChatConfig() ChatConfig {
	return LoadChatConfig()
}

func (h *Handler) chatBackendAvailable(cfg ChatConfig) bool {
	if !cfg.Enabled {
		return false
	}
	if strings.TrimSpace(cfg.LiteLLMAPIURL) == "" {
		return false
	}
	if strings.TrimSpace(cfg.LiteLLMModel) == "" {
		return false
	}
	if strings.TrimSpace(cfg.LiteLLMAPIKey) == "" {
		return false
	}
	return true
}

func (h *Handler) chatHistoryForConversation(conversationID string) []ConversationMessage {
	if h == nil || h.convos == nil {
		return nil
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil
	}
	convo := h.convos.Get(conversationID)
	if convo == nil || len(convo.Messages) == 0 {
		return nil
	}
	out := make([]ConversationMessage, len(convo.Messages))
	copy(out, convo.Messages)
	return out
}

func (h *Handler) appendConversationTurn(conversationID, userPrompt, assistantReply string) {
	if h == nil || h.convos == nil {
		return
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return
	}
	h.convos.GetOrCreate(conversationID)
	if strings.TrimSpace(userPrompt) != "" {
		h.convos.Append(conversationID, "user", userPrompt)
	}
	if strings.TrimSpace(assistantReply) != "" {
		h.convos.Append(conversationID, "assistant", assistantReply)
	}
}

func (h *Handler) chatTimeoutContext(ctx context.Context, cfg ChatConfig) (context.Context, context.CancelFunc) {
	timeout := dashboardChatTimeout
	if cfg.LiteLLMTimeout > 0 {
		timeout = time.Duration(cfg.LiteLLMTimeout) * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}

func (h *Handler) buildOpenAIClient(cfg ChatConfig) openai.Client {
	opts := []option.RequestOption{
		option.WithBaseURL(strings.TrimSpace(cfg.LiteLLMAPIURL)),
		option.WithAPIKey(strings.TrimSpace(cfg.LiteLLMAPIKey)),
	}
	if cfg.RetryMax >= 0 {
		opts = append(opts, option.WithMaxRetries(cfg.RetryMax))
	}
	if cfg.LiteLLMTimeout > 0 {
		opts = append(opts, option.WithRequestTimeout(time.Duration(cfg.LiteLLMTimeout)*time.Second))
	}
	return openai.NewClient(opts...)
}

func (h *Handler) buildOpenAIChatMessages(req ChatRequest, history []ConversationMessage, contextMarkdown string, cfg ChatConfig) []openai.ChatCompletionMessageParamUnion {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(history)+3)
	messages = append(messages, openai.SystemMessage(dashboardChatSystemPrompt()))

	for _, msg := range history {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(msg.Role)) {
		case "assistant":
			messages = append(messages, openai.AssistantMessage(content))
		default:
			messages = append(messages, openai.UserMessage(content))
		}
	}

	var prompt strings.Builder
	if tab := strings.TrimSpace(req.Tab); tab != "" {
		fmt.Fprintf(&prompt, "Current review tab: %s\n\n", tab)
	}
	if ctx := strings.TrimSpace(req.Context); ctx != "" {
		fmt.Fprintf(&prompt, "User context:\n%s\n\n", ctx)
	}
	if ctx := strings.TrimSpace(contextMarkdown); ctx != "" {
		fmt.Fprintf(&prompt, "Review context (markdown):\n%s\n\n", ctx)
	}
	fmt.Fprintf(&prompt, "User prompt:\n%s", strings.TrimSpace(req.Prompt))
	messages = append(messages, openai.UserMessage(prompt.String()))
	return messages
}

func (h *Handler) assembleChatContextMarkdown(ctx context.Context, scope string) string {
	cfg := h.runtimeChatConfig()
	maxSize := cfg.MaxContextSize
	if maxSize <= 0 {
		maxSize = dashboardChatMaxPromptLen
	}

	scope = normalizeChatContextScope(scope, cfg.ContextScopeDefault)
	sections := make([]string, 0, 8)
	sections = append(sections, renderMarkdownSection("Summary", []string{
		fmt.Sprintf("scope: %s", scope),
		fmt.Sprintf("generated_at: %s", time.Now().UTC().Format(time.RFC3339)),
	}))

	if scope == "all" || scope == "sessions" {
		sections = append(sections, h.renderSessionsContext(ctx, cfg)...)
	}
	if scope == "all" || scope == "assignments" {
		sections = append(sections, h.renderAssignmentsContext(ctx)...)
	}
	if scope == "all" || scope == "queue" {
		sections = append(sections, h.renderQueueContext(ctx)...)
	}
	if scope == "all" || scope == "repos" {
		sections = append(sections, h.renderReposContext(ctx)...)
	}
	if scope == "all" || scope == "archives" {
		sections = append(sections, h.renderArchivesContext(ctx, cfg)...)
	}
	if scope == "all" || scope == "feedback" {
		sections = append(sections, h.renderFeedbackContext(ctx, cfg)...)
	}

	full := strings.Join(sections, "\n\n")
	return trimMarkdownToBudget(full, maxSize)
}

func normalizeChatContextScope(scope, fallback string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = strings.ToLower(strings.TrimSpace(fallback))
	}
	switch scope {
	case "", "all":
		return "all"
	case "sessions", "session":
		return "sessions"
	case "assignments", "assignment":
		return "assignments"
	case "queue":
		return "queue"
	case "repos", "repo":
		return "repos"
	case "archives", "archive":
		return "archives"
	case "feedback":
		return "feedback"
	default:
		if strings.Contains(scope, "session") {
			return "sessions"
		}
		if strings.Contains(scope, "assign") {
			return "assignments"
		}
		if strings.Contains(scope, "queue") {
			return "queue"
		}
		if strings.Contains(scope, "repo") {
			return "repos"
		}
		if strings.Contains(scope, "archive") {
			return "archives"
		}
		if strings.Contains(scope, "feedback") {
			return "feedback"
		}
		return "all"
	}
}

func trimMarkdownToBudget(markdown string, limit int) string {
	if limit <= 0 || len(markdown) <= limit {
		return markdown
	}
	cut := strings.LastIndex(markdown[:limit], "\n")
	if cut < 0 {
		cut = limit
	}
	trimmed := strings.TrimSpace(markdown[:cut])
	if trimmed == "" {
		trimmed = markdown[:limit]
	}
	note := "\n\n_context trimmed to fit budget_"
	if len(trimmed)+len(note) <= limit {
		return trimmed + note
	}
	return trimmed
}

func renderMarkdownSection(title string, lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s", title)
	if len(lines) == 0 {
		return b.String()
	}
	b.WriteString("\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s\n", line)
	}
	return strings.TrimSpace(b.String())
}

func (h *Handler) renderSessionsContext(ctx context.Context, cfg ChatConfig) []string {
	sessions, err := queryActiveSessions(ctx, h.db, cfg.ContextSessionsLimit)
	if err != nil {
		return []string{fmt.Sprintf("Active sessions unavailable: %v", err)}
	}
	lines := []string{fmt.Sprintf("active_sessions: %d", len(sessions))}
	for _, s := range sessions {
		age := time.Since(s.LastHeartbeatAt).Round(time.Second)
		if age < 0 {
			age = 0
		}
		task := ""
		if s.Task != nil {
			task = fmt.Sprintf(" task=%s/%s", s.Task.ID, s.Task.Phase)
		}
		lines = append(lines, fmt.Sprintf("%s agent=%s repo=%s branch=%s checkpoint=%s heartbeat_age=%s%s",
			s.SessionID, s.AgentID, s.Repo, s.Branch, sessionCheckpointForChat(s), age, task))
	}
	if len(sessions) == 0 {
		lines = append(lines, "none")
	}
	return []string{renderMarkdownSection("Active Sessions", lines)}
}

func (h *Handler) renderAssignmentsContext(ctx context.Context) []string {
	assignments, err := queryAssignments(ctx, h.db, 12)
	if err != nil {
		return []string{renderMarkdownSection("Assignments", []string{fmt.Sprintf("unavailable: %v", err)})}
	}
	lines := []string{fmt.Sprintf("assignments: %d", len(assignments))}
	for _, a := range assignments {
		branch := a.Branch
		if branch == "" {
			branch = "-"
		}
		lines = append(lines, fmt.Sprintf("%s session=%s agent=%s repo=%s branch=%s state=%s substatus=%s pr=%d",
			a.AssignmentID, a.SessionID, a.AgentID, a.Repo, branch, a.State, a.Substatus, a.PRNumber))
	}
	if len(assignments) == 0 {
		lines = append(lines, "none")
	}
	return []string{renderMarkdownSection("Assignments", lines)}
}

func (h *Handler) renderQueueContext(ctx context.Context) []string {
	pending, active, blocked, total, err := queryQueueStats(ctx, h.db)
	if err != nil {
		return []string{renderMarkdownSection("Queue", []string{fmt.Sprintf("unavailable: %v", err)})}
	}
	items, err := queryQueue(ctx, h.db)
	if err != nil {
		return []string{renderMarkdownSection("Queue", []string{fmt.Sprintf("queue depth: pending=%d active=%d blocked=%d total=%d", pending, active, blocked, total), fmt.Sprintf("items unavailable: %v", err)})}
	}
	lines := []string{
		fmt.Sprintf("queue depth: pending=%d active=%d blocked=%d total=%d", pending, active, blocked, total),
	}
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("%s/%s state=%s priority=%d owner_session=%s submitted=%s",
			item.Repo, item.Branch, item.State, item.Priority, item.OwnerSessionID, item.SubmissionTime.UTC().Format(time.RFC3339)))
	}
	if len(items) == 0 {
		lines = append(lines, "none")
	}
	return []string{renderMarkdownSection("Queue", lines)}
}

func (h *Handler) renderReposContext(ctx context.Context) []string {
	repos, err := queryRepos(ctx, h.db)
	if err != nil {
		return []string{renderMarkdownSection("Repos", []string{fmt.Sprintf("unavailable: %v", err)})}
	}
	lines := []string{fmt.Sprintf("repos: %d", len(repos))}
	for _, repo := range repos {
		gates := make([]string, 0, len(repo.GateSummary))
		for _, pill := range repo.GateSummary {
			gates = append(gates, pill.Name+"="+pill.Status)
		}
		gateText := "none"
		if len(gates) > 0 {
			gateText = strings.Join(gates, ", ")
		}
		lines = append(lines, fmt.Sprintf("%s/%s state=%s head=%s last_run=%s gates=%s",
			repo.Repo, repo.Branch, repo.State, repo.HeadHash, repo.LastRunStatus, gateText))
	}
	if len(repos) == 0 {
		lines = append(lines, "none")
	}
	return []string{renderMarkdownSection("Repos", lines)}
}

func (h *Handler) renderArchivesContext(ctx context.Context, cfg ChatConfig) []string {
	archives, err := querySessionArchives(ctx, h.db, cfg.ContextArchivesLimit)
	if err != nil {
		return []string{renderMarkdownSection("Recent Archives", []string{fmt.Sprintf("unavailable: %v", err)})}
	}
	lines := []string{fmt.Sprintf("archives: %d", len(archives))}
	for _, archive := range archives {
		task := archive.TaskID
		if task == "" {
			task = "-"
		}
		lines = append(lines, fmt.Sprintf("%s session=%s result=%s repo=%s branch=%s task=%s duration=%ds commits=%d archived_at=%s",
			archive.ArchiveID, archive.SessionID, archive.Result, archive.Repo, archive.Branch, task, archive.DurationSeconds, archive.CommitCount, archive.EndedAt))
	}
	if len(archives) == 0 {
		lines = append(lines, "none")
	}
	return []string{renderMarkdownSection("Recent Archives", lines)}
}

func (h *Handler) renderFeedbackContext(ctx context.Context, cfg ChatConfig) []string {
	feedback, err := queryFeedbackHistory(ctx, h.db, cfg.ContextFeedbackLimit)
	if err != nil {
		return []string{renderMarkdownSection("Recent Feedback", []string{fmt.Sprintf("unavailable: %v", err)})}
	}
	lines := []string{fmt.Sprintf("feedback_items: %d", len(feedback))}
	for _, item := range feedback {
		message := strings.TrimSpace(item.Message)
		message = strings.ReplaceAll(message, "\n", " ")
		message = truncateContextText(message, 120)
		lines = append(lines, fmt.Sprintf("%s severity=%s source=%s repo=%s branch=%s file=%s line=%d message=%q",
			item.ID, item.Severity, item.Source, item.Repo, item.Branch, item.File, item.Line, message))
	}
	if len(feedback) == 0 {
		lines = append(lines, "none")
	}
	return []string{renderMarkdownSection("Recent Feedback", lines)}
}

func sessionCheckpointForChat(s ActiveSession) string {
	if s.ActivityState != "" {
		switch strings.ToLower(strings.TrimSpace(s.ActivityState)) {
		case "waiting":
			return "WAITING"
		case "blocked":
			return "BLOCKED"
		case "reviewing":
			return "REVIEWING"
		case "completed":
			return "COMPLETED"
		}
	}
	if s.Task != nil && s.Task.Phase != "" {
		return strings.ToUpper(strings.TrimSpace(s.Task.Phase))
	}
	return "ACTIVE"
}

func truncateContextText(s string, max int) string {
	if max <= 0 {
		return s
	}
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
