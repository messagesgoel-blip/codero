package tui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/tui"
	"github.com/codero/codero/internal/tui/adapters"
)

// ── GatePane tests ────────────────────────────────────────────────────────────

func makeGateVM(copilot, litellm, current string) adapters.GateViewModel {
	return adapters.FromGateResult(gate.Result{
		Status:        gate.StatusPending,
		CopilotStatus: copilot,
		LiteLLMStatus: litellm,
		CurrentGate:   current,
		ElapsedSec:    30,
		PollAfterSec:  180,
	})
}

func TestGatePane_View_ShowsAgentNames(t *testing.T) {
	p := tui.NewGatePane(tui.DefaultTheme)
	p.SetSize(40, 30)
	p.SetVM(makeGateVM("running", "pending", "copilot"))

	view := p.View()
	if !strings.Contains(view, "copilot") {
		t.Error("GatePane should contain 'copilot'")
	}
	if !strings.Contains(view, "litellm") {
		t.Error("GatePane should contain 'litellm'")
	}
}

func TestGatePane_View_ShowsProcessesHeader(t *testing.T) {
	p := tui.NewGatePane(tui.DefaultTheme)
	p.SetSize(40, 30)
	p.SetVM(makeGateVM("pending", "pending", ""))

	view := p.View()
	if !strings.Contains(view, "PROCESSES & AGENTS") {
		t.Error("GatePane header should contain 'PROCESSES & AGENTS'")
	}
}

func TestGatePane_View_ShowsSystemHealth(t *testing.T) {
	p := tui.NewGatePane(tui.DefaultTheme)
	p.SetSize(60, 30)
	p.SetVM(makeGateVM("pass", "pass", ""))

	view := p.View()
	if !strings.Contains(view, "System Health") {
		t.Error("GatePane should show System Health indicator")
	}
}

func TestGatePane_View_ShowsProgressBar(t *testing.T) {
	p := tui.NewGatePane(tui.DefaultTheme)
	p.SetSize(50, 30)
	p.SetVM(makeGateVM("pass", "pending", ""))

	view := p.View()
	// Progress bar should contain block characters.
	if !strings.Contains(view, "█") && !strings.Contains(view, "░") {
		t.Error("GatePane should show progress bar characters")
	}
}

func TestGatePane_View_EmptyAtZeroWidth(t *testing.T) {
	p := tui.NewGatePane(tui.DefaultTheme)
	p.SetSize(0, 30)
	p.SetVM(makeGateVM("pending", "pending", ""))

	if p.View() != "" {
		t.Error("GatePane with width=0 should return empty string")
	}
}

func TestGatePane_View_RelayOrchestration_WhenEnoughHeight(t *testing.T) {
	p := tui.NewGatePane(tui.DefaultTheme)
	p.SetSize(50, 40)
	vm := makeGateVM("pending", "pending", "")
	p.SetVM(vm)

	view := p.View()
	if !strings.Contains(view, "RELAY ORCHESTRATION") {
		t.Error("GatePane should show RELAY ORCHESTRATION when height is sufficient")
	}
}

// ── ChecksPane tests ──────────────────────────────────────────────────────────

func makeChecksVM() adapters.CheckReportViewModel {
	return adapters.FromCheckReport(gatecheck.Report{
		Summary: gatecheck.Summary{
			OverallStatus:  gatecheck.StatusFail,
			Passed:         1,
			Failed:         2,
			Disabled:       1,
			Total:          4,
			RequiredFailed: 1,
			Profile:        gatecheck.ProfilePortable,
			SchemaVersion:  gatecheck.SchemaVersion,
		},
		Checks: []gatecheck.CheckResult{
			{ID: "fmt-check", Group: gatecheck.GroupFormat, Status: gatecheck.StatusPass},
			{ID: "auth-check", Group: gatecheck.GroupSecurity, Status: gatecheck.StatusFail, Required: true, ReasonCode: gatecheck.ReasonCheckFailed},
			{ID: "xss-check", Group: gatecheck.GroupSecurity, Status: gatecheck.StatusFail, Required: false},
			{ID: "gitleaks", Group: gatecheck.GroupSecurity, Status: gatecheck.StatusDisabled, ReasonCode: gatecheck.ReasonMissingTool},
		},
		RunAt: time.Unix(0, 0).UTC(),
	})
}

