package tui

import (
	"strings"
	"testing"

	"github.com/codero/codero/internal/tui/adapters"
)

func TestRenderTerminalThread_ShowsNewestMessages(t *testing.T) {
	m := Model{
		theme: DefaultTheme,
		cliMessages: []terminalMessage{
			{Role: "assistant", Content: "oldest"},
			{Role: "assistant", Content: "middle"},
			{Role: "assistant", Content: "newest"},
		},
	}

	lines := m.renderTerminalThread(80, 2)
	rendered := strings.Join(lines, "\n")

	if strings.Contains(rendered, "oldest") {
		t.Fatalf("expected oldest message to be truncated, got:\n%s", rendered)
	}
	for _, want := range []string{"middle", "newest"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered thread to contain %q, got:\n%s", want, rendered)
		}
	}
}

func TestRenderTerminalThread_PreservesHeaderWhenClipped(t *testing.T) {
	m := Model{
		theme: DefaultTheme,
		cliMessages: []terminalMessage{
			{Role: "assistant", Meta: "local", Content: "first\nsecond\nthird"},
		},
	}

	lines := m.renderTerminalThread(80, 2)
	rendered := strings.Join(lines, "\n")

	if !strings.Contains(rendered, "ASSISTANT") {
		t.Fatalf("expected clipped message to keep header, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "third") {
		t.Fatalf("expected clipped message to keep newest body line, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "second") {
		t.Fatalf("expected middle body line to be trimmed, got:\n%s", rendered)
	}
}

func TestLocalTerminalCommand_StatusAndGateParity(t *testing.T) {
	m := Model{
		gateVM: adapters.GateViewModel{
			StatusLabel:   "FAIL",
			CurrentGate:   "",
			ElapsedSec:    61,
			PipelineRows:  []adapters.PipelineRow{{Name: "gitleaks", Status: "pass", Note: "clean"}},
			CopilotStatus: "running",
			LiteLLMStatus: "pending",
		},
		checksPane: ChecksPane{
			vm: adapters.CheckReportViewModel{
				Summary: adapters.CheckSummaryViewModel{
					Overall:          "fail",
					Passed:           8,
					Failed:           3,
					Skipped:          1,
					Disabled:         1,
					RequiredFailed:   2,
					RequiredDisabled: 1,
					Total:            14,
				},
			},
		},
	}

	handled, messages, _, _ := m.localTerminalCommand("status")
	if !handled || len(messages) != 1 {
		t.Fatalf("status command = (%v, %d messages), want handled single message", handled, len(messages))
	}
	wantStatus := "Gate: FAIL  |  Current gate: pending  |  Elapsed: 1m1s\nFindings: 3 failed, 8 passed, 14 total"
	if messages[0].Content != wantStatus {
		t.Fatalf("status content mismatch\nwant:\n%s\n\ngot:\n%s", wantStatus, messages[0].Content)
	}

	handled, messages, _, _ = m.localTerminalCommand("gate")
	if !handled || len(messages) != 1 {
		t.Fatalf("gate command = (%v, %d messages), want handled single message", handled, len(messages))
	}
	wantGate := "Overall: fail\nPassed: 8  Failed: 3  Skipped: 1  Disabled: 1\nRequired failed: 2\nRequired disabled: 1"
	if messages[0].Content != wantGate {
		t.Fatalf("gate content mismatch\nwant:\n%s\n\ngot:\n%s", wantGate, messages[0].Content)
	}
}
