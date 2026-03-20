package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/tui"
	"github.com/codero/codero/internal/tui/adapters"
)

func makeCfg(watchMode bool) tui.Config {
	return tui.Config{
		RepoPath:  "/tmp/test-repo",
		Interval:  5 * time.Second,
		Theme:     tui.DefaultTheme,
		WatchMode: watchMode,
		InitialVM: adapters.FromGateResult(gate.Result{
			Status:        gate.StatusPending,
			CopilotStatus: "pending",
			LiteLLMStatus: "pending",
		}),
	}
}

func TestNew_NoPanic(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)
	_ = m // should not panic
}

func TestInit_WatchMode(t *testing.T) {
	cfg := makeCfg(true)
	m := tui.New(cfg)
	cmd := m.Init()
	if cmd == nil {
		t.Error("expected non-nil Init cmd in WatchMode")
	}
}

func TestInit_NoWatch(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)
	cmd := m.Init()
	if cmd != nil {
		t.Error("expected nil Init cmd when WatchMode=false")
	}
}

func TestView_AfterWindowSize(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)

	// Send a window size message
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := newModel.(tui.Model).View()
	if len(view) == 0 {
		t.Error("expected non-empty View after WindowSizeMsg")
	}
}

func TestView_ShowsMockStyleTitle(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)

	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := newModel.(tui.Model).View()
	if !strings.Contains(view, "COMMAND TERMINAL - CODERO") {
		t.Error("expected mock-style title in top bar")
	}
}

func TestView_ShowsCommandStrip(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)

	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := newModel.(tui.Model).View()
	for _, want := range []string{
		"> Type a command or message…",
		"status",
		"help",
		"run gate",
		"queue",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected command strip to contain %q\nfull view:\n%s", want, view)
		}
	}
}

func TestView_HidesCenterTabStrip(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)

	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view := newModel.(tui.Model).View()
	if strings.Contains(view, "logs & arch") {
		t.Fatalf("expected center tab strip to be hidden\nfull view:\n%s", view)
	}
}

func TestUpdate_QuitKey(t *testing.T) {
	cfg := makeCfg(false)
	m := tui.New(cfg)
	// Set a layout so View doesn't return "initializing"
	newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	_ = newModel
	_ = cmd

	// Now send quit key
	m2 := newModel.(tui.Model)
	_, quitCmd := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	// The quit command should be non-nil
	if quitCmd == nil {
		t.Error("expected quit cmd from q key")
	}
}

func TestAdapterFromPath(t *testing.T) {
	vm := tui.AdapterFromPath("/nonexistent")
	if vm.Status != gate.StatusPending {
		t.Errorf("expected pending, got %q", vm.Status)
	}
}
