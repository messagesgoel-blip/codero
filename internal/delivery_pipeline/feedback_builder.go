package deliverypipeline

import (
	"sort"
	"strings"
	"time"
)

// FeedbackSourceType identifies a feedback source for precedence sorting.
type FeedbackSourceType string

const (
	FeedbackSourceHuman         FeedbackSourceType = "human"
	FeedbackSourceCoderabbit    FeedbackSourceType = "coderabbit"
	FeedbackSourceGate          FeedbackSourceType = "gate"
	FeedbackSourceCI            FeedbackSourceType = "ci"
	FeedbackSourceAutomated     FeedbackSourceType = "automated"
	FeedbackSourceInformational FeedbackSourceType = "informational"
)

// FeedbackSource represents a single source of feedback findings.
type FeedbackSource struct {
	Type     FeedbackSourceType
	Findings []FeedbackItem
}

// FeedbackSection is an ordered, rendered section of feedback.
type FeedbackSection struct {
	Title  string         `json:"title"`
	Source string         `json:"source"`
	Items  []FeedbackItem `json:"items"`
}

// BuildFeedback assembles feedback sections in precedence order and trims
// low-priority sources if the output exceeds the configured size.
func BuildFeedback(sources []FeedbackSource) (*FeedbackPackage, error) {
	entries := make([]sourceEntry, 0, len(sources))
	for idx, src := range sources {
		items := cleanFeedbackItems(src.Findings)
		if len(items) == 0 {
			continue
		}
		entries = append(entries, sourceEntry{
			index: idx,
			source: FeedbackSource{
				Type:     normalizeSourceType(src.Type),
				Findings: items,
			},
		})
	}
	if len(entries) == 0 {
		return nil, nil
	}

	sort.SliceStable(entries, func(i, j int) bool {
		pi := sourcePrecedence(entries[i].source.Type)
		pj := sourcePrecedence(entries[j].source.Type)
		if pi == pj {
			return entries[i].index < entries[j].index
		}
		return pi < pj
	})

	sections := make([]FeedbackSection, 0, len(entries))
	for _, entry := range entries {
		sections = append(sections, FeedbackSection{
			Title:  sourceTitle(entry.source.Type),
			Source: string(entry.source.Type),
			Items:  entry.source.Findings,
		})
	}

	trimmed, truncated := trimSectionsToSize(sections, feedbackMaxSize())
	feedback := &FeedbackPackage{
		Sections:     trimmed,
		GeneratedAt:  time.Now().UTC(),
		Truncated:    truncated,
		SkipTruncate: true,
	}

	for _, section := range trimmed {
		switch FeedbackSourceType(section.Source) {
		case FeedbackSourceGate:
			feedback.GateFindings = append(feedback.GateFindings, section.Items...)
		case FeedbackSourceCoderabbit:
			feedback.CodeReview = append(feedback.CodeReview, section.Items...)
		case FeedbackSourceCI:
			feedback.CIFailures = append(feedback.CIFailures, section.Items...)
		case FeedbackSourceHuman:
			feedback.ReviewComments = append(feedback.ReviewComments, section.Items...)
		}
	}

	return feedback, nil
}

type sourceEntry struct {
	index  int
	source FeedbackSource
}

func cleanFeedbackItems(items []FeedbackItem) []FeedbackItem {
	clean := make([]FeedbackItem, 0, len(items))
	for _, item := range items {
		msg := strings.TrimSpace(item.Message)
		if msg == "" {
			continue
		}
		item.Message = msg
		clean = append(clean, item)
	}
	return clean
}

func normalizeSourceType(source FeedbackSourceType) FeedbackSourceType {
	switch source {
	case FeedbackSourceHuman, FeedbackSourceCoderabbit, FeedbackSourceGate, FeedbackSourceCI,
		FeedbackSourceAutomated, FeedbackSourceInformational:
		return source
	default:
		return FeedbackSourceInformational
	}
}

func sourceTitle(source FeedbackSourceType) string {
	switch source {
	case FeedbackSourceHuman:
		return "Review Comments"
	case FeedbackSourceCoderabbit:
		return "Code Review"
	case FeedbackSourceGate:
		return "Gate Findings"
	case FeedbackSourceCI:
		return "CI Failures"
	case FeedbackSourceAutomated:
		return "Automated Feedback"
	default:
		return "Informational"
	}
}

func sourcePrecedence(source FeedbackSourceType) int {
	switch source {
	case FeedbackSourceHuman:
		return 10
	case FeedbackSourceCoderabbit:
		return 20
	case FeedbackSourceGate:
		return 25
	case FeedbackSourceCI:
		return 30
	case FeedbackSourceAutomated:
		return 40
	default:
		return 50
	}
}

func trimSectionsToSize(sections []FeedbackSection, max int) ([]FeedbackSection, bool) {
	if max <= 0 {
		return sections, false
	}

	trimmed := append([]FeedbackSection(nil), sections...)
	truncated := false

	for len(trimmed) > 0 {
		content := renderFeedbackSections(trimmed)
		if len(content) <= max {
			return trimmed, truncated
		}
		idx := removableSectionIndex(trimmed)
		if idx < 0 {
			return trimmed, true
		}
		truncated = true
		trimmed = append(trimmed[:idx], trimmed[idx+1:]...)
	}
	return trimmed, truncated
}

func removableSectionIndex(sections []FeedbackSection) int {
	for i := len(sections) - 1; i >= 0; i-- {
		switch FeedbackSourceType(sections[i].Source) {
		case FeedbackSourceHuman, FeedbackSourceCoderabbit:
			continue
		default:
			return i
		}
	}
	return -1
}
