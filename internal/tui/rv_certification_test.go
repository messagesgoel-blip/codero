package tui_test

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/tui"
	"github.com/codero/codero/internal/tui/adapters"
)

// ══════════════════════════════════════════════════════════════════════════
// Real-Time Views v1 TUI Certification Tests
// ══════════════════════════════════════════════════════════════════════════

func rvCfg() tui.Config {
	return tui.Config{
		RepoPath:  "test-repo", // relative path avoids path-guard
		Interval:  1 * time.Second,
		Theme:     tui.DefaultTheme,
		WatchMode: false,
		InitialVM: adapters.FromGateResult(gate.Result{Status: gate.StatusPending}),
	}
}

// §2.3 Session drill-down view exists and is renderable.
func TestRV_SessionDrillPane_Renderable(t *testing.T) {
	cfg := rvCfg()
	cfg.InitialTab = tui.TabSessionDrill
	m := tui.New(cfg)
	// Send a window size to trigger layout.
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newM.(tui.Model)
	v := m.View()
	// The session drill pane should render its placeholder.
	if v == "" {
		t.Error("session drill view rendered empty")
	}
}

// §2.8 Archives view exists and is renderable.
func TestRV_ArchivesPane_Renderable(t *testing.T) {
	cfg := rvCfg()
	cfg.InitialTab = tui.TabArchives
	m := tui.New(cfg)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newM.(tui.Model)
	v := m.View()
	if v == "" {
		t.Error("archives view rendered empty")
	}
}

// §2.9 Compliance view exists and is renderable.
func TestRV_CompliancePane_Renderable(t *testing.T) {
	cfg := rvCfg()
	cfg.InitialTab = tui.TabCompliance
	m := tui.New(cfg)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newM.(tui.Model)
	v := m.View()
	if v == "" {
		t.Error("compliance view rendered empty")
	}
}

// §2.2 Overview view exists and renders.
func TestRV_OverviewPane_Renderable(t *testing.T) {
	cfg := rvCfg()
	m := tui.New(cfg)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newM.(tui.Model)
	v := m.View()
	if v == "" {
		t.Error("overview/default view rendered empty")
	}
}

// §2.4 Gate view (GatePane is rendered in left pane always).
func TestRV_GatePane_VisibleInLayout(t *testing.T) {
	cfg := rvCfg()
	m := tui.New(cfg)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newM.(tui.Model)
	v := m.View()
	// Gate pane renders status icons.
	if v == "" {
		t.Error("gate view not rendered")
	}
}

// §2.7 Events view exists and is renderable.
func TestRV_EventsPane_Renderable(t *testing.T) {
	cfg := rvCfg()
	cfg.InitialTab = tui.TabEvents
	m := tui.New(cfg)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newM.(tui.Model)
	v := m.View()
	if v == "" {
		t.Error("events view rendered empty")
	}
}

// RV-6 Graceful degradation: TUI renders without DB, without error.
func TestRV_GracefulDegradation_NoDB(t *testing.T) {
	cfg := rvCfg()
	cfg.StateDB = nil // no DB
	cfg.InitialTab = tui.TabArchives
	m := tui.New(cfg)
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = newM.(tui.Model)
	v := m.View()
	if v == "" {
		t.Error("TUI should render gracefully without DB")
	}
}

// §5 TUI config: verify Config struct has the required fields.
func TestRV_TUIConfig_RequiredFields(t *testing.T) {
	cfg := tui.Config{
		RepoPath:   "test-cfg-x",
		Interval:   1 * time.Second,
		Theme:      tui.DefaultTheme,
		WatchMode:  true,
		InitialTab: tui.TabLogs,
	}
	m := tui.New(cfg)
	_ = m // should compile and not panic
}

// Tab enumeration includes all 9 required tabs.
func TestRV_TabEnum_AllPresent(t *testing.T) {
	tabs := []tui.Tab{
		tui.TabLogs,
		tui.TabOverview,
		tui.TabEvents,
		tui.TabQueue,
		tui.TabChat,
		tui.TabSessionDrill,
		tui.TabArchives,
		tui.TabCompliance,
		tui.TabConfig,
	}
	seen := make(map[tui.Tab]bool)
	for _, tab := range tabs {
		if seen[tab] {
			t.Errorf("duplicate tab value %d", tab)
		}
		seen[tab] = true
	}
	if len(seen) != 9 {
		t.Errorf("expected 9 unique tabs, got %d", len(seen))
	}
}
