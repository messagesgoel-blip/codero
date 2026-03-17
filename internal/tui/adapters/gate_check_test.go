package adapters_test

import (
	"testing"
	"time"

	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/tui/adapters"
)

func TestFromCheckReport_ReasonVisibleForSkipDisabled(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{SchemaVersion: gatecheck.SchemaVersion},
		Checks: []gatecheck.CheckResult{
			{ID: "skip-check", Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonNotInScope},
			{ID: "disabled-check", Status: gatecheck.StatusDisabled, ReasonCode: gatecheck.ReasonMissingTool},
		},
		RunAt: time.Now().UTC(),
	}

	vm := adapters.FromCheckReport(report)
	if len(vm.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(vm.Checks))
	}
	if vm.Checks[0].Reason == "" {
		t.Errorf("skip check reason should be visible")
	}
	if vm.Checks[1].Reason == "" {
		t.Errorf("disabled check reason should be visible")
	}
}
