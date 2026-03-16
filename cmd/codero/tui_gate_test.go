package main

import (
	"strings"
	"testing"

	"github.com/codero/codero/internal/gate"
)

// --- GateStateToPrecommitStatus tests ---

func TestGateStateToPrecommitStatus_Pass(t *testing.T) {
	if got := GateStateToPrecommitStatus("pass"); got != "passed" {
		t.Errorf("pass → %q, want passed", got)
	}
}

func TestGateStateToPrecommitStatus_Blocked(t *testing.T) {
	if got := GateStateToPrecommitStatus("blocked"); got != "failed" {
		t.Errorf("blocked → %q, want failed", got)
	}
}

func TestGateStateToPrecommitStatus_Timeout(t *testing.T) {
	if got := GateStateToPrecommitStatus("timeout"); got != "failed" {
		t.Errorf("timeout → %q, want failed", got)
	}
}

func TestGateStateToPrecommitStatus_InfraFail(t *testing.T) {
	// infra_fail is non-blocking; maps to error, not failed.
	if got := GateStateToPrecommitStatus("infra_fail"); got != "error" {
		t.Errorf("infra_fail → %q, want error", got)
	}
}

func TestGateStateToPrecommitStatus_Pending(t *testing.T) {
	// pending should not appear at terminal state, but maps to error defensively.
	if got := GateStateToPrecommitStatus("pending"); got != "error" {
		t.Errorf("pending → %q, want error", got)
	}
}

func TestGateStateToPrecommitStatus_Empty(t *testing.T) {
	if got := GateStateToPrecommitStatus(""); got != "error" {
		t.Errorf("empty → %q, want error", got)
	}
}

func TestGateStateToPrecommitStatus_Unknown(t *testing.T) {
	if got := GateStateToPrecommitStatus("not_a_state"); got != "error" {
		t.Errorf("unknown → %q, want error", got)
	}
}

// --- parseEnvToResult tests ---

func TestParseEnvToResult_Pass(t *testing.T) {
	env := `STATUS=PASS
RUN_ID=20260316-abc123
PROGRESS_BAR=[+ copilot:pass] [+ litellm:pass]
CURRENT_GATE=none
COPILOT_STATUS=pass
LITELLM_STATUS=pass
ELAPSED_SEC=12
POLL_AFTER_SEC=180
COMMENTS=none
`
	r := parseEnvToResult(env)

	if r.Status != gate.StatusPass {
		t.Errorf("Status: got %q, want PASS", r.Status)
	}
	if r.RunID != "20260316-abc123" {
		t.Errorf("RunID: got %q", r.RunID)
	}
	if r.CopilotStatus != "pass" {
		t.Errorf("CopilotStatus: got %q, want pass", r.CopilotStatus)
	}
	if r.LiteLLMStatus != "pass" {
		t.Errorf("LiteLLMStatus: got %q, want pass", r.LiteLLMStatus)
	}
	if r.ElapsedSec != 12 {
		t.Errorf("ElapsedSec: got %d, want 12", r.ElapsedSec)
	}
	if r.PollAfterSec != 180 {
		t.Errorf("PollAfterSec: got %d, want 180", r.PollAfterSec)
	}
	if len(r.Comments) != 0 {
		t.Errorf("Comments: got %v, want empty (COMMENTS=none)", r.Comments)
	}
	if !r.IsFinal() {
		t.Error("PASS should be final")
	}
}

func TestParseEnvToResult_Fail(t *testing.T) {
	env := `STATUS=FAIL
RUN_ID=20260316-xyz
COPILOT_STATUS=blocked
LITELLM_STATUS=pass
COMMENTS=BLOCK: missing auth check|BLOCK: untested error path
`
	r := parseEnvToResult(env)

	if r.Status != gate.StatusFail {
		t.Errorf("Status: got %q, want FAIL", r.Status)
	}
	if r.CopilotStatus != "blocked" {
		t.Errorf("CopilotStatus: got %q, want blocked", r.CopilotStatus)
	}
	if len(r.Comments) != 2 {
		t.Errorf("Comments: got %d, want 2; comments = %v", len(r.Comments), r.Comments)
	}
}

func TestParseEnvToResult_Pending(t *testing.T) {
	env := `STATUS=PENDING
RUN_ID=run-pending
COPILOT_STATUS=running
LITELLM_STATUS=pending
CURRENT_GATE=copilot
`
	r := parseEnvToResult(env)

	if r.Status != gate.StatusPending {
		t.Errorf("Status: got %q, want PENDING", r.Status)
	}
	if r.CurrentGate != "copilot" {
		t.Errorf("CurrentGate: got %q, want copilot", r.CurrentGate)
	}
	if r.IsFinal() {
		t.Error("PENDING should not be final")
	}
}

func TestParseEnvToResult_NoFile(t *testing.T) {
	// Empty content should default to PENDING with pending statuses.
	r := parseEnvToResult("")
	if r.Status != gate.StatusPending {
		t.Errorf("empty content: Status got %q, want PENDING", r.Status)
	}
	if r.CopilotStatus != "pending" {
		t.Errorf("empty content: CopilotStatus got %q, want pending", r.CopilotStatus)
	}
	if r.LiteLLMStatus != "pending" {
		t.Errorf("empty content: LiteLLMStatus got %q, want pending", r.LiteLLMStatus)
	}
}

func TestParseEnvToResult_UnknownStatus(t *testing.T) {
	r := parseEnvToResult("STATUS=RUNNING\nCOPILOT_STATUS=running\n")
	// Unknown status should be treated as PENDING.
	if r.Status != gate.StatusPending {
		t.Errorf("unknown STATUS: got %q, want PENDING", r.Status)
	}
}

