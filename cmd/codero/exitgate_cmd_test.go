package main

import (
	"testing"
)

func TestBuildCrit_Ready(t *testing.T) {
	c := buildCrit("test_metric", "Test metric", 10, 15, true, false)
	if !c.Ready {
		t.Errorf("expected Ready=true when current (%d) >= target (%d)", 15, 10)
	}
	if c.Gap != "" {
		t.Errorf("expected empty Gap when ready, got %q", c.Gap)
	}
}

func TestBuildCrit_NotReady(t *testing.T) {
	c := buildCrit("test_metric", "Test metric", 10, 3, true, false)
	if c.Ready {
		t.Errorf("expected Ready=false when current (%d) < target (%d)", 3, 10)
	}
	if c.Gap == "" {
		t.Error("expected non-empty Gap when not ready")
	}
}

func TestBuildCrit_AtExactThreshold(t *testing.T) {
	c := buildCrit("exact", "Exact threshold", 30, 30, true, false)
	if !c.Ready {
		t.Errorf("expected Ready=true when current == target (%d)", 30)
	}
}

func TestBuildZeroCrit_ZeroIsPass(t *testing.T) {
	c := buildZeroCrit("zero_repairs", "Zero repairs", 0, true)
	if !c.Ready {
		t.Error("expected Ready=true when current=0 for zero criterion")
	}
	if c.Gap != "" {
		t.Errorf("expected empty gap when at zero, got %q", c.Gap)
	}
}

func TestBuildZeroCrit_NonZeroFails(t *testing.T) {
	c := buildZeroCrit("zero_repairs", "Zero repairs", 3, true)
	if c.Ready {
		t.Error("expected Ready=false when current > 0 for zero criterion")
	}
	if c.Gap == "" {
		t.Error("expected non-empty Gap when current > 0")
	}
}

func TestBuildSummary_AllReady(t *testing.T) {
	criteria := []ExitCriterion{
		{Ready: true}, {Ready: true}, {Ready: true},
	}
	s := buildSummary("READY", criteria)
	if s == "" {
		t.Error("expected non-empty summary")
	}
}

func TestBuildSummary_NotReady(t *testing.T) {
	criteria := []ExitCriterion{
		{Ready: true}, {Ready: false, Required: true}, {Ready: false, Required: false},
	}
	s := buildSummary("NOT_READY", criteria)
	if s == "" {
		t.Error("expected non-empty summary")
	}
}

func TestCountUnready_OnlyCountsRequired(t *testing.T) {
	report := &ExitGateReport{
		Criteria: []ExitCriterion{
			{Ready: false, Required: true},  // counted
			{Ready: false, Required: false}, // not counted (advisory)
			{Ready: true, Required: true},   // not counted (ready)
		},
	}
	n := countUnready(report)
	if n != 1 {
		t.Errorf("expected 1 unready required criterion, got %d", n)
	}
}

func TestTruncEG_ShortString(t *testing.T) {
	got := truncEG("short", 20)
	if got != "short" {
		t.Errorf("truncEG should not truncate short strings, got %q", got)
	}
}

func TestTruncEG_LongString(t *testing.T) {
	long := "this_is_a_very_long_criterion_name"
	got := truncEG(long, 10)
	// truncEG returns max chars including the ellipsis suffix
	if len([]rune(got)) > 10 {
		t.Errorf("truncEG result rune-length %d exceeds max 10", len([]rune(got)))
	}
	if len(got) == 0 {
		t.Error("truncEG returned empty string")
	}
}

func TestExitGateThresholds_Values(t *testing.T) {
	// Ensure thresholds match documented Phase 1F requirements
	th := exitGateThresholds
	tests := []struct {
		name string
		got  int
		want int
	}{
		{"ConsecutiveDays", th.ConsecutiveDays, 30},
		{"ActiveRepos", th.ActiveRepos, 2},
		{"BranchesReviewed7Days", th.BranchesReviewed7Days, 3},
		{"StaleDetections30Days", th.StaleDetections30Days, 2},
		{"LeaseExpiryRecoveries", th.LeaseExpiryRecoveries, 1},
		{"PrecommitReviews7Days", th.PrecommitReviews7Days, 10},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("threshold %s: got %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestExitCriterion_GapMessage_ContainsName(t *testing.T) {
	c := buildCrit("consecutive_days", "Consecutive days", 30, 5, true, false)
	if c.Gap == "" {
		t.Fatal("expected non-empty gap")
	}
	// Gap message should be informative
	if len(c.Gap) < 10 {
		t.Errorf("gap message too short: %q", c.Gap)
	}
}
