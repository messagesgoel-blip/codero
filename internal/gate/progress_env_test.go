package gate_test

import (
	"testing"

	"github.com/codero/codero/internal/gate"
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
	r := gate.ParseProgressEnv(content)
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
	r := gate.ParseProgressEnv("")
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
	r := gate.ParseProgressEnv(content)
	if r.Status != gate.StatusFail {
		t.Errorf("expected fail, got %q", r.Status)
	}
	if len(r.Comments) != 2 {
		t.Errorf("expected 2 comments, got %d: %v", len(r.Comments), r.Comments)
	}
}

func TestReadProgressEnv_NonExistent(t *testing.T) {
	r := gate.ReadProgressEnv("/nonexistent/path/that/does/not/exist")
	if r.Status != gate.StatusPending {
		t.Errorf("expected pending for missing file, got %q", r.Status)
	}
	if r.IsFinal() {
		t.Error("expected IsFinal=false for missing file")
	}
}
