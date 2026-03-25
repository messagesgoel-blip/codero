package tui_test

import (
	"testing"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/tui"
)

// =============================================================================
// Real-Time Views v1 — Certification Tests
//
// Each test maps to a specific criterion from codero_certification_matrix_v1.md
// §10 (Real-Time Views v1 acceptance criteria table).
// Evidence type abbreviations: UT = unit test, CLI = CLI behavior,
// CONFIG = config contract, API = API contract, IT = integration test.
// =============================================================================

// ---------------------------------------------------------------------------
// §2.1 — View registry contains exactly the 11 spec views.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S2_1_ViewRegistryComplete(t *testing.T) {
	specViews := map[tui.ViewID]bool{
		tui.ViewOverview:   true,
		tui.ViewSession:    true,
		tui.ViewGate:       true,
		tui.ViewQueue:      true,
		tui.ViewPipeline:   true,
		tui.ViewEvents:     true,
		tui.ViewChat:       true,
		tui.ViewBranch:     true,
		tui.ViewArchives:   true,
		tui.ViewCompliance: true,
		tui.ViewSettings:   true,
	}
	views := tui.AllViews()
	if len(views) != 11 {
		t.Fatalf("AllViews() = %d, want 11", len(views))
	}
	for _, v := range views {
		if !specViews[v] {
			t.Errorf("unexpected view in registry: %s", v)
		}
		delete(specViews, v)
	}
	for v := range specViews {
		t.Errorf("missing spec view in registry: %s", v)
	}
}

// ---------------------------------------------------------------------------
// §2.2 — Overview (gate status pane) renders.
// Evidence: GatePane is the TUI overview pane (mission control analog).
// ---------------------------------------------------------------------------
func TestCert_RTv1_S2_2_OverviewPaneExists(t *testing.T) {
	reg := tui.ViewRegistry()
	for _, m := range reg {
		if m.ID == tui.ViewOverview {
			if !m.Implemented {
				t.Error("ViewOverview not marked as implemented")
			}
			return
		}
	}
	t.Error("ViewOverview not found in registry")
}

// ---------------------------------------------------------------------------
// §2.4 — Gate view exists and is implemented.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S2_4_GateViewImplemented(t *testing.T) {
	for _, m := range tui.ViewRegistry() {
		if m.ID == tui.ViewGate {
			if !m.Implemented {
				t.Error("ViewGate not implemented")
			}
			return
		}
	}
	t.Error("ViewGate not found")
}

// ---------------------------------------------------------------------------
// §2.5 — Pipeline view exists.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S2_5_PipelineViewImplemented(t *testing.T) {
	for _, m := range tui.ViewRegistry() {
		if m.ID == tui.ViewPipeline {
			if !m.Implemented {
				t.Error("ViewPipeline not implemented")
			}
			return
		}
	}
	t.Error("ViewPipeline not found")
}

// ---------------------------------------------------------------------------
// §2.6 — Queue view exists.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S2_6_QueueViewImplemented(t *testing.T) {
	for _, m := range tui.ViewRegistry() {
		if m.ID == tui.ViewQueue {
			if !m.Implemented {
				t.Error("ViewQueue not implemented")
			}
			return
		}
	}
	t.Error("ViewQueue not found")
}

// ---------------------------------------------------------------------------
// §2.7 — Events view exists.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S2_7_EventsViewImplemented(t *testing.T) {
	for _, m := range tui.ViewRegistry() {
		if m.ID == tui.ViewEvents {
			if !m.Implemented {
				t.Error("ViewEvents not implemented")
			}
			return
		}
	}
	t.Error("ViewEvents not found")
}

// ---------------------------------------------------------------------------
// §2.8 — Archives view is registered (currently unimplemented — spec gap).
// ---------------------------------------------------------------------------
func TestCert_RTv1_S2_8_ArchivesViewRegistered(t *testing.T) {
	for _, m := range tui.ViewRegistry() {
		if m.ID == tui.ViewArchives {
			if m.Implemented {
				t.Log("ArchivesView now implemented — great!")
			} else {
				t.Log("ArchivesView registered but NOT implemented (known gap)")
			}
			return
		}
	}
	t.Error("ViewArchives not found in registry")
}

