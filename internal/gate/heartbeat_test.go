package gate_test

import (
	"strings"
	"testing"

	"github.com/codero/codero/internal/gate"
)

// --- ParseOutput tests ---

func TestParseOutput_Pending(t *testing.T) {
	input := `STATUS: PENDING
RUN_ID: 20260316-010000-1234
ELAPSED_SEC: 5
POLL_AFTER_SEC: 180
PROGRESS_BAR: [o copilot:pending] [o litellm:pending]
CURRENT_GATE: none
COPILOT_STATUS: pending
LITELLM_STATUS: pending
`
	r := gate.ParseOutput(input)

	if r.Status != gate.StatusPending {
		t.Errorf("Status: got %q, want %q", r.Status, gate.StatusPending)
	}
	if r.RunID != "20260316-010000-1234" {
		t.Errorf("RunID: got %q", r.RunID)
	}
	if r.ElapsedSec != 5 {
		t.Errorf("ElapsedSec: got %d, want 5", r.ElapsedSec)
	}
	if r.PollAfterSec != 180 {
		t.Errorf("PollAfterSec: got %d, want 180", r.PollAfterSec)
	}
	if r.CurrentGate != "none" {
		t.Errorf("CurrentGate: got %q, want none", r.CurrentGate)
	}
	if r.CopilotStatus != "pending" {
		t.Errorf("CopilotStatus: got %q, want pending", r.CopilotStatus)
	}
	if r.LiteLLMStatus != "pending" {
		t.Errorf("LiteLLMStatus: got %q, want pending", r.LiteLLMStatus)
	}
	if r.IsFinal() {
		t.Error("IsFinal: PENDING should not be final")
	}
}

func TestParseOutput_Pass(t *testing.T) {
	input := `STATUS: PASS
RUN_ID: 20260316-010000-5678
PROGRESS_BAR: [+ copilot:pass] [+ litellm:pass]
CURRENT_GATE: none
COPILOT_STATUS: pass
LITELLM_STATUS: pass
COMMENTS: none
`
	r := gate.ParseOutput(input)

	if r.Status != gate.StatusPass {
		t.Errorf("Status: got %q, want %q", r.Status, gate.StatusPass)
	}
	if !r.IsFinal() {
		t.Error("IsFinal: PASS should be final")
	}
	if len(r.Comments) != 0 {
		t.Errorf("Comments: got %d, want 0", len(r.Comments))
	}
}

func TestParseOutput_FailWithComments(t *testing.T) {
	input := `STATUS: FAIL
RUN_ID: 20260316-010000-9999
PROGRESS_BAR: [x copilot:blocked] [+ litellm:pass]
CURRENT_GATE: none
COPILOT_STATUS: blocked
LITELLM_STATUS: pass
COMMENTS:
BLOCK: found 3 findings in auth.go
BLOCK: missing error handling in handler.go
`
	r := gate.ParseOutput(input)

	if r.Status != gate.StatusFail {
		t.Errorf("Status: got %q, want %q", r.Status, gate.StatusFail)
	}
	if !r.IsFinal() {
		t.Error("IsFinal: FAIL should be final")
	}
	if len(r.Comments) < 2 {
		t.Errorf("Comments: got %d, want >= 2", len(r.Comments))
	}
	if !strings.Contains(r.Comments[0], "BLOCK") {
		t.Errorf("Comments[0]: expected BLOCK prefix, got %q", r.Comments[0])
	}
}

func TestParseOutput_FailWithInlineComment(t *testing.T) {
	// COMMENTS: with a value on the same line
	input := `STATUS: FAIL
RUN_ID: run-abc
COPILOT_STATUS: blocked
LITELLM_STATUS: pass
COMMENTS: BLOCK: inline blocker
`
	r := gate.ParseOutput(input)

	if r.Status != gate.StatusFail {
		t.Errorf("Status: got %q, want %q", r.Status, gate.StatusFail)
	}
	if len(r.Comments) == 0 {
		t.Fatal("expected at least one comment")
	}
	if !strings.Contains(r.Comments[0], "BLOCK") {
		t.Errorf("first comment should contain BLOCK, got %q", r.Comments[0])
	}
}

