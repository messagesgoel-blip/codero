package gate_test

import (
	"strings"
	"testing"

	"github.com/codero/codero/internal/gate"
)

// --- StateIcon tests ---

func TestStateIcon_AllKnownStates(t *testing.T) {
	cases := []struct {
		state string
		want  string
	}{
		{"pending", "○"},
		{"running", "●"},
		{"pass", "✓"},
		{"blocked", "✗"},
		{"timeout", "⏱"},
		{"infra_fail", "!"},
	}
	for _, tc := range cases {
		icon := gate.StateIcon(tc.state)
		if icon != tc.want {
			t.Errorf("StateIcon(%q): got %q, want %q", tc.state, icon, tc.want)
		}
	}
}

func TestStateIcon_Unknown(t *testing.T) {
	icon := gate.StateIcon("not_a_real_state")
	if icon != "?" {
		t.Errorf("unknown state icon: got %q, want ?", icon)
	}
}

// --- RenderBar snapshot tests ---

// TestRenderBar_AllPending verifies the initial state display.
func TestRenderBar_AllPending(t *testing.T) {
	bar := gate.RenderBar("pending", "pending", "none")
	if !strings.Contains(bar, "copilot:pending") {
		t.Errorf("expected copilot:pending in bar, got: %s", bar)
	}
	if !strings.Contains(bar, "litellm:pending") {
		t.Errorf("expected litellm:pending in bar, got: %s", bar)
	}
}

// TestRenderBar_CopilotActive verifies running icon on active gate.
func TestRenderBar_CopilotActive(t *testing.T) {
	bar := gate.RenderBar("running", "pending", "copilot")
	if !strings.Contains(bar, "●") {
		t.Errorf("expected running dot ● for active copilot gate, got: %s", bar)
	}
}

// TestRenderBar_LiteLLMActive verifies running icon on active LiteLLM gate.
func TestRenderBar_LiteLLMActive(t *testing.T) {
	bar := gate.RenderBar("pass", "running", "litellm")
	// copilot should show pass icon; litellm should show active running icon
	if !strings.Contains(bar, "✓") {
		t.Errorf("expected pass icon ✓ for copilot, got: %s", bar)
	}
	if !strings.Contains(bar, "●") {
		t.Errorf("expected running dot ● for active litellm gate, got: %s", bar)
	}
}

// TestRenderBar_BothPassed verifies pass state.
func TestRenderBar_BothPassed(t *testing.T) {
	bar := gate.RenderBar("pass", "pass", "none")
	if strings.Count(bar, "✓") < 2 {
		t.Errorf("expected two pass icons ✓, got: %s", bar)
	}
}

// TestRenderBar_CopilotBlocked verifies blocked state.
func TestRenderBar_CopilotBlocked(t *testing.T) {
	bar := gate.RenderBar("blocked", "pass", "none")
	if !strings.Contains(bar, "✗") {
		t.Errorf("expected blocked icon ✗, got: %s", bar)
	}
}

// TestRenderBar_CopilotInfraFail verifies infra_fail is non-blocking and shows !.
func TestRenderBar_CopilotInfraFail(t *testing.T) {
	bar := gate.RenderBar("infra_fail", "pass", "none")
	if !strings.Contains(bar, "!") {
		t.Errorf("expected infra_fail icon !, got: %s", bar)
	}
}

// TestRenderBar_Timeout verifies timeout state.
func TestRenderBar_Timeout(t *testing.T) {
	bar := gate.RenderBar("timeout", "pending", "none")
	if !strings.Contains(bar, "⏱") {
		t.Errorf("expected timeout icon ⏱, got: %s", bar)
	}
}

// --- FormatProgressLine tests ---

func TestFormatProgressLine_WithBar(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusPending,
		ProgressBar:   "[o copilot:pending] [o litellm:pending]",
		ElapsedSec:    12,
		CopilotStatus: "pending",
		LiteLLMStatus: "pending",
	}
	line := gate.FormatProgressLine(r)
	if !strings.Contains(line, "[o copilot:pending]") {
		t.Errorf("progress line missing bar content: %q", line)
	}
	if !strings.Contains(line, "12s elapsed") {
		t.Errorf("progress line missing elapsed: %q", line)
	}
}

func TestFormatProgressLine_NoBar_Fallback(t *testing.T) {
	// When ProgressBar is empty, RenderBar should be used as fallback.
	r := gate.Result{
		Status:        gate.StatusPending,
		ProgressBar:   "",
		CopilotStatus: "running",
		LiteLLMStatus: "pending",
		CurrentGate:   "copilot",
	}
	line := gate.FormatProgressLine(r)
	if !strings.Contains(line, "copilot") {
		t.Errorf("fallback bar should contain copilot, got: %q", line)
	}
}

// --- FormatSummary tests ---

func TestFormatSummary_Pass(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusPass,
		RunID:         "run-123",
		ProgressBar:   "[+ copilot:pass] [+ litellm:pass]",
		CopilotStatus: "pass",
		LiteLLMStatus: "pass",
	}
	summary := gate.FormatSummary(r)
	if !strings.Contains(summary, "RunID: run-123") {
		t.Errorf("summary missing RunID, got: %q", summary)
	}
	if !strings.Contains(summary, "[+") {
		t.Errorf("summary missing pass icon, got: %q", summary)
	}
	if strings.Contains(summary, "Blockers") {
		t.Errorf("no blockers expected on PASS, got: %q", summary)
	}
}

func TestFormatSummary_FailWithBlockers(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusFail,
		RunID:         "run-456",
		CopilotStatus: "blocked",
		LiteLLMStatus: "pass",
		Comments:      []string{"BLOCK: missing auth check", "BLOCK: untested error path"},
	}
	summary := gate.FormatSummary(r)
	if !strings.Contains(summary, "Blockers") {
		t.Errorf("summary should contain Blockers section, got: %q", summary)
	}
	if !strings.Contains(summary, "BLOCK: missing auth check") {
		t.Errorf("summary missing first blocker, got: %q", summary)
	}
}
