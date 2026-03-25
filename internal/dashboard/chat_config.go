package dashboard

import (
	"os"
	"strconv"
	"strings"
)

// ChatConfig holds all 30+ config variables defined in the LiteLLM Chat v1 spec §6.
type ChatConfig struct {
	Enabled              bool
	LiteLLMAPIURL        string
	LiteLLMAPIKey        string
	LiteLLMModel         string
	LiteLLMTimeout       int // seconds
	LiteLLMMaxTokens     int
	LiteLLMTemperature   float64
	LiteLLMStream        bool
	MaxContextSize       int // bytes
	MaxHistory           int
	ConversationTTL      int // seconds
	PersistHistory       bool
	ToolsEnabled         bool
	SystemPromptPath     string
	QuickQueriesEnabled  bool
	ContextSessionsLimit int
	ContextArchivesLimit int
	ContextFeedbackLimit int
	ContextScopeDefault  string
	TUIKeybind           string
	TUIMarkdown          bool
	TUISyntaxHighlight   bool
	TUITypingIndicator   bool
	TUICopyKeybind       string
	RetryOnError         bool
	RetryMax             int
	RetryDelay           int // seconds
	LogQueries           bool
	LogResponses         bool
	RateLimit            int // queries per minute, 0=unlimited
}

// LoadChatConfig reads all CODERO_CHAT_* env vars with the spec defaults.
func LoadChatConfig() ChatConfig {
	return ChatConfig{
		Enabled:              envBool("CODERO_CHAT_ENABLED", true),
		LiteLLMAPIURL:        envStr("CODERO_CHAT_LITELLM_API_URL", dashboardChatEndpointDefault()),
		LiteLLMAPIKey:        envFirstOf([]string{"CODERO_CHAT_LITELLM_API_KEY", "CODERO_LITELLM_MASTER_KEY", "CODERO_LITELLM_API_KEY", "LITELLM_MASTER_KEY", "LITELLM_API_KEY", "OPENAI_API_KEY"}, ""),
		LiteLLMModel:         envFirstOf([]string{"CODERO_CHAT_LITELLM_MODEL", "CODERO_LITELLM_MODEL", "LITELLM_MODEL"}, "qwen3-coder-plus"),
		LiteLLMTimeout:       envInt("CODERO_CHAT_LITELLM_TIMEOUT", 30),
		LiteLLMMaxTokens:     envInt("CODERO_CHAT_LITELLM_MAX_TOKENS", 2048),
		LiteLLMTemperature:   envFloat("CODERO_CHAT_LITELLM_TEMPERATURE", 0.1),
		LiteLLMStream:        envBool("CODERO_CHAT_LITELLM_STREAM", true),
		MaxContextSize:       envInt("CODERO_CHAT_MAX_CONTEXT_SIZE", 16384),
		MaxHistory:           envInt("CODERO_CHAT_MAX_HISTORY", 50),
		ConversationTTL:      envInt("CODERO_CHAT_CONVERSATION_TTL", 3600),
		PersistHistory:       envBool("CODERO_CHAT_PERSIST_HISTORY", false),
		ToolsEnabled:         envBool("CODERO_CHAT_TOOLS_ENABLED", false),
		SystemPromptPath:     envStr("CODERO_CHAT_SYSTEM_PROMPT_PATH", ""),
		QuickQueriesEnabled:  envBool("CODERO_CHAT_QUICK_QUERIES_ENABLED", true),
		ContextSessionsLimit: envInt("CODERO_CHAT_CONTEXT_SESSIONS_LIMIT", 20),
		ContextArchivesLimit: envInt("CODERO_CHAT_CONTEXT_ARCHIVES_LIMIT", 10),
		ContextFeedbackLimit: envInt("CODERO_CHAT_CONTEXT_FEEDBACK_LIMIT", 5),
		ContextScopeDefault:  envStr("CODERO_CHAT_CONTEXT_SCOPE_DEFAULT", "all"),
		TUIKeybind:           envStr("CODERO_CHAT_TUI_KEYBIND", "c"),
		TUIMarkdown:          envBool("CODERO_CHAT_TUI_MARKDOWN", true),
		TUISyntaxHighlight:   envBool("CODERO_CHAT_TUI_SYNTAX_HIGHLIGHT", true),
		TUITypingIndicator:   envBool("CODERO_CHAT_TUI_TYPING_INDICATOR", true),
		TUICopyKeybind:       envStr("CODERO_CHAT_TUI_COPY_KEYBIND", "ctrl+y"),
		RetryOnError:         envBool("CODERO_CHAT_RETRY_ON_ERROR", true),
		RetryMax:             envInt("CODERO_CHAT_RETRY_MAX", 2),
		RetryDelay:           envInt("CODERO_CHAT_RETRY_DELAY", 1),
		LogQueries:           envBool("CODERO_CHAT_LOG_QUERIES", false),
		LogResponses:         envBool("CODERO_CHAT_LOG_RESPONSES", false),
		RateLimit:            envInt("CODERO_CHAT_RATE_LIMIT", 30),
	}
}

func dashboardChatEndpointDefault() string {
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

func envStr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envFirstOf(keys []string, def string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return def
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
