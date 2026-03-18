package tui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/tui"
)

// TestRenderCheckReportSnapshot_PreservesReasonParity verifies that check IDs and
// reason text are present in the snapshot output for skip/disabled checks.
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

// TestRenderCheckReportSnapshot_SummarySection verifies the new structured summary
// block is present in the snapshot output with OVERALL, PROFILE, and COUNTS fields.
func TestRenderCheckReportSnapshot_SummarySection(t *testing.T) {
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
			{ID: "skip-check", Group: gatecheck.GroupFormat, Required: true,
				Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonNotInScope, Reason: "no staged files"},
			{ID: "disabled-check", Group: gatecheck.GroupSecurity,
				Status: gatecheck.StatusDisabled, ReasonCode: gatecheck.ReasonMissingTool, Reason: "gitleaks not found"},
		},
		RunAt: time.Unix(0, 0).UTC(),
	}

	got := tui.RenderCheckReportSnapshot(report)
	for _, want := range []string{
		"GATE CHECKS",
		"OVERALL",
		"PASS",
		"PROFILE",
		"portable",
		"COUNTS",
		"pass=0",
		"fail=0",
		"skip=1",
		"disabled=1",
		"total=2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot summary missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestRenderCheckReportSnapshot_DisplayStateColumn verifies that the snapshot
// table includes the DISPLAY column (LOG-001) with the normalized display state.
func TestRenderCheckReportSnapshot_DisplayStateColumn(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			OverallStatus: gatecheck.StatusFail,
			Passed:        1,
			Failed:        1,
			Disabled:      1,
			Total:         3,
			Profile:       gatecheck.ProfilePortable,
			SchemaVersion: gatecheck.SchemaVersion,
		},
		Checks: []gatecheck.CheckResult{
			{ID: "c1", Group: gatecheck.GroupFormat, Status: gatecheck.StatusPass},
			{ID: "c2", Group: gatecheck.GroupSecurity, Status: gatecheck.StatusFail, Required: true, ReasonCode: gatecheck.ReasonCheckFailed},
			{ID: "c3", Group: gatecheck.GroupLint, Status: gatecheck.StatusDisabled, ReasonCode: gatecheck.ReasonMissingTool},
		},
		RunAt: time.Unix(0, 0).UTC(),
	}

	got := tui.RenderCheckReportSnapshot(report)
	for _, want := range []string{
		"DISPLAY",
		"passing",
		"failing",
		"disabled",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestRenderCheckReportSnapshot_NoANSI verifies that --tui-snapshot output
// contains no ANSI escape sequences (required for deterministic parsing).
func TestRenderCheckReportSnapshot_NoANSI(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			OverallStatus: gatecheck.StatusFail,
			Passed:        1,
			Failed:        1,
			Total:         2,
			Profile:       gatecheck.ProfilePortable,
			SchemaVersion: gatecheck.SchemaVersion,
		},
		Checks: []gatecheck.CheckResult{
			{ID: "c1", Group: gatecheck.GroupFormat, Status: gatecheck.StatusPass},
			{ID: "c2", Group: gatecheck.GroupSecurity, Status: gatecheck.StatusFail, Required: true},
		},
		RunAt: time.Unix(0, 0).UTC(),
	}

	got := tui.RenderCheckReportSnapshot(report)
	if strings.Contains(got, "\033[") {
		t.Fatalf("snapshot contains ANSI escape sequences:\n%s", got)
	}
}

// TestRenderCheckReportSnapshot_RequiredCounts verifies the REQUIRED line
// appears in the summary only when required-failed or required-disabled is non-zero.
func TestRenderCheckReportSnapshot_RequiredCounts(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			OverallStatus:  gatecheck.StatusFail,
			Failed:         1,
			RequiredFailed: 1,
			Total:          1,
			Profile:        gatecheck.ProfileStrict,
			SchemaVersion:  gatecheck.SchemaVersion,
		},
		Checks: []gatecheck.CheckResult{
			{ID: "req-fail", Group: gatecheck.GroupSecurity, Status: gatecheck.StatusFail, Required: true},
		},
		RunAt: time.Unix(0, 0).UTC(),
	}

	got := tui.RenderCheckReportSnapshot(report)
	if !strings.Contains(got, "REQUIRED") {
		t.Fatalf("snapshot missing REQUIRED line when required-failed>0:\n%s", got)
	}
	if !strings.Contains(got, "failed=1") {
		t.Fatalf("snapshot missing failed=1 in REQUIRED line:\n%s", got)
	}
}

// TestRenderCheckReportSnapshot_RequiredCounts_Absent verifies the REQUIRED line
// is absent when there are no required failures or required disabled.
func TestRenderCheckReportSnapshot_RequiredCounts_Absent(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			OverallStatus: gatecheck.StatusPass,
			Passed:        1,
			Total:         1,
			Profile:       gatecheck.ProfilePortable,
			SchemaVersion: gatecheck.SchemaVersion,
		},
		Checks: []gatecheck.CheckResult{
			{ID: "c1", Group: gatecheck.GroupFormat, Status: gatecheck.StatusPass},
		},
		RunAt: time.Unix(0, 0).UTC(),
	}

	got := tui.RenderCheckReportSnapshot(report)
	if strings.Contains(got, "REQUIRED") {
		t.Fatalf("snapshot should not contain REQUIRED line when counts are 0:\n%s", got)
	}
}

// TestRenderCheckReportSnapshot_DurationColumn verifies the DUR column is
// present in the snapshot table header.
func TestRenderCheckReportSnapshot_DurationColumn(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			OverallStatus: gatecheck.StatusPass,
			Passed:        1,
			Total:         1,
			Profile:       gatecheck.ProfilePortable,
			SchemaVersion: gatecheck.SchemaVersion,
		},
		Checks: []gatecheck.CheckResult{
			{ID: "timed-check", Group: gatecheck.GroupFormat, Status: gatecheck.StatusPass, DurationMS: 350},
		},
		RunAt: time.Unix(0, 0).UTC(),
	}

	got := tui.RenderCheckReportSnapshot(report)
	if !strings.Contains(got, "DUR") {
		t.Fatalf("snapshot missing DUR column:\n%s", got)
	}
	if !strings.Contains(got, "350ms") {
		t.Fatalf("snapshot missing duration value:\n%s", got)
	}
}
