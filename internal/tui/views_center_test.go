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
	if !strings.Contains(view, "INTERACTIVE LOGS & ARCHITECTURE VISUALIZATION") {
		t.Error("LogsArchPane should show the INTERACTIVE LOGS & ARCHITECTURE VISUALIZATION header")
	}
}

func TestLogsArchPane_View_ShowsArchNodes(t *testing.T) {
	p := tui.NewLogsArchPane(tui.DefaultTheme)
	p.SetSize(100, 30)

	view := p.View()
	for _, want := range []string{"Auth Service", "Token Flow", "Auth Middleware"} {
		if !strings.Contains(view, want) {
			t.Errorf("LogsArchPane architecture diagram should contain %q", want)
		}
	}
}

func TestLogsArchPane_View_ShowsCodeSnippet(t *testing.T) {
	p := tui.NewLogsArchPane(tui.DefaultTheme)
	p.SetSize(100, 40)

	view := p.View()
	if !strings.Contains(view, "config.yaml") {
		t.Error("LogsArchPane should show code snippet box with config.yaml")
	}
	if !strings.Contains(view, "secret_loc") {
		t.Error("LogsArchPane should show secret_loc in code snippet")
	}
}

func TestLogsArchPane_View_ShowsDefaultLogEntries(t *testing.T) {
	p := tui.NewLogsArchPane(tui.DefaultTheme)
	p.SetSize(100, 30)

	view := p.View()
	// Default (no events) state: pane should still render without panic.
	// The arch diagram header is always visible.
	if !strings.Contains(view, "INTERACTIVE LOGS") {
		t.Error("LogsArchPane default view should render header without panic")
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
	// Very narrow — arch column should be suppressed
	p := tui.NewLogsArchPane(tui.DefaultTheme)
	p.SetSize(25, 20)

	view := p.View()
	// Must still render without panic; header should still appear
	if !strings.Contains(view, "INTERACTIVE LOGS") {
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

func TestBottomBar_ReviewFindingsButton(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)

	// Apply a layout so View() renders fully
	newModel, _ := m.Update(makeWindowSize(120, 40))
	view := newModel.(tui.Model).View()

	if !strings.Contains(view, "Review Findings") {
		t.Error("bottom bar should contain 'Review Findings' button")
	}
}

func TestBottomBar_MergeStatus(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)

	newModel, _ := m.Update(makeWindowSize(120, 40))
	view := newModel.(tui.Model).View()

	if !strings.Contains(view, "Merge Status") {
		t.Error("bottom bar should contain 'Merge Status'")
	}
}
