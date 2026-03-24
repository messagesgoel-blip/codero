// Package feedback implements feedback aggregation, precedence ordering, truncation, and worktree file delivery per Task Layer v2 §12.
package feedback

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

const MaxContextBlockBytes = 16000 // ~4000 tokens at ~4 bytes/token

// Source identifiers
const (
	SourceCompliance = "compliance"
	SourceCoderabbit = "coderabbit"
	SourceCI         = "ci"
	SourceHuman      = "human"
)

// Source status values (per spec §12.2)
const (
	StatusAvailable     = "available"
	StatusNotConfigured = "not_configured"
	StatusPending       = "pending"
	StatusError         = "error"
)

// SourceSnapshot represents the state of a single feedback source.
type SourceSnapshot struct {
	Status   string // "success", "failure", "changes_requested", "approved", "pending", "comment", "error"
	Blocking bool   // for CodeRabbit: true = blocking, false = advisory
	Body     string // the feedback text
}

// AggregateInput collects snapshots from all feedback sources.
type AggregateInput struct {
	CI         *SourceSnapshot
	Coderabbit *SourceSnapshot
	Human      *SourceSnapshot
	Compliance *SourceSnapshot
}

// FeedbackSection is one ordered section in the aggregated output.
type FeedbackSection struct {
	Source  string
	Status  string
	Body    string
	Blocked bool
}

// AggregateResult is the output of AggregateFeedback.
type AggregateResult struct {
	OrderedSections []FeedbackSection
	ContextBlock    string
	Truncated       bool
	SourceStatuses  map[string]string
	CacheHash       string
}

// sourceStatus returns the aggregate status string for a single source.
func sourceStatus(snap *SourceSnapshot) string {
	if snap == nil {
		return StatusNotConfigured
	}
	if snap.Status == "pending" || (snap.Status == "" && snap.Body == "") {
		return StatusPending
	}
	if snap.Status == "error" {
		return StatusError
	}
	return StatusAvailable
}

// precedenceKey returns the sort key for a source+snapshot pair.
// Lower values = higher precedence.
// compliance(1) > blocking_coderabbit(2) > ci_failure(3) > human(4) > advisory_coderabbit(5)
func precedenceKey(source string, snap *SourceSnapshot) int {
	switch source {
	case SourceCompliance:
		return 1
	case SourceCoderabbit:
		if snap != nil && snap.Blocking {
			return 2
		}
		return 5
	case SourceCI:
		if snap != nil && (snap.Status == "failure" || snap.Status == "changes_requested") {
			return 3
		}
		// Non-failure CI still sorts after human
		return 3
	case SourceHuman:
		return 4
	default:
		return 6
	}
}

// isBlocked returns true if the section should be marked as blocking.
func isBlocked(source string, snap *SourceSnapshot) bool {
	if snap == nil {
		return false
	}
	if snap.Status == "failure" || snap.Status == "changes_requested" {
		return true
	}
	if source == SourceCoderabbit && snap.Blocking {
		return true
	}
	return false
}

// AggregateFeedback merges all feedback sources into a single ordered result.
func AggregateFeedback(input AggregateInput) AggregateResult {
	// Build source statuses map.
	statuses := map[string]string{
		SourceCI:         sourceStatus(input.CI),
		SourceCoderabbit: sourceStatus(input.Coderabbit),
		SourceHuman:      sourceStatus(input.Human),
		SourceCompliance: sourceStatus(input.Compliance),
	}

	// Collect sections from non-nil sources that have content.
	type entry struct {
		source string
		snap   *SourceSnapshot
	}
	candidates := []entry{
		{SourceCI, input.CI},
		{SourceCoderabbit, input.Coderabbit},
		{SourceHuman, input.Human},
		{SourceCompliance, input.Compliance},
	}

	var sections []FeedbackSection
	for _, c := range candidates {
		if c.snap == nil {
			continue
		}
		if c.snap.Body == "" && c.snap.Status == "" {
			continue
		}
		sections = append(sections, FeedbackSection{
			Source:  c.source,
			Status:  c.snap.Status,
			Body:    c.snap.Body,
			Blocked: isBlocked(c.source, c.snap),
		})
	}

	// Insertion sort by precedence (n <= 4).
	for i := 1; i < len(sections); i++ {
		key := sections[i]
		keyPrec := precedenceKey(key.Source, snapForSource(key.Source, input))
		j := i - 1
		for j >= 0 && precedenceKey(sections[j].Source, snapForSource(sections[j].Source, input)) > keyPrec {
			sections[j+1] = sections[j]
			j--
		}
		sections[j+1] = key
	}

	// Assemble context block.
	var buf strings.Builder
	for _, s := range sections {
		fmt.Fprintf(&buf, "## [%s] %s\n", strings.ToUpper(s.Source), s.Status)
		buf.WriteString(s.Body)
		buf.WriteString("\n\n")
	}
	contextBlock := buf.String()

	// Truncate if needed.
	truncated := false
	if len(contextBlock) > MaxContextBlockBytes {
		truncated = true
		contextBlock = contextBlock[:MaxContextBlockBytes]
		// Find last newline for a clean cut.
		if idx := strings.LastIndex(contextBlock, "\n"); idx > 0 {
			contextBlock = contextBlock[:idx]
		}
		contextBlock += "\n\n[truncated — feedback exceeded context window limit]\n"
	}

	return AggregateResult{
		OrderedSections: sections,
		ContextBlock:    contextBlock,
		Truncated:       truncated,
		SourceStatuses:  statuses,
		CacheHash:       ComputeCacheHash(input),
	}
}

// snapForSource returns the snapshot pointer for a given source name.
func snapForSource(source string, input AggregateInput) *SourceSnapshot {
	switch source {
	case SourceCI:
		return input.CI
	case SourceCoderabbit:
		return input.Coderabbit
	case SourceHuman:
		return input.Human
	case SourceCompliance:
		return input.Compliance
	default:
		return nil
	}
}

// ComputeCacheHash returns the first 16 hex chars of the SHA-256 of the
// JSON-marshaled input.
func ComputeCacheHash(input AggregateInput) string {
	data, _ := json.Marshal(input)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:8])
}
