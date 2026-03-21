package tui_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codero/codero/internal/tui"
)

func makeWindowSize(w, h int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: w, Height: h}
}

func TestLogsArchPane_View_ShowsHeader(t *testing.T) {
	p := tui.NewLogsArchPane(tui.DefaultTheme)
	p.SetSize(100, 30)

	view := p.View()
	if !strings.Contains(view, "EVENT STREAM") {
		t.Error("LogsArchPane should show the EVENT STREAM header")
	}
}

func TestLogsArchPane_View_ShowsDefaultLogEntries(t *testing.T) {
	p := tui.NewLogsArchPane(tui.DefaultTheme)
	p.SetSize(100, 30)

	view := p.View()
	for _, want := range []string{
		"Waiting for event stream...",
		"SSE connection establishing...",
		"Dashboard relay standby...",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("LogsArchPane default view should contain %q", want)
		}
	}
}

func TestLogsArchPane_View_EmptyAtZeroWidth(t *testing.T) {
	p := tui.NewLogsArchPane(tui.DefaultTheme)
	p.SetSize(0, 30)

	if p.View() != "" {
		t.Error("LogsArchPane with width=0 should return empty string")
	}
}

func TestLogsArchPane_View_NarrowHidesArch(t *testing.T) {
	p := tui.NewLogsArchPane(tui.DefaultTheme)
	p.SetSize(25, 20)

	view := p.View()
	if !strings.Contains(view, "EVENT STREAM") {
		t.Error("narrow LogsArchPane should still render header without panic")
	}
}

func TestChecksPane_View_ShowsVisualAnalysisPath(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(60, 50)
	p.SetVM(makeChecksVM())

	view := p.View()
	if !strings.Contains(view, "Visual analysis path") {
		t.Error("ChecksPane should show '→ Visual analysis path' under each bucket")
	}
}

func TestChecksPane_View_ShowsTotalLinesAnalyzed(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(60, 50)
	p.SetVM(makeChecksVM())

	view := p.View()
	if !strings.Contains(view, "Est. Lines Analyzed") {
		t.Error("ChecksPane Summary should show 'Est. Lines Analyzed'")
	}
}

func TestTerminalCLI_ShowsPrompt(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)

	newModel, _ := m.Update(makeWindowSize(120, 40))
	view := newModel.(tui.Model).View()

	if !strings.Contains(view, "REVIEW ASSISTANT") {
		t.Error("terminal area should contain 'REVIEW ASSISTANT'")
	}
	if !strings.Contains(strings.ToLower(view), "type a command or message…") {
		t.Error("terminal area should contain the command prompt placeholder")
	}
	for _, want := range []string{"status", "help", "run gate", "queue"} {
		if !strings.Contains(strings.ToLower(view), want) {
			t.Fatalf("terminal area should contain %q\nfull view:\n%s", want, view)
		}
	}
}
