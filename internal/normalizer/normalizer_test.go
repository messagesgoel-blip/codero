package normalizer_test

import (
	"errors"
	"testing"
	"time"

	"github.com/codero/codero/internal/normalizer"
)

var fixedTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func TestNormalize_ValidFinding(t *testing.T) {
	raw := normalizer.RawFinding{
		Severity: "warning",
		Category: "Security",
		File:     "main.go",
		Line:     42,
		Message:  "use of unsafe pointer",
		RuleID:   "G103",
	}
	f, err := normalizer.Normalize(raw, "gosec", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Severity != normalizer.SeverityWarning {
		t.Errorf("severity: got %q, want %q", f.Severity, normalizer.SeverityWarning)
	}
	if f.Category != "security" {
		t.Errorf("category: got %q, want %q", f.Category, "security")
	}
	if f.File != "main.go" {
		t.Errorf("file: got %q, want %q", f.File, "main.go")
	}
	if f.Line != 42 {
		t.Errorf("line: got %d, want 42", f.Line)
	}
	if f.Message != "use of unsafe pointer" {
		t.Errorf("message: got %q", f.Message)
	}
	if f.Source != "gosec" {
		t.Errorf("source: got %q, want %q", f.Source, "gosec")
	}
	if f.RuleID != "G103" {
		t.Errorf("rule_id: got %q, want %q", f.RuleID, "G103")
	}
	if !f.Timestamp.Equal(fixedTime) {
		t.Errorf("timestamp: got %v, want %v", f.Timestamp, fixedTime)
	}
}

func TestNormalize_EmptyMessage(t *testing.T) {
	raw := normalizer.RawFinding{
		Severity: "error",
		Message:  "",
	}
	_, err := normalizer.Normalize(raw, "test", fixedTime)
	if err == nil {
		t.Fatal("expected error for empty message, got nil")
	}
	if !errors.Is(err, normalizer.ErrMalformedFinding) {
		t.Errorf("expected ErrMalformedFinding, got %v", err)
	}
}

func TestNormalize_WhitespaceOnlyMessage(t *testing.T) {
	raw := normalizer.RawFinding{Message: "   \t  "}
	_, err := normalizer.Normalize(raw, "test", fixedTime)
	if !errors.Is(err, normalizer.ErrMalformedFinding) {
		t.Errorf("expected ErrMalformedFinding for whitespace-only message, got %v", err)
	}
}

func TestNormalize_UnknownSeverityDefaultsToInfo(t *testing.T) {
	raw := normalizer.RawFinding{
		Severity: "UNKNOWN_SEVERITY",
		Message:  "some finding",
	}
	f, err := normalizer.Normalize(raw, "test", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Severity != normalizer.SeverityInfo {
		t.Errorf("expected SeverityInfo for unknown severity, got %q", f.Severity)
	}
}

func TestNormalize_EmptySeverityDefaultsToInfo(t *testing.T) {
	raw := normalizer.RawFinding{Message: "finding"}
	f, err := normalizer.Normalize(raw, "test", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Severity != normalizer.SeverityInfo {
		t.Errorf("expected SeverityInfo for empty severity, got %q", f.Severity)
	}
}

func TestNormalize_SeverityAliases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  normalizer.Severity
	}{
		{"error_lower", "error", normalizer.SeverityError},
		{"error_upper", "ERROR", normalizer.SeverityError},
		{"err", "err", normalizer.SeverityError},
		{"critical", "critical", normalizer.SeverityError},
		{"fatal", "fatal", normalizer.SeverityError},
		{"warning", "warning", normalizer.SeverityWarning},
		{"warn_upper", "WARN", normalizer.SeverityWarning},
		{"medium", "medium", normalizer.SeverityWarning},
		{"info", "info", normalizer.SeverityInfo},
		{"note", "note", normalizer.SeverityInfo},
		{"suggestion", "suggestion", normalizer.SeverityInfo},
		{"low", "low", normalizer.SeverityInfo},
		{"hint", "hint", normalizer.SeverityInfo},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw := normalizer.RawFinding{Severity: tc.input, Message: "x"}
			f, err := normalizer.Normalize(raw, "src", fixedTime)
			if err != nil {
				t.Fatalf("severity %q: unexpected error: %v", tc.input, err)
			}
			if f.Severity != tc.want {
				t.Errorf("severity %q: got %q, want %q", tc.input, f.Severity, tc.want)
			}
		})
	}
}

