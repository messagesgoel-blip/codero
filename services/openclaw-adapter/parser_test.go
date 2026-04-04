package main

import "testing"

func TestParseTaskComplete_WithBlock(t *testing.T) {
	// Full block extraction: marker + 3 key:value lines
	input := "some output\nTASK_COMPLETE\npr_title: fix lint error\nchange_summary: fixed unused import\ntest_notes: ran go vet\n"
	r := ParseTaskComplete(input)
	if !r.Detected {
		t.Fatal("expected Detected=true")
	}
	if r.PRTitle != "fix lint error" {
		t.Errorf("PRTitle = %q, want %q", r.PRTitle, "fix lint error")
	}
	if r.ChangeSummary != "fixed unused import" {
		t.Errorf("ChangeSummary = %q, want %q", r.ChangeSummary, "fixed unused import")
	}
	if r.TestNotes != "ran go vet" {
		t.Errorf("TestNotes = %q, want %q", r.TestNotes, "ran go vet")
	}
	if r.UsedFallback {
		t.Error("expected UsedFallback=false")
	}
}

func TestParseTaskComplete_NoBlock(t *testing.T) {
	// Marker present but no summary block (blank line after marker)
	input := "some output\nTASK_COMPLETE\n\nsome other text\n"
	r := ParseTaskComplete(input)
	if !r.Detected {
		t.Fatal("expected Detected=true")
	}
	if r.PRTitle != "" {
		t.Errorf("PRTitle = %q, want empty", r.PRTitle)
	}
	if !r.UsedFallback {
		t.Error("expected UsedFallback=true")
	}
}

func TestParseTaskComplete_NotPresent(t *testing.T) {
	// No marker in text
	input := "normal agent output\nno marker here\n"
	r := ParseTaskComplete(input)
	if r.Detected {
		t.Error("expected Detected=false")
	}
}

func TestParseTaskComplete_PartialBlock(t *testing.T) {
	// Only pr_title present, then non-matching line
	input := "TASK_COMPLETE\npr_title: partial fix\nsome random line\n"
	r := ParseTaskComplete(input)
	if !r.Detected {
		t.Fatal("expected Detected=true")
	}
	if r.PRTitle != "partial fix" {
		t.Errorf("PRTitle = %q, want %q", r.PRTitle, "partial fix")
	}
	if r.ChangeSummary != "" {
		t.Errorf("ChangeSummary = %q, want empty", r.ChangeSummary)
	}
	if r.TestNotes != "" {
		t.Errorf("TestNotes = %q, want empty", r.TestNotes)
	}
	if r.UsedFallback {
		t.Error("expected UsedFallback=false when pr_title is present")
	}
}

func TestParseTaskComplete_NoiseBeforeMarker(t *testing.T) {
	// Marker appears mid-text with noise before it
	input := "line 1\nline 2\nline 3\nTASK_COMPLETE\npr_title: after noise\nchange_summary: cleaned up\n"
	r := ParseTaskComplete(input)
	if !r.Detected {
		t.Fatal("expected Detected=true")
	}
	if r.PRTitle != "after noise" {
		t.Errorf("PRTitle = %q, want %q", r.PRTitle, "after noise")
	}
	if r.ChangeSummary != "cleaned up" {
		t.Errorf("ChangeSummary = %q, want %q", r.ChangeSummary, "cleaned up")
	}
}
