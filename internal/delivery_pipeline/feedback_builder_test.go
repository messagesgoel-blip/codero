package deliverypipeline

import (
	"strings"
	"testing"
)

func TestBuildFeedback_PrecedenceOrder(t *testing.T) {
	sources := []FeedbackSource{
		{Type: FeedbackSourceInformational, Findings: []FeedbackItem{{Message: "note"}}},
		{Type: FeedbackSourceAutomated, Findings: []FeedbackItem{{Message: "auto"}}},
		{Type: FeedbackSourceCI, Findings: []FeedbackItem{{Message: "ci"}}},
		{Type: FeedbackSourceGate, Findings: []FeedbackItem{{Message: "gate"}}},
		{Type: FeedbackSourceCoderabbit, Findings: []FeedbackItem{{Message: "cr"}}},
		{Type: FeedbackSourceHuman, Findings: []FeedbackItem{{Message: "human"}}},
	}

	feedback, err := BuildFeedback(sources)
	if err != nil {
		t.Fatalf("BuildFeedback: %v", err)
	}
	if feedback == nil {
		t.Fatal("expected feedback, got nil")
	}

	want := []FeedbackSourceType{
		FeedbackSourceHuman,
		FeedbackSourceCoderabbit,
		FeedbackSourceGate,
		FeedbackSourceCI,
		FeedbackSourceAutomated,
		FeedbackSourceInformational,
	}
	if len(feedback.Sections) != len(want) {
		t.Fatalf("sections: got %d, want %d", len(feedback.Sections), len(want))
	}
	for i, section := range feedback.Sections {
		if got := FeedbackSourceType(section.Source); got != want[i] {
			t.Fatalf("section %d source: got %q, want %q", i, got, want[i])
		}
	}
}

func TestBuildFeedback_TruncatesLowestPriorityFirst(t *testing.T) {
	t.Setenv("CODERO_FEEDBACK_MAX_SIZE", "200")
	large := strings.Repeat("x", 400)
	sources := []FeedbackSource{
		{Type: FeedbackSourceHuman, Findings: []FeedbackItem{{Message: "human"}}},
		{Type: FeedbackSourceCoderabbit, Findings: []FeedbackItem{{Message: "coderabbit"}}},
		{Type: FeedbackSourceAutomated, Findings: []FeedbackItem{{Message: large}}},
	}

	feedback, err := BuildFeedback(sources)
	if err != nil {
		t.Fatalf("BuildFeedback: %v", err)
	}
	if feedback == nil {
		t.Fatal("expected feedback, got nil")
	}
	if !feedback.Truncated {
		t.Fatal("expected feedback to be truncated")
	}
	if len(feedback.Sections) != 2 {
		t.Fatalf("sections: got %d, want 2", len(feedback.Sections))
	}
	for _, section := range feedback.Sections {
		if section.Source == string(FeedbackSourceAutomated) {
			t.Fatal("expected automated feedback to be trimmed first")
		}
	}
}

func TestBuildFeedback_GateOnly(t *testing.T) {
	feedback, err := BuildFeedback([]FeedbackSource{
		{Type: FeedbackSourceGate, Findings: []FeedbackItem{{Message: "gate fail"}}},
	})
	if err != nil {
		t.Fatalf("BuildFeedback: %v", err)
	}
	if feedback == nil {
		t.Fatal("expected feedback, got nil")
	}
	if len(feedback.Sections) != 1 {
		t.Fatalf("sections: got %d, want 1", len(feedback.Sections))
	}
	if feedback.Sections[0].Title != "Gate Findings" {
		t.Fatalf("title: got %q, want Gate Findings", feedback.Sections[0].Title)
	}
}

func TestBuildFeedback_EmptySources(t *testing.T) {
	feedback, err := BuildFeedback(nil)
	if err != nil {
		t.Fatalf("BuildFeedback: %v", err)
	}
	if feedback != nil {
		t.Fatalf("expected nil feedback, got %+v", feedback)
	}
}
