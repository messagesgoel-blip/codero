package tui

import (
	"strings"
	"testing"
)

func TestRenderTerminalThread_ShowsNewestMessages(t *testing.T) {
	m := Model{
		theme: DefaultTheme,
		cliMessages: []terminalMessage{
			{Role: "assistant", Content: "oldest"},
			{Role: "assistant", Content: "middle"},
			{Role: "assistant", Content: "newest"},
		},
	}

	lines := m.renderTerminalThread(80, 2)
	rendered := strings.Join(lines, "\n")

	if strings.Contains(rendered, "oldest") {
		t.Fatalf("expected oldest message to be truncated, got:\n%s", rendered)
	}
	for _, want := range []string{"middle", "newest"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered thread to contain %q, got:\n%s", want, rendered)
		}
	}
}