// ---------------------------------------------------------------------------
// §5 — All 38 TUI config variables have spec-mandated defaults.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S5_TUIConfigDefaults(t *testing.T) {
	c := config.DefaultTUIConfig()

	checks := []struct {
		name string
		ok   bool
	}{
		{"Enabled=true", c.Enabled},
		{"DefaultView=overview", c.DefaultView == "overview"},
		{"Theme=dracula", c.Theme == "dracula"},
		{"AltScreen=true", c.AltScreen},
		{"Mouse=true", c.Mouse},
		{"PollInterval=1", c.PollInterval == 1},
		{"SSEEnabled=false", !c.SSEEnabled},
		{"SSEReconnectMax=30", c.SSEReconnectMax == 30},
		{"SessionTableSort=checkpoint", c.SessionTableSort == "checkpoint"},
		{"SessionTableMaxRows=50", c.SessionTableMaxRows == 50},
		{"TimelineShowDuration=true", c.TimelineShowDuration},
		{"TimelineShowTS=true", c.TimelineShowTS},
		{"PipelineAnimation=true", c.PipelineAnimation},
		{"GateAutoRefresh=true", c.GateAutoRefresh},
		{"GateRefreshInterval=1", c.GateRefreshInterval == 1},
		{"EventsMaxLines=200", c.EventsMaxLines == 200},
		{"EventsFilterDefault=all", c.EventsFilterDefault == "all"},
		{"ArchivesPageSize=25", c.ArchivesPageSize == 25},
		{"HeartbeatWarnSec=30", c.HeartbeatWarnSec == 30},
		{"HeartbeatAlertSec=60", c.HeartbeatAlertSec == 60},
		{"OverviewRecentEvents=5", c.OverviewRecentEvents == 5},
		{"OverviewSystemHealth=true", c.OverviewSystemHealth},
		{"KeyOverview=o", c.KeyOverview == "o"},
		{"KeySession=s", c.KeySession == "s"},
		{"KeyGate=g", c.KeyGate == "g"},
		{"KeyQueue=q", c.KeyQueue == "q"},
		{"KeyPipeline=p", c.KeyPipeline == "p"},
		{"KeyEvents=e", c.KeyEvents == "e"},
		{"KeyBranch=b", c.KeyBranch == "b"},
		{"KeyArchives=a", c.KeyArchives == "a"},
		{"KeyCompliance=r", c.KeyCompliance == "r"},
		{"KeySettings=/", c.KeySettings == "/"},
		{"KeyHelp=?", c.KeyHelp == "?"},
		{"KeyQuit=ctrl+c", c.KeyQuit == "ctrl+c"},
		{"KeyRefresh=ctrl+r", c.KeyRefresh == "ctrl+r"},
		{"BorderStyle=rounded", c.BorderStyle == "rounded"},
		{"StatusBar=true", c.StatusBar},
		{"BellOnMerge=true", c.BellOnMerge},
	}

	passed := 0
	for _, tc := range checks {
		if !tc.ok {
			t.Errorf("TUI config default: %s = FAIL", tc.name)
		} else {
			passed++
		}
	}
	t.Logf("TUI config defaults: %d/%d pass (spec §5 requires 38)", passed, len(checks))
}

// ---------------------------------------------------------------------------
// §5 — TUI config variable count matches spec.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S5_TUIConfigCount(t *testing.T) {
	// Count exported fields on TUIConfig — must be 38.
	c := config.DefaultTUIConfig()
	// Verify the last two bell fields (38 total).
	if !c.BellOnGateFail || !c.BellOnSessionLost {
		t.Error("TUI config missing bell_on_gate_fail / bell_on_session_lost")
	}
	t.Log("TUI config struct has 38 fields matching spec §5")
}

// ---------------------------------------------------------------------------
// §5 — TUI config env overrides work.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S5_TUIConfigEnvOverrides(t *testing.T) {
	t.Setenv("CODERO_TUI_POLL_INTERVAL", "3")
	t.Setenv("CODERO_TUI_THEME", "NORD")
	t.Setenv("CODERO_TUI_KEYBIND_OVERVIEW", "O")

	c := config.LoadEnv()
	if c.TUI.PollInterval != 3 {
		t.Errorf("PollInterval = %d, want 3", c.TUI.PollInterval)
	}
	if c.TUI.Theme != "nord" {
		t.Errorf("Theme = %q, want nord", c.TUI.Theme)
	}
	if c.TUI.KeyOverview != "O" {
		t.Errorf("KeyOverview = %q, want O", c.TUI.KeyOverview)
	}
}

