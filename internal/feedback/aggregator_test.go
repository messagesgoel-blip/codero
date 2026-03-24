package feedback

import (
	"strings"
	"testing"
)

func TestAggregateFeedback_PrecedenceOrder(t *testing.T) {
	input := AggregateInput{
		CI: &SourceSnapshot{
			Status: "failure",
			Body:   "CI failed: lint errors",
		},
		Coderabbit: &SourceSnapshot{
			Status:   "changes_requested",
			Blocking: true,
			Body:     "CodeRabbit blocking review",
		},
		Human: &SourceSnapshot{
			Status: "changes_requested",
			Body:   "Human review comments",
		},
		Compliance: &SourceSnapshot{
			Status: "failure",
			Body:   "Compliance gate failed",
		},
	}

	result := AggregateFeedback(input)

	if len(result.OrderedSections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(result.OrderedSections))
	}

	expected := []string{SourceCompliance, SourceCoderabbit, SourceCI, SourceHuman}
	for i, want := range expected {
		if result.OrderedSections[i].Source != want {
			t.Errorf("section[%d]: expected source %q, got %q", i, want, result.OrderedSections[i].Source)
		}
	}
}

func TestAggregateFeedback_AdvisoryCoderabbitLast(t *testing.T) {
	input := AggregateInput{
		Coderabbit: &SourceSnapshot{
			Status:   "comment",
			Blocking: false,
			Body:     "Advisory suggestions",
		},
		Human: &SourceSnapshot{
			Status: "approved",
			Body:   "Looks good",
		},
	}

	result := AggregateFeedback(input)

	if len(result.OrderedSections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(result.OrderedSections))
	}

	if result.OrderedSections[0].Source != SourceHuman {
		t.Errorf("expected human first, got %q", result.OrderedSections[0].Source)
	}
	if result.OrderedSections[1].Source != SourceCoderabbit {
		t.Errorf("expected coderabbit second, got %q", result.OrderedSections[1].Source)
	}
}

func TestAggregateFeedback_Truncation(t *testing.T) {
	bigBody := strings.Repeat("x", 20000)
	input := AggregateInput{
		CI: &SourceSnapshot{
			Status: "failure",
			Body:   bigBody,
		},
	}

	result := AggregateFeedback(input)

	if !result.Truncated {
		t.Error("expected Truncated=true")
	}
	if len(result.ContextBlock) > MaxContextBlockBytes+200 {
		// Allow some overhead for the truncation notice.
		t.Errorf("context block too large: %d bytes", len(result.ContextBlock))
	}
	if !strings.Contains(result.ContextBlock, "[truncated") {
		t.Error("expected truncation notice in context block")
	}
}

func TestAggregateFeedback_EmptyInputs(t *testing.T) {
	result := AggregateFeedback(AggregateInput{})

	if len(result.OrderedSections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(result.OrderedSections))
	}
	if result.ContextBlock != "" {
		t.Errorf("expected empty context block, got %q", result.ContextBlock)
	}
}

func TestAggregateFeedback_SourceStatus(t *testing.T) {
	input := AggregateInput{
		CI: &SourceSnapshot{
			Status: "success",
			Body:   "All checks passed",
		},
		// Coderabbit: nil -> not_configured
		Human: &SourceSnapshot{
			Status: "pending",
			Body:   "",
		},
		Compliance: &SourceSnapshot{
			Status: "success",
			Body:   "All gates passed",
		},
	}

	result := AggregateFeedback(input)

	tests := []struct {
		source string
		want   string
	}{
		{SourceCI, StatusAvailable},
		{SourceCoderabbit, StatusNotConfigured},
		{SourceHuman, StatusPending},
		{SourceCompliance, StatusAvailable},
	}

	for _, tc := range tests {
		got := result.SourceStatuses[tc.source]
		if got != tc.want {
			t.Errorf("source %q: expected status %q, got %q", tc.source, tc.want, got)
		}
	}
}

func TestComputeCacheHash_Deterministic(t *testing.T) {
	input := AggregateInput{
		CI: &SourceSnapshot{
			Status: "success",
			Body:   "All checks passed",
		},
	}

	h1 := ComputeCacheHash(input)
	h2 := ComputeCacheHash(input)

	if h1 == "" {
		t.Error("hash should not be empty")
	}
	if h1 != h2 {
		t.Errorf("hash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("expected 16 hex chars, got %d: %q", len(h1), h1)
	}
}

func TestComputeCacheHash_DifferentInputs(t *testing.T) {
	input1 := AggregateInput{
		CI: &SourceSnapshot{
			Status: "success",
			Body:   "All checks passed",
		},
	}
	input2 := AggregateInput{
		CI: &SourceSnapshot{
			Status: "failure",
			Body:   "Lint errors",
		},
	}

	h1 := ComputeCacheHash(input1)
	h2 := ComputeCacheHash(input2)

	if h1 == h2 {
		t.Errorf("different inputs should produce different hashes: %q == %q", h1, h2)
	}
}