func TestParseEnvToResult_InvalidElapsedAndPollFallbackToZero(t *testing.T) {
	env := "STATUS=PENDING\nELAPSED_SEC=abc\nPOLL_AFTER_SEC=xyz\n"
	r := parseEnvToResult(env)
	if r.ElapsedSec != 0 {
		t.Errorf("ElapsedSec: got %d, want 0 on parse error", r.ElapsedSec)
	}
	if r.PollAfterSec != 0 {
		t.Errorf("PollAfterSec: got %d, want 0 on parse error", r.PollAfterSec)
	}
}

// --- RenderGateStatusBox tests ---

func TestRenderGateStatusBox_PassContainsBarAndStatus(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusPass,
		RunID:         "run-pass-123",
		CopilotStatus: "pass",
		LiteLLMStatus: "pass",
		CurrentGate:   "none",
	}
	box := RenderGateStatusBox(r, t.TempDir())

	if !strings.Contains(box, "PASS") {
		t.Errorf("box should contain PASS, got:\n%s", box)
	}
	if !strings.Contains(box, "copilot:pass") || !strings.Contains(box, "litellm:pass") {
		t.Errorf("box should contain gate statuses, got:\n%s", box)
	}
	if !strings.Contains(box, "run-pass-123") {
		t.Errorf("box should contain RunID, got:\n%s", box)
	}
	if !strings.Contains(box, "✅") {
		t.Errorf("box should contain ✅ for PASS, got:\n%s", box)
	}
}

func TestRenderGateStatusBox_FailContainsBlockers(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusFail,
		RunID:         "run-fail-456",
		CopilotStatus: "blocked",
		LiteLLMStatus: "pass",
		Comments:      []string{"BLOCK: missing auth check", "BLOCK: error path not tested"},
	}
	box := RenderGateStatusBox(r, "")

	if !strings.Contains(box, "FAIL") {
		t.Errorf("box should contain FAIL, got:\n%s", box)
	}
	if !strings.Contains(box, "BLOCK: missing auth check") {
		t.Errorf("box should contain first blocker, got:\n%s", box)
	}
	if !strings.Contains(box, "BLOCK: error path not tested") {
		t.Errorf("box should contain second blocker, got:\n%s", box)
	}
	if !strings.Contains(box, "Blockers") {
		t.Errorf("box should contain Blockers label, got:\n%s", box)
	}
}

func TestRenderGateStatusBox_PendingNoBlockers(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusPending,
		CopilotStatus: "running",
		LiteLLMStatus: "pending",
		CurrentGate:   "copilot",
	}
	box := RenderGateStatusBox(r, "")

	if !strings.Contains(box, "PENDING") {
		t.Errorf("box should contain PENDING, got:\n%s", box)
	}
	if strings.Contains(box, "Blockers") {
		t.Errorf("PENDING box should not contain Blockers section, got:\n%s", box)
	}
}

func TestRenderGateStatusBox_BarMatchesRenderBar(t *testing.T) {
	// When ProgressBar is empty, RenderGateStatusBox calls gate.RenderBar.
	// The resulting box must contain identical output to gate.RenderBar.
	r := gate.Result{
		Status:        gate.StatusPass,
		CopilotStatus: "pass",
		LiteLLMStatus: "pass",
		CurrentGate:   "none",
	}
	expectedBar := gate.RenderBar("pass", "pass", "none")
	box := RenderGateStatusBox(r, "")

	if !strings.Contains(box, expectedBar) {
		t.Errorf("TUI box bar does not match gate.RenderBar output.\nExpected bar: %q\nBox:\n%s", expectedBar, box)
	}
}

func TestRenderGateStatusBox_BoxStructure(t *testing.T) {
	r := gate.Result{Status: gate.StatusPending, CopilotStatus: "pending", LiteLLMStatus: "pending"}
	box := RenderGateStatusBox(r, "")

	// Should have top and bottom borders.
	if !strings.Contains(box, "┌") || !strings.Contains(box, "┐") {
		t.Error("box missing top border")
	}
	if !strings.Contains(box, "└") || !strings.Contains(box, "┘") {
		t.Error("box missing bottom border")
	}
}

// --- truncate helper tests ---

func TestTruncate_ShortString(t *testing.T) {
	if got := truncate("short", 20); got != "short" {
		t.Errorf("short string: got %q, want short", got)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	if got := truncate("exact", 5); got != "exact" {
		t.Errorf("exact length: got %q, want exact", got)
	}
}

func TestTruncate_LongString(t *testing.T) {
	got := truncate("this is a long string that exceeds the limit", 15)
	if len([]rune(got)) > 15 {
		t.Errorf("truncated string too long: %q (len %d)", got, len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated string should end with …, got %q", got)
	}
}

// --- gateStatusDisplay tests ---

func TestGateStatusDisplay_Pass(t *testing.T) {
	label, icon, color := gateStatusDisplay(gate.StatusPass)
	if label != "PASS" {
		t.Errorf("label: got %q, want PASS", label)
	}
	if !strings.Contains(icon, "✅") {
		t.Errorf("icon: got %q, expected ✅", icon)
	}
	if color == "" {
		t.Error("color should not be empty for PASS")
	}
}

func TestGateStatusDisplay_Fail(t *testing.T) {
	label, icon, color := gateStatusDisplay(gate.StatusFail)
	if label != "FAIL" {
		t.Errorf("label: got %q, want FAIL", label)
	}
	_ = icon
	if color == "" {
		t.Error("color should not be empty for FAIL")
	}
}

func TestGateStatusDisplay_Pending(t *testing.T) {
	label, _, _ := gateStatusDisplay(gate.StatusPending)
	if label != "PENDING" {
		t.Errorf("label: got %q, want PENDING", label)
	}
}
