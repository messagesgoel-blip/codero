// Package normalizer converts provider-specific review findings into the
// canonical internal finding schema. Normalization is deterministic: the same
// raw input always produces the same Finding output.
package normalizer

import (
	"fmt"
	"strings"
	"time"
)

// Severity is the canonical finding severity level.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// validSeverities maps recognized severity strings (lower-cased) to canonical values.
var validSeverities = map[string]Severity{
	"error":       SeverityError,
	"err":         SeverityError,
	"critical":    SeverityError,
	"fatal":       SeverityError,
	"warning":     SeverityWarning,
	"warn":        SeverityWarning,
	"medium":      SeverityWarning,
	"info":        SeverityInfo,
	"information": SeverityInfo,
	"note":        SeverityInfo,
	"suggestion":  SeverityInfo,
	"low":         SeverityInfo,
	"hint":        SeverityInfo,
}

// Finding is the canonical internal schema for a review finding.
// All fields are stable and deterministic for the same raw input.
type Finding struct {
	Severity  Severity  `json:"severity"`
	Category  string    `json:"category"`
	File      string    `json:"file"`
	Line      int       `json:"line"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
	RuleID    string    `json:"rule_id,omitempty"`
}

// RawFinding is the provider-agnostic raw input to the normalizer.
// Providers fill in what they can; the normalizer fills in defaults.
type RawFinding struct {
	Severity string
	Category string
	File     string
	Line     int
	Message  string
	Source   string
	RuleID   string
	// Timestamp overrides the normalization time if set. Leave zero for now.
	Timestamp time.Time
}

// ErrMalformedFinding is returned when a raw finding cannot be normalized due
// to missing required fields.
var ErrMalformedFinding = fmt.Errorf("malformed finding")

// Normalize converts a RawFinding to a canonical Finding.
// It is deterministic: same input → same output.
// Returns ErrMalformedFinding if Message is empty after trimming.
// Unknown severity strings default to SeverityInfo with no error.
func Normalize(raw RawFinding, source string, now time.Time) (Finding, error) {
	msg := strings.TrimSpace(raw.Message)
	if msg == "" {
		return Finding{}, fmt.Errorf("%w: message is required", ErrMalformedFinding)
	}

	sev := normalizeSeverity(raw.Severity)
	cat := normalizeCategory(raw.Category)

	// Source: prefer explicit field, fall back to the parameter.
	src := strings.TrimSpace(raw.Source)
	if src == "" {
		src = strings.TrimSpace(source)
	}
	if src == "" {
		src = "unknown"
	}

	ts := raw.Timestamp
	if ts.IsZero() {
		ts = now
	}

	return Finding{
		Severity:  sev,
		Category:  cat,
		File:      strings.TrimSpace(raw.File),
		Line:      normalizeLineNumber(raw.Line),
		Message:   msg,
		Source:    src,
		Timestamp: ts.UTC().Truncate(time.Second),
		RuleID:    strings.TrimSpace(raw.RuleID),
	}, nil
}

// NormalizeAll normalizes a slice of raw findings. Malformed entries are
// collected into the returned error slice; well-formed entries are returned.
// Processing continues past malformed entries so callers receive maximum output.
func NormalizeAll(raws []RawFinding, source string, now time.Time) ([]Finding, []error) {
	findings := make([]Finding, 0, len(raws))
	var errs []error
	for i, raw := range raws {
		f, err := Normalize(raw, source, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("finding[%d]: %w", i, err))
			continue
		}
		findings = append(findings, f)
	}
	return findings, errs
}

// normalizeSeverity maps a raw severity string to the canonical Severity.
// Unknown values default to SeverityInfo (no error; providers vary widely).
func normalizeSeverity(raw string) Severity {
	key := strings.ToLower(strings.TrimSpace(raw))
	if sev, ok := validSeverities[key]; ok {
		return sev
	}
	return SeverityInfo
}

// normalizeCategory trims and lowercases the category string.
// Returns "general" for empty categories.
func normalizeCategory(raw string) string {
	cat := strings.ToLower(strings.TrimSpace(raw))
	if cat == "" {
		return "general"
	}
	return cat
}

// normalizeLineNumber clamps negative line numbers to 0 (file-level finding).
func normalizeLineNumber(line int) int {
	if line < 0 {
		return 0
	}
	return line
}