func TestParseOutput_InfraFail(t *testing.T) {
	input := `STATUS: PASS
RUN_ID: infra-run
PROGRESS_BAR: [! copilot:infra_fail] [+ litellm:pass]
COPILOT_STATUS: infra_fail
LITELLM_STATUS: pass
COMMENTS: none
`
	r := gate.ParseOutput(input)

	if r.Status != gate.StatusPass {
		t.Errorf("Status: got %q, want PASS (infra_fail is non-blocking)", r.Status)
	}
	if r.CopilotStatus != "infra_fail" {
		t.Errorf("CopilotStatus: got %q, want infra_fail", r.CopilotStatus)
	}
}

func TestParseOutput_PartialOutput(t *testing.T) {
	// Partial output should not panic and should set sensible defaults.
	r := gate.ParseOutput("STATUS: PENDING\n")
	if r.Status != gate.StatusPending {
		t.Errorf("Status: got %q", r.Status)
	}
	if r.CopilotStatus != "" {
		t.Errorf("CopilotStatus should be empty for partial output")
	}
}

func TestParseOutput_Empty(t *testing.T) {
	r := gate.ParseOutput("")
	if r.Status != gate.StatusPending {
		t.Errorf("empty output should default to PENDING, got %q", r.Status)
	}
}

// --- LoadConfig tests ---

