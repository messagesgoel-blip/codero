package gatecheck_test

import (
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/gatecheck"
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

	got := gatecheck.RenderCheckReportSnapshot(report)
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

	got := gatecheck.RenderCheckReportSnapshot(report)
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

	got := gatecheck.RenderCheckReportSnapshot(report)
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

	got := gatecheck.RenderCheckReportSnapshot(report)
	if strings.Contains(got, "\033[") {
		t.Fatalf("snapshot contains ANSI escape sequences:\n%s", got)
	}
}

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

	got := gatecheck.RenderCheckReportSnapshot(report)
	if !strings.Contains(got, "REQUIRED") {
		t.Fatalf("snapshot missing REQUIRED line when required-failed>0:\n%s", got)
	}
	if !strings.Contains(got, "failed=1") {
		t.Fatalf("snapshot missing failed=1 in REQUIRED line:\n%s", got)
	}
}

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

	got := gatecheck.RenderCheckReportSnapshot(report)
	if strings.Contains(got, "REQUIRED") {
		t.Fatalf("snapshot should not contain REQUIRED line when counts are 0:\n%s", got)
	}
}

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

	got := gatecheck.RenderCheckReportSnapshot(report)
	if !strings.Contains(got, "DUR") {
		t.Fatalf("snapshot missing DUR column:\n%s", got)
	}
	if !strings.Contains(got, "350ms") {
		t.Fatalf("snapshot missing duration value:\n%s", got)
	}
}

func TestFromCheckReport_ReasonVisibleForSkipDisabled(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{SchemaVersion: gatecheck.SchemaVersion},
		Checks: []gatecheck.CheckResult{
			{ID: "skip-check", Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonNotInScope},
			{ID: "disabled-check", Status: gatecheck.StatusDisabled, ReasonCode: gatecheck.ReasonMissingTool},
		},
		RunAt: time.Now().UTC(),
	}

	vm := gatecheck.FromCheckReport(report)
	if len(vm.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(vm.Checks))
	}
	if vm.Checks[0].Reason == "" {
		t.Errorf("skip check reason should be visible")
	}
	if vm.Checks[0].ReasonCode != string(gatecheck.ReasonNotInScope) {
		t.Errorf("skip check reason code = %q, want %q", vm.Checks[0].ReasonCode, gatecheck.ReasonNotInScope)
	}
	if vm.Checks[1].Reason == "" {
		t.Errorf("disabled check reason should be visible")
	}
	if vm.Checks[1].ReasonCode != string(gatecheck.ReasonMissingTool) {
		t.Errorf("disabled check reason code = %q, want %q", vm.Checks[1].ReasonCode, gatecheck.ReasonMissingTool)
	}
}

func TestFromCheckReport_FallsBackToCanonicalNotApplicableReason(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{SchemaVersion: gatecheck.SchemaVersion},
		Checks: []gatecheck.CheckResult{
			{ID: "skip-check", Status: gatecheck.StatusSkip},
			{ID: "disabled-check", Status: gatecheck.StatusDisabled},
		},
		RunAt: time.Now().UTC(),
	}

	vm := gatecheck.FromCheckReport(report)
	if len(vm.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(vm.Checks))
	}

	want := string(gatecheck.ReasonNotApplicable)
	for i, check := range vm.Checks {
		if check.Reason != want {
			t.Fatalf("check[%d] reason = %q, want %q", i, check.Reason, want)
		}
		if check.ReasonCode != want {
			t.Fatalf("check[%d] reason_code = %q, want %q", i, check.ReasonCode, want)
		}
	}
}

func TestFromCheckReport_PreservesHumanReasonWhenReasonCodeMissing(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{SchemaVersion: gatecheck.SchemaVersion},
		Checks: []gatecheck.CheckResult{
			{ID: "skip-check", Status: gatecheck.StatusSkip, Reason: "no staged files"},
			{ID: "disabled-check", Status: gatecheck.StatusDisabled, Reason: "tool unavailable in env"},
		},
		RunAt: time.Now().UTC(),
	}

	vm := gatecheck.FromCheckReport(report)
	if len(vm.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(vm.Checks))
	}
	if vm.Checks[0].ReasonCode != "" {
		t.Fatalf("skip check reason_code = %q, want empty", vm.Checks[0].ReasonCode)
	}
	if vm.Checks[0].Reason != "no staged files" {
		t.Fatalf("skip check reason = %q", vm.Checks[0].Reason)
	}
	if vm.Checks[1].ReasonCode != "" {
		t.Fatalf("disabled check reason_code = %q, want empty", vm.Checks[1].ReasonCode)
	}
	if vm.Checks[1].Reason != "tool unavailable in env" {
		t.Fatalf("disabled check reason = %q", vm.Checks[1].Reason)
	}
}

func TestDisplayStateIcon(t *testing.T) {
	cases := []struct {
		ds   string
		want string
	}{
		{"passing", "✓"},
		{"failing", "✗"},
		{"disabled", "–"},
		{"unknown", "?"},
		{"", "?"},
	}
	for _, tc := range cases {
		if got := gatecheck.DisplayStateIcon(tc.ds); got != tc.want {
			t.Errorf("DisplayStateIcon(%q) = %q, want %q", tc.ds, got, tc.want)
		}
	}
}

func TestFormatDurationMS(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, ""},
		{-1, ""},
		{500, "500ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{10000, "10.0s"},
	}
	for _, tc := range cases {
		if got := gatecheck.FormatDurationMS(tc.ms); got != tc.want {
			t.Errorf("FormatDurationMS(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestFromCheckReport_RequiredCounts(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			SchemaVersion:    gatecheck.SchemaVersion,
			RequiredFailed:   2,
			RequiredDisabled: 1,
		},
		RunAt: time.Now().UTC(),
	}

	vm := gatecheck.FromCheckReport(report)
	if vm.Summary.RequiredFailed != 2 {
		t.Errorf("RequiredFailed = %d, want 2", vm.Summary.RequiredFailed)
	}
	if vm.Summary.RequiredDisabled != 1 {
		t.Errorf("RequiredDisabled = %d, want 1", vm.Summary.RequiredDisabled)
	}
}

func TestFromCheckReport_DisplayState(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{SchemaVersion: gatecheck.SchemaVersion},
		Checks: []gatecheck.CheckResult{
			{ID: "a", Status: gatecheck.StatusPass},
			{ID: "b", Status: gatecheck.StatusFail},
			{ID: "c", Status: gatecheck.StatusSkip, ReasonCode: gatecheck.ReasonNotInScope},
			{ID: "d", Status: gatecheck.StatusDisabled, ReasonCode: gatecheck.ReasonMissingTool},
		},
		RunAt: time.Now().UTC(),
	}

	vm := gatecheck.FromCheckReport(report)
	if len(vm.Checks) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(vm.Checks))
	}

	cases := []struct {
		idx  int
		want string
	}{
		{0, "passing"},
		{1, "failing"},
		{2, "disabled"},
		{3, "disabled"},
	}
	for _, tc := range cases {
		if got := vm.Checks[tc.idx].DisplayState; got != tc.want {
			t.Errorf("checks[%d].DisplayState = %q, want %q", tc.idx, got, tc.want)
		}
	}
}
