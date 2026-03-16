package adapters_test

import (
	"testing"

	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/tui/adapters"
)

func TestParseProgressEnv_Full(t *testing.T) {
	content := `STATUS=PASS
RUN_ID=abc123
ELAPSED_SEC=42
POLL_AFTER_SEC=30
PROGRESS_BAR=[✓ copilot:pass] [✓ litellm:pass]
CURRENT_GATE=litellm
COPILOT_STATUS=pass
LITELLM_STATUS=pass
COMMENTS=all good
`
	r := adapters.ParseProgressEnv(content)
	if r.Status != gate.StatusPass {
		t.Errorf("expected StatusPass, got %q", r.Status)
	}
	if r.RunID != "abc123" {
		t.Errorf("expected RunID abc123, got %q", r.RunID)
	}
	if r.ElapsedSec != 42 {
		t.Errorf("expected ElapsedSec=42, got %d", r.ElapsedSec)
	}
	if r.CopilotStatus != "pass" {
		t.Errorf("expected copilot=pass, got %q", r.CopilotStatus)
	}
}

func TestParseProgressEnv_Defaults(t *testing.T) {
	r := adapters.ParseProgressEnv("")
	if r.Status != gate.StatusPending {
		t.Errorf("expected pending, got %q", r.Status)
	}
	if r.CopilotStatus != "pending" {
		t.Errorf("expected copilot pending, got %q", r.CopilotStatus)
	}
	if r.LiteLLMStatus != "pending" {
		t.Errorf("expected litellm pending, got %q", r.LiteLLMStatus)
	}
}

func TestParseProgressEnv_Fail(t *testing.T) {
	content := "STATUS=FAIL\nCOMMENTS=blocker1|blocker2\n"
	r := adapters.ParseProgressEnv(content)
	if r.Status != gate.StatusFail {
		t.Errorf("expected fail, got %q", r.Status)
	}
	if len(r.Comments) != 2 {
		t.Errorf("expected 2 comments, got %d: %v", len(r.Comments), r.Comments)
	}
}

func TestFromGateResult_Pass(t *testing.T) {
	r := gate.Result{
		Status:        gate.StatusPass,
		CopilotStatus: "pass",
		LiteLLMStatus: "pass",
	}
	vm := adapters.FromGateResult(r)
	if vm.StatusLabel != "PASS" {
		t.Errorf("expected PASS, got %q", vm.StatusLabel)
	}
	if vm.StatusIcon != "✓" {
		t.Errorf("expected ✓, got %q", vm.StatusIcon)
	}
	if !vm.IsFinal {
		t.Error("expected IsFinal=true for PASS")
	}
}

func TestFromGateResult_Fail(t *testing.T) {
	r := gate.Result{Status: gate.StatusFail}
	vm := adapters.FromGateResult(r)
	if vm.StatusLabel != "FAIL" {
		t.Errorf("expected FAIL, got %q", vm.StatusLabel)
	}
	if vm.StatusIcon != "✗" {
		t.Errorf("expected ✗, got %q", vm.StatusIcon)
	}
	if !vm.IsFinal {
		t.Error("expected IsFinal=true for FAIL")
	}
}

func TestFromGateResult_Pending(t *testing.T) {
	r := gate.Result{Status: gate.StatusPending}
	vm := adapters.FromGateResult(r)
	if vm.StatusLabel != "PENDING" {
		t.Errorf("expected PENDING, got %q", vm.StatusLabel)
	}
	if vm.IsFinal {
		t.Error("expected IsFinal=false for PENDING")
	}
}

func TestFromGateResult_ProgressBarParity(t *testing.T) {
	r := gate.Result{
		CopilotStatus: "pass",
		LiteLLMStatus: "running",
		CurrentGate:   "litellm",
	}
	want := gate.RenderBar(r.CopilotStatus, r.LiteLLMStatus, r.CurrentGate)
	vm := adapters.FromGateResult(r)
	if vm.ProgressBar != want {
		t.Errorf("progress bar parity: want %q, got %q", want, vm.ProgressBar)
	}
}

func TestFromProgressEnv_NonExistent(t *testing.T) {
	vm := adapters.FromProgressEnv("/nonexistent/path/that/does/not/exist")
	if vm.Status != gate.StatusPending {
		t.Errorf("expected pending for missing file, got %q", vm.Status)
	}
	if vm.IsFinal {
		t.Error("expected IsFinal=false for missing file")
	}
}

func TestElapsedLabel(t *testing.T) {
	tests := []struct {
		sec  int
		want string
	}{
		{0, "—"},
		{-1, "—"},
		{30, "30s"},
		{60, "1m0s"},
		{90, "1m30s"},
	}
	for _, tc := range tests {
		got := adapters.ElapsedLabel(tc.sec)
		if got != tc.want {
			t.Errorf("ElapsedLabel(%d): want %q, got %q", tc.sec, tc.want, got)
		}
	}
}