func TestChecksPane_View_ShowsFindingsHeader(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(50, 40)
	p.SetVM(makeChecksVM())

	view := p.View()
	if !strings.Contains(view, "FINDINGS & ROUTING DASHBOARD") {
		t.Error("ChecksPane should show FINDINGS & ROUTING DASHBOARD header")
	}
}

func TestChecksPane_View_ShowsSeverityBuckets(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(50, 40)
	p.SetVM(makeChecksVM())

	view := p.View()
	for _, want := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		if !strings.Contains(view, want) {
			t.Errorf("ChecksPane should show %q severity bucket", want)
		}
	}
}

func TestChecksPane_View_ShowsRoutingFlowchart(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(50, 40)
	p.SetVM(makeChecksVM())

	view := p.View()
	if !strings.Contains(view, "ROUTING FLOWCHART") {
		t.Error("ChecksPane should show ROUTING FLOWCHART section")
	}
	if !strings.Contains(view, "AI Agent Review") {
		t.Error("ChecksPane flowchart should contain 'AI Agent Review'")
	}
}

func TestChecksPane_View_ShowsSummary(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(50, 40)
	p.SetVM(makeChecksVM())

	view := p.View()
	if !strings.Contains(view, "Summary") {
		t.Error("ChecksPane should show Summary section")
	}
	if !strings.Contains(view, "Findings Found") {
		t.Error("ChecksPane Summary should show Findings Found")
	}
	if !strings.Contains(view, "Risk Score") {
		t.Error("ChecksPane Summary should show Risk Score")
	}
}

func TestChecksPane_View_CriticalBucketForRequiredFail(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(60, 40)
	p.SetVM(makeChecksVM())

	view := p.View()
	// auth-check is failing + required → CRITICAL bucket
	if !strings.Contains(view, "auth-check") {
		t.Error("auth-check (failing + required) should appear in CRITICAL bucket")
	}
}

func TestChecksPane_View_HighBucketForNonRequiredFail(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(60, 40)
	p.SetVM(makeChecksVM())

	view := p.View()
	// xss-check is failing + not required → HIGH bucket
	if !strings.Contains(view, "xss-check") {
		t.Error("xss-check (failing + not required) should appear in HIGH bucket")
	}
}

func TestChecksPane_View_NarrowLayout(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	// Set width below narrow threshold (60)
	p.SetSize(50, 40)
	p.SetVM(makeChecksVM())

	view := p.View()
	// The narrow layout should still show the findings header and buckets.
	if !strings.Contains(view, "FINDINGS & ROUTING DASHBOARD") {
		t.Error("narrow ChecksPane should still show findings header")
	}
}

func TestChecksPane_View_WideLayout(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	// Set width well above narrow threshold
	p.SetSize(100, 40)
	p.SetVM(makeChecksVM())

	view := p.View()
	if !strings.Contains(view, "FINDINGS & ROUTING DASHBOARD") {
		t.Error("wide ChecksPane should show findings header")
	}
}

func TestChecksPane_View_EmptyAtZeroWidth(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(0, 40)
	p.SetVM(makeChecksVM())

	if p.View() != "" {
		t.Error("ChecksPane with width=0 should return empty string")
	}
}

func TestChecksPane_View_EmptyVM(t *testing.T) {
	p := tui.NewChecksPane(tui.DefaultTheme)
	p.SetSize(50, 40)
	// No SetVM call — default empty view model.

	view := p.View()
	// Should still render without panic.
	if !strings.Contains(view, "FINDINGS & ROUTING DASHBOARD") {
		t.Error("empty ChecksPane should render header without panic")
	}
}
