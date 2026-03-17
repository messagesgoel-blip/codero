package tui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/tui"
)

func TestRenderCheckReportSnapshot_PreservesReasonParity(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			OverallStatus: gatecheck.StatusPass,
			Passed:        0,
			Failed:        0,
			Skipped:       1,
			Disabled:      1,
			Total:         2,
			Profile:       gatecheck.ProfilePortable,
			SchemaVersion: gatecheck.SchemaVersion,
		},
		Checks: []gatecheck.CheckResult{
			{
				ID:         "skip-check",
				Group:      gatecheck.GroupFormat,
				Required:   true,
				Status:     gatecheck.StatusSkip,
				ReasonCode: gatecheck.ReasonNotInScope,
				Reason:     "no staged files",
			},
			{
				ID:         "disabled-check",
				Group:      gatecheck.GroupSecurity,
				Status:     gatecheck.StatusDisabled,
				ReasonCode: gatecheck.ReasonMissingTool,
				Reason:     "gitleaks not found",
			},
		},
		RunAt: time.Unix(0, 0).UTC(),
	}

	got := tui.RenderCheckReportSnapshot(report)
	for _, want := range []string{
		"GATE CHECKS",
		"Summary  overall=pass  pass=0  fail=0  skip=1  infra=0  disabled=1  total=2  profile=portable",
		"skip-check",
		"not_in_scope - no staged files",
		"disabled-check",
		"missing_tool - gitleaks not found",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot missing %q\nfull output:\n%s", want, got)
		}
	}
}