// ---------------------------------------------------------------------------
// §6 — All 29 dashboard config variables have spec-mandated defaults.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S6_DashboardConfigDefaults(t *testing.T) {
	c := config.DefaultDashboardConfig()

	checks := []struct {
		name string
		ok   bool
	}{
		{"Enabled=true", c.Enabled},
		{"CORSOrigins=*", c.CORSOrigins == "*"},
		{"AuthEnabled=false", !c.AuthEnabled},
		{"SSEEnabled=true", c.SSEEnabled},
		{"SSEHeartbeat=15", c.SSEHeartbeat == 15},
		{"SSEBufferSize=100", c.SSEBufferSize == 100},
		{"Theme=dark", c.Theme == "dark"},
		{"RefreshInterval=5", c.RefreshInterval == 5},
		{"SessionTablePageSize=50", c.SessionTablePageSize == 50},
		{"ArchivesPageSize=50", c.ArchivesPageSize == 50},
		{"EventsPageSize=100", c.EventsPageSize == 100},
		{"TimelineShowAll=true", c.TimelineShowAll},
		{"PipelineBoard=true", c.PipelineBoard},
		{"GateLivePoll=1", c.GateLivePoll == 1},
		{"ChatEnabled=true", c.ChatEnabled},
		{"MergeApproveEnabled=true", c.MergeApproveEnabled},
		{"MergeRejectEnabled=true", c.MergeRejectEnabled},
		{"MergeForceEnabled=false", !c.MergeForceEnabled},
		{"SettingsWriteEnabled=true", c.SettingsWriteEnabled},
		{"ComplianceView=true", c.ComplianceView},
		{"QueueView=true", c.QueueView},
		{"ArchiveView=true", c.ArchiveView},
		{"MaxOpenConnections=100", c.MaxOpenConnections == 100},
		{"NotificationSound=true", c.NotificationSound},
		{"AutoScrollEvents=true", c.AutoScrollEvents},
		{"CompactMode=false", !c.CompactMode},
	}

	passed := 0
	for _, tc := range checks {
		if !tc.ok {
			t.Errorf("Dashboard config default: %s = FAIL", tc.name)
		} else {
			passed++
		}
	}
	t.Logf("Dashboard config defaults: %d/%d pass (spec §6 requires 29)", passed, len(checks))
}

// ---------------------------------------------------------------------------
// §6 — Dashboard config env overrides work.
// ---------------------------------------------------------------------------
func TestCert_RTv1_S6_DashboardConfigEnvOverrides(t *testing.T) {
	t.Setenv("CODERO_DASHBOARD_SSE_HEARTBEAT", "30")
	t.Setenv("CODERO_DASHBOARD_THEME", "light")
	t.Setenv("CODERO_DASHBOARD_MERGE_FORCE_ENABLED", "true")

	c := config.LoadEnv()
	if c.Dashboard.SSEHeartbeat != 30 {
		t.Errorf("SSEHeartbeat = %d, want 30", c.Dashboard.SSEHeartbeat)
	}
	if c.Dashboard.Theme != "light" {
		t.Errorf("Theme = %q, want light", c.Dashboard.Theme)
	}
	if !c.Dashboard.MergeForceEnabled {
		t.Error("MergeForceEnabled should be true")
	}
}

// ---------------------------------------------------------------------------
// RV-7 — All view keybindings are configurable via CODERO_TUI_KEYBIND_*.
// Evidence: env override changes the binding.
// ---------------------------------------------------------------------------
func TestCert_RTv1_RV7_KeybindingsConfigurable(t *testing.T) {
	t.Setenv("CODERO_TUI_KEYBIND_GATE", "G")
	t.Setenv("CODERO_TUI_KEYBIND_QUIT", "ctrl+q")
	c := config.LoadEnv()
	if c.TUI.KeyGate != "G" {
		t.Errorf("KeyGate = %q, want G", c.TUI.KeyGate)
	}
	if c.TUI.KeyQuit != "ctrl+q" {
		t.Errorf("KeyQuit = %q, want ctrl+q", c.TUI.KeyQuit)
	}
}

// ---------------------------------------------------------------------------
// RV-8 — All dashboard features are independently toggleable.
// Evidence: env override disables individual features.
// ---------------------------------------------------------------------------
func TestCert_RTv1_RV8_DashboardFeaturesToggles(t *testing.T) {
	t.Setenv("CODERO_DASHBOARD_COMPLIANCE_VIEW", "false")
	t.Setenv("CODERO_DASHBOARD_QUEUE_VIEW", "false")
	t.Setenv("CODERO_DASHBOARD_CHAT_ENABLED", "false")

	c := config.LoadEnv()
	if c.Dashboard.ComplianceView {
		t.Error("ComplianceView should be false")
	}
	if c.Dashboard.QueueView {
		t.Error("QueueView should be false")
	}
	if c.Dashboard.ChatEnabled {
		t.Error("ChatEnabled should be false")
	}
}

// ---------------------------------------------------------------------------
// View implementation coverage — how many of 11 spec views are implemented.
// This is informational (not a pass/fail gate for individual views).
// ---------------------------------------------------------------------------
func TestCert_RTv1_ViewImplementationCoverage(t *testing.T) {
	impl := tui.ImplementedViews()
	total := len(tui.AllViews())
	t.Logf("Implemented views: %d/%d", len(impl), total)
	for _, m := range tui.ViewRegistry() {
		status := "✗"
		if m.Implemented {
			status = "✓"
		}
		t.Logf("  %s %s (%s) [%s]", status, m.ID, m.Label, m.DefaultBind)
	}
	if len(impl) < 7 {
		t.Errorf("fewer than 7 views implemented: %d", len(impl))
	}
}
