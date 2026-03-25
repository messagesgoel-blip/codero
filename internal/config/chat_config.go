package config

import (
	"os"
	"strconv"
)

// ChatConfig holds all 30 CODERO_CHAT_* configuration variables from
// LiteLLM Chat v1 §6. Every field has a spec-mandated default.
type ChatConfig struct {
	Enabled              bool    `yaml:"enabled"`
	LiteLLMAPIURL        string  `yaml:"litellm_api_url"`
	LiteLLMAPIKey        string  `yaml:"litellm_api_key"`
	LiteLLMModel         string  `yaml:"litellm_model"`
	LiteLLMTimeout       int     `yaml:"litellm_timeout"`
	LiteLLMMaxTokens     int     `yaml:"litellm_max_tokens"`
	LiteLLMTemperature   float64 `yaml:"litellm_temperature"`
	LiteLLMStream        bool    `yaml:"litellm_stream"`
	MaxContextSize       int     `yaml:"max_context_size"`
	MaxHistory           int     `yaml:"max_history"`
	ConversationTTL      int     `yaml:"conversation_ttl"`
	PersistHistory       bool    `yaml:"persist_history"`
	ToolsEnabled         bool    `yaml:"tools_enabled"`
	SystemPromptPath     string  `yaml:"system_prompt_path"`
	QuickQueriesEnabled  bool    `yaml:"quick_queries_enabled"`
	ContextSessionsLimit int     `yaml:"context_sessions_limit"`
	ContextArchivesLimit int     `yaml:"context_archives_limit"`
	ContextFeedbackLimit int     `yaml:"context_feedback_limit"`
	ContextScopeDefault  string  `yaml:"context_scope_default"`
	TUIKeybind           string  `yaml:"tui_keybind"`
	TUIMarkdown          bool    `yaml:"tui_markdown"`
	TUISyntaxHighlight   bool    `yaml:"tui_syntax_highlight"`
	TUITypingIndicator   bool    `yaml:"tui_typing_indicator"`
	TUICopyKeybind       string  `yaml:"tui_copy_keybind"`
	RetryOnError         bool    `yaml:"retry_on_error"`
	RetryMax             int     `yaml:"retry_max"`
	RetryDelay           int     `yaml:"retry_delay"`
	LogQueries           bool    `yaml:"log_queries"`
	LogResponses         bool    `yaml:"log_responses"`
	RateLimit            int     `yaml:"rate_limit"`
}

// DefaultChatConfig returns spec-mandated defaults (LiteLLM Chat v1 §6).
func DefaultChatConfig() ChatConfig {
	return ChatConfig{
		Enabled:              true,
		LiteLLMAPIURL:        "http://localhost:4000",
		LiteLLMAPIKey:        "",
		LiteLLMModel:         "gpt-4o-mini",
		LiteLLMTimeout:       30,
		LiteLLMMaxTokens:     2048,
		LiteLLMTemperature:   0.1,
		LiteLLMStream:        true,
		MaxContextSize:       16384,
		MaxHistory:           50,
		ConversationTTL:      3600,
		PersistHistory:       false,
		ToolsEnabled:         false,
		SystemPromptPath:     "",
		QuickQueriesEnabled:  true,
		ContextSessionsLimit: 20,
		ContextArchivesLimit: 10,
		ContextFeedbackLimit: 5,
		ContextScopeDefault:  "all",
		TUIKeybind:           "c",
		TUIMarkdown:          true,
		TUISyntaxHighlight:   true,
		TUITypingIndicator:   true,
		TUICopyKeybind:       "ctrl+y",
		RetryOnError:         true,
		RetryMax:             2,
		RetryDelay:           1,
		LogQueries:           false,
		LogResponses:         false,
		RateLimit:            30,
	}
}

// applyChatEnvOverrides reads CODERO_CHAT_* environment variables.
func applyChatEnvOverrides(c *ChatConfig) {
	if v := os.Getenv("CODERO_CHAT_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Enabled = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_LITELLM_API_URL"); v != "" {
		c.LiteLLMAPIURL = v
	}
	if v := os.Getenv("CODERO_CHAT_LITELLM_API_KEY"); v != "" {
		c.LiteLLMAPIKey = v
	}
	if v := os.Getenv("CODERO_CHAT_LITELLM_MODEL"); v != "" {
		c.LiteLLMModel = v
	}
	if v := os.Getenv("CODERO_CHAT_LITELLM_TIMEOUT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.LiteLLMTimeout = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_LITELLM_MAX_TOKENS"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.LiteLLMMaxTokens = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_LITELLM_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			c.LiteLLMTemperature = f
		}
	}
	if v := os.Getenv("CODERO_CHAT_LITELLM_STREAM"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.LiteLLMStream = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_MAX_CONTEXT_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.MaxContextSize = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_MAX_HISTORY"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.MaxHistory = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_CONVERSATION_TTL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.ConversationTTL = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_PERSIST_HISTORY"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.PersistHistory = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_TOOLS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.ToolsEnabled = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_SYSTEM_PROMPT_PATH"); v != "" {
		c.SystemPromptPath = v
	}
	if v := os.Getenv("CODERO_CHAT_QUICK_QUERIES_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.QuickQueriesEnabled = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_CONTEXT_SESSIONS_LIMIT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.ContextSessionsLimit = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_CONTEXT_ARCHIVES_LIMIT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.ContextArchivesLimit = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_CONTEXT_FEEDBACK_LIMIT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			c.ContextFeedbackLimit = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_CONTEXT_SCOPE_DEFAULT"); v != "" {
		c.ContextScopeDefault = v
	}
	if v := os.Getenv("CODERO_CHAT_TUI_KEYBIND"); v != "" {
		c.TUIKeybind = v
	}
	if v := os.Getenv("CODERO_CHAT_TUI_MARKDOWN"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.TUIMarkdown = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_TUI_SYNTAX_HIGHLIGHT"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.TUISyntaxHighlight = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_TUI_TYPING_INDICATOR"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.TUITypingIndicator = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_TUI_COPY_KEYBIND"); v != "" {
		c.TUICopyKeybind = v
	}
	if v := os.Getenv("CODERO_CHAT_RETRY_ON_ERROR"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.RetryOnError = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_RETRY_MAX"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 {
			c.RetryMax = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_RETRY_DELAY"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 {
			c.RetryDelay = i
		}
	}
	if v := os.Getenv("CODERO_CHAT_LOG_QUERIES"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.LogQueries = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_LOG_RESPONSES"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.LogResponses = b
		}
	}
	if v := os.Getenv("CODERO_CHAT_RATE_LIMIT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 {
			c.RateLimit = i
		}
	}
}