func TestLoadConfig_Defaults(t *testing.T) {
	// Unset all gate env vars to test defaults.
	vars := []string{
		"CODERO_GATE_HEARTBEAT_BIN",
		"CODERO_AI_GATE_FALLBACK",
		"CODERO_COPILOT_TIMEOUT_SEC",
		"CODERO_LITELLM_TIMEOUT_SEC",
		"CODERO_GATE_TOTAL_TIMEOUT_SEC",
		"CODERO_GATE_POLL_INTERVAL_SEC",
		"CODERO_REPO_PATH",
	}
	for _, v := range vars {
		t.Setenv(v, "")
	}

	cfg := gate.LoadConfig()

	if cfg.HeartbeatBin != gate.DefaultHeartbeatBin {
		t.Errorf("HeartbeatBin default: got %q", cfg.HeartbeatBin)
	}
	if cfg.FallbackMode != gate.DefaultFallbackMode {
		t.Errorf("FallbackMode default: got %q, want %q", cfg.FallbackMode, gate.DefaultFallbackMode)
	}
	if cfg.CopilotTimeoutSec != gate.DefaultCopilotTimeoutSec {
		t.Errorf("CopilotTimeoutSec default: got %d, want %d", cfg.CopilotTimeoutSec, gate.DefaultCopilotTimeoutSec)
	}
	if cfg.LiteLLMTimeoutSec != gate.DefaultLiteLLMTimeoutSec {
		t.Errorf("LiteLLMTimeoutSec default: got %d, want %d", cfg.LiteLLMTimeoutSec, gate.DefaultLiteLLMTimeoutSec)
	}
	if cfg.GateTotalTimeoutSec != gate.DefaultGateTotalTimeoutSec {
		t.Errorf("GateTotalTimeoutSec default: got %d, want %d", cfg.GateTotalTimeoutSec, gate.DefaultGateTotalTimeoutSec)
	}
	if cfg.PollIntervalSec != gate.DefaultPollIntervalSec {
		t.Errorf("PollIntervalSec default: got %d, want %d", cfg.PollIntervalSec, gate.DefaultPollIntervalSec)
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	t.Setenv("CODERO_GATE_HEARTBEAT_BIN", "/custom/gate-heartbeat")
	t.Setenv("CODERO_COPILOT_TIMEOUT_SEC", "30")
	t.Setenv("CODERO_LITELLM_TIMEOUT_SEC", "90")
	t.Setenv("CODERO_GATE_TOTAL_TIMEOUT_SEC", "300")
	t.Setenv("CODERO_GATE_POLL_INTERVAL_SEC", "60")
	t.Setenv("CODERO_REPO_PATH", "/my/repo")

	cfg := gate.LoadConfig()

	if cfg.HeartbeatBin != "/custom/gate-heartbeat" {
		t.Errorf("HeartbeatBin: got %q", cfg.HeartbeatBin)
	}
	if cfg.CopilotTimeoutSec != 30 {
		t.Errorf("CopilotTimeoutSec: got %d, want 30", cfg.CopilotTimeoutSec)
	}
	if cfg.LiteLLMTimeoutSec != 90 {
		t.Errorf("LiteLLMTimeoutSec: got %d, want 90", cfg.LiteLLMTimeoutSec)
	}
	if cfg.GateTotalTimeoutSec != 300 {
		t.Errorf("GateTotalTimeoutSec: got %d, want 300", cfg.GateTotalTimeoutSec)
	}
	if cfg.PollIntervalSec != 60 {
		t.Errorf("PollIntervalSec: got %d, want 60", cfg.PollIntervalSec)
	}
	if cfg.RepoPath != "/my/repo" {
		t.Errorf("RepoPath: got %q", cfg.RepoPath)
	}
}

// TestLoadConfig_TimeoutIndependence verifies that each gate timeout env var
// is read independently — setting one does not affect the others.
func TestLoadConfig_TimeoutIndependence(t *testing.T) {
	// Set only the Copilot timeout; LiteLLM and total should remain at defaults.
	t.Setenv("CODERO_LITELLM_TIMEOUT_SEC", "")
	t.Setenv("CODERO_GATE_TOTAL_TIMEOUT_SEC", "")
	t.Setenv("CODERO_GATE_POLL_INTERVAL_SEC", "")
	t.Setenv("CODERO_COPILOT_TIMEOUT_SEC", "120")

	cfg := gate.LoadConfig()

	if cfg.CopilotTimeoutSec != 120 {
		t.Errorf("CopilotTimeoutSec: got %d, want 120", cfg.CopilotTimeoutSec)
	}
	// LiteLLM must be its own default, not influenced by Copilot.
	if cfg.LiteLLMTimeoutSec != gate.DefaultLiteLLMTimeoutSec {
		t.Errorf("LiteLLMTimeoutSec affected by CopilotTimeoutSec: got %d, want %d",
			cfg.LiteLLMTimeoutSec, gate.DefaultLiteLLMTimeoutSec)
	}
	// Total must be its own default, not influenced by Copilot.
	if cfg.GateTotalTimeoutSec != gate.DefaultGateTotalTimeoutSec {
		t.Errorf("GateTotalTimeoutSec affected by CopilotTimeoutSec: got %d, want %d",
			cfg.GateTotalTimeoutSec, gate.DefaultGateTotalTimeoutSec)
	}
}

func TestParseFallbackMode(t *testing.T) {
	cases := []struct {
		raw  string
		want gate.FallbackMode
	}{
		{"block", gate.FallbackBlock},
		{"warn", gate.FallbackWarn},
		{"skip", gate.FallbackSkip},
		{"", gate.DefaultFallbackMode},
		{"unknown", gate.DefaultFallbackMode},
		{"BLOCK", gate.DefaultFallbackMode}, // case-sensitive
	}
	for _, tc := range cases {
		t.Setenv("CODERO_AI_GATE_FALLBACK", tc.raw)
		cfg := gate.LoadConfig()
		if cfg.FallbackMode != tc.want {
			t.Errorf("CODERO_AI_GATE_FALLBACK=%q: got %q, want %q", tc.raw, cfg.FallbackMode, tc.want)
		}
	}
}
