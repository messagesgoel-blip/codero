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

	vm := adapters.FromCheckReport(report)
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

	vm := adapters.FromCheckReport(report)
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

// --- UI-001: DisplayStateIcon and FormatDurationMS helpers ---

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
		if got := adapters.DisplayStateIcon(tc.ds); got != tc.want {
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
		if got := adapters.FormatDurationMS(tc.ms); got != tc.want {
			t.Errorf("FormatDurationMS(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

// --- UI-001: RequiredFailed/RequiredDisabled propagation ---

func TestFromCheckReport_RequiredCounts(t *testing.T) {
	report := gatecheck.Report{
		Summary: gatecheck.Summary{
			SchemaVersion:    gatecheck.SchemaVersion,
			RequiredFailed:   2,
			RequiredDisabled: 1,
		},
		RunAt: time.Now().UTC(),
	}

	vm := adapters.FromCheckReport(report)
	if vm.Summary.RequiredFailed != 2 {
		t.Errorf("RequiredFailed = %d, want 2", vm.Summary.RequiredFailed)
	}
	if vm.Summary.RequiredDisabled != 1 {
		t.Errorf("RequiredDisabled = %d, want 1", vm.Summary.RequiredDisabled)
	}
}

// --- LOG-001: DisplayState in TUI adapter ---

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

	vm := adapters.FromCheckReport(report)
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
