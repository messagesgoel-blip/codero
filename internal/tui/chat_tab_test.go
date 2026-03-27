package tui

import (
	"strings"
	"testing"
)

func TestRenderCodexMessage_UserPrefix(t *testing.T) {
	m := newTestModel()
	msg := terminalMessage{Role: "user", Content: "hello world"}
	rendered := m.renderCodexMessage(msg, 80)
	if !strings.Contains(rendered, "\u203a") {
		t.Error("user message should have \u203a prefix")
	}
	if !strings.Contains(rendered, "hello world") {
		t.Error("user message should contain content")
	}
}

func TestRenderCodexMessage_AssistantPrefix(t *testing.T) {
	m := newTestModel()
	msg := terminalMessage{Role: "assistant", Content: "gate passed"}
	rendered := m.renderCodexMessage(msg, 80)
	if !strings.Contains(rendered, "\u2022") {
		t.Error("assistant message should have \u2022 prefix")
	}
}

func TestRenderCodexMessage_ErrorPrefix(t *testing.T) {
	m := newTestModel()
	msg := terminalMessage{Role: "error", Content: "connection failed"}
	rendered := m.renderCodexMessage(msg, 80)
	if !strings.Contains(rendered, "\u2717") {
		t.Error("error message should have \u2717 prefix")
	}
}

func TestEstimateTokenUsageApprox_Empty(t *testing.T) {
	m := newTestModel()
	m.cliMessages = nil
	used, total := m.estimateTokenUsageApprox()
	if used != 0 {
		t.Errorf("empty: used = %d, want 0", used)
	}
	if total <= 0 {
		t.Error("total should be positive")
	}
}

func TestEstimateTokenUsageApprox_WithMessages(t *testing.T) {
	m := newTestModel()
	m.cliMessages = []terminalMessage{
		{Role: "user", Content: strings.Repeat("a", 400)}, // ~100 tokens
	}
	used, total := m.estimateTokenUsageApprox()
	if used != 100 {
		t.Errorf("used = %d, want 100", used)
	}
	if total <= 0 {
		t.Error("total should be positive")
	}
}

func TestRenderChatTab_RendersComponents(t *testing.T) {
	m := newTestModel()
	m.cliMessages = []terminalMessage{
		{Role: "system", Content: "Welcome"},
		{Role: "user", Content: "status"},
	}
	rendered := m.renderChatTab(80, 24)
	if !strings.Contains(rendered, "Codero Chat") {
		t.Error("chat tab should contain session header")
	}
	if !strings.Contains(rendered, "status") {
		t.Error("chat tab should contain suggestion chip or user message")
	}
}

func TestSimpleWordWrap(t *testing.T) {
	input := "one two three four five six"
	wrapped := simpleWordWrap(input, 10)
	for _, line := range strings.Split(wrapped, "\n") {
		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}
		if len(line) > 12 { // some tolerance for single long words
			t.Errorf("line too long: %q", line)
		}
	}
}

func newTestModel() Model {
	return New(Config{
		Theme: DefaultTheme,
	})
}