func TestNormalize_EmptyCategoryDefaultsToGeneral(t *testing.T) {
	raw := normalizer.RawFinding{Message: "finding"}
	f, err := normalizer.Normalize(raw, "test", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Category != "general" {
		t.Errorf("category: got %q, want %q", f.Category, "general")
	}
}

func TestNormalize_NegativeLineClampedToZero(t *testing.T) {
	raw := normalizer.RawFinding{Message: "finding", Line: -5}
	f, err := normalizer.Normalize(raw, "test", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Line != 0 {
		t.Errorf("line: got %d, want 0", f.Line)
	}
}

func TestNormalize_SourceFallback(t *testing.T) {
	raw := normalizer.RawFinding{Message: "finding", Source: ""}
	f, err := normalizer.Normalize(raw, "provider-name", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Source != "provider-name" {
		t.Errorf("source: got %q, want %q", f.Source, "provider-name")
	}
}

func TestNormalize_ExplicitSourceOverridesParam(t *testing.T) {
	raw := normalizer.RawFinding{Message: "finding", Source: "specific-tool"}
	f, err := normalizer.Normalize(raw, "provider-name", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Source != "specific-tool" {
		t.Errorf("source: got %q, want %q", f.Source, "specific-tool")
	}
}

func TestNormalize_EmptySourceAndParamDefaultsToUnknown(t *testing.T) {
	raw := normalizer.RawFinding{Message: "finding"}
	f, err := normalizer.Normalize(raw, "", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Source != "unknown" {
		t.Errorf("source: got %q, want %q", f.Source, "unknown")
	}
}

func TestNormalize_TimestampUTCTruncated(t *testing.T) {
	rawTime := time.Date(2026, 1, 15, 12, 0, 0, 999999999, time.UTC)
	raw := normalizer.RawFinding{Message: "finding", Timestamp: rawTime}
	f, err := normalizer.Normalize(raw, "test", fixedTime)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantTime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	if !f.Timestamp.Equal(wantTime) {
		t.Errorf("timestamp: got %v, want %v", f.Timestamp, wantTime)
	}
}

func TestNormalize_Deterministic(t *testing.T) {
	raw := normalizer.RawFinding{
		Severity: "warning",
		Category: "Security",
		File:     "internal/foo.go",
		Line:     10,
		Message:  "SQL injection risk",
		RuleID:   "S001",
	}
	f1, err := normalizer.Normalize(raw, "test", fixedTime)
	if err != nil {
		t.Fatalf("first normalize: %v", err)
	}
	f2, err := normalizer.Normalize(raw, "test", fixedTime)
	if err != nil {
		t.Fatalf("second normalize: %v", err)
	}
	if f1 != f2 {
		t.Errorf("normalize is not deterministic: %+v != %+v", f1, f2)
	}
}

func TestNormalizeAll_MixedValid(t *testing.T) {
	raws := []normalizer.RawFinding{
		{Message: "valid finding", Severity: "error"},
		{Message: ""}, // malformed
		{Message: "another ok", Severity: "info"},
		{Message: "   "}, // malformed whitespace
	}
	findings, errs := normalizer.NormalizeAll(raws, "test", fixedTime)
	if len(findings) != 2 {
		t.Errorf("expected 2 valid findings, got %d", len(findings))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

func TestNormalizeAll_AllMalformed(t *testing.T) {
	raws := []normalizer.RawFinding{
		{Message: ""},
		{Message: "\t"},
	}
	findings, errs := normalizer.NormalizeAll(raws, "test", fixedTime)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

func TestNormalizeAll_Empty(t *testing.T) {
	findings, errs := normalizer.NormalizeAll(nil, "test", fixedTime)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nil input, got %d", len(findings))
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors for nil input, got %d", len(errs))
	}
}
