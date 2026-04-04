package dashboard

import (
	"testing"
	"time"
)

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"error", 0},
		{"warning", 1},
		{"info", 2},
		{"unknown", 3},
		{"", 3},
	}
	for _, tt := range tests {
		got := severityRank(tt.input)
		if got != tt.want {
			t.Errorf("severityRank(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestSortFindings(t *testing.T) {
	now := time.Now().UTC()
	items := []FindingItem{
		{Severity: "info", File: "readme.md", Ts: now.Add(-1 * time.Minute)},
		{Severity: "error", File: "main.go", Ts: now.Add(-3 * time.Minute)},
		{Severity: "warning", File: "config.go", Ts: now.Add(-2 * time.Minute)},
		{Severity: "error", File: "handler.go", Ts: now.Add(-4 * time.Minute)},
		{Severity: "warning", File: "utils.go", Ts: now},
	}

	sortFindings(items)

	// Expect: errors first (handler.go older, main.go newer), then warnings (config.go, utils.go), then info.
	expected := []struct {
		severity string
		file     string
	}{
		{"error", "handler.go"},
		{"error", "main.go"},
		{"warning", "config.go"},
		{"warning", "utils.go"},
		{"info", "readme.md"},
	}

	if len(items) != len(expected) {
		t.Fatalf("expected %d items, got %d", len(expected), len(items))
	}

	for i, e := range expected {
		if items[i].Severity != e.severity || items[i].File != e.file {
			t.Errorf("items[%d] = {%s, %s}, want {%s, %s}",
				i, items[i].Severity, items[i].File, e.severity, e.file)
		}
	}
}

func TestSortFindings_Empty(t *testing.T) {
	var items []FindingItem
	sortFindings(items) // should not panic
	if len(items) != 0 {
		t.Errorf("expected empty, got %d items", len(items))
	}
}

func TestSortFindings_SingleItem(t *testing.T) {
	items := []FindingItem{
		{Severity: "warning", File: "a.go"},
	}
	sortFindings(items)
	if items[0].Severity != "warning" || items[0].File != "a.go" {
		t.Errorf("single item mutated: %+v", items[0])
	}
}
