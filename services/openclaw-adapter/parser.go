package main

import (
	"strings"
)

const taskCompleteMarker = "TASK_COMPLETE"

// ParseResult holds the extracted summary block fields.
type ParseResult struct {
	Detected      bool
	PRTitle       string
	ChangeSummary string
	TestNotes     string
	UsedFallback  bool
}

// ParseTaskComplete scans text for TASK_COMPLETE and extracts the summary block.
// The summary block is the contiguous key: value lines immediately following the marker.
// Keys: pr_title, change_summary, test_notes (all optional).
// Returns ParseResult with UsedFallback=true if no pr_title was found.
func ParseTaskComplete(text string) ParseResult {
	lines := strings.Split(text, "\n")
	markerIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == taskCompleteMarker {
			markerIdx = i
			break
		}
	}
	if markerIdx < 0 {
		return ParseResult{}
	}

	var r ParseResult
	r.Detected = true

	for i := markerIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			break
		}
		key, val, ok := parseKV(line)
		if !ok {
			break
		}
		switch key {
		case "pr_title":
			r.PRTitle = val
		case "change_summary":
			r.ChangeSummary = val
		case "test_notes":
			r.TestNotes = val
		}
	}

	r.UsedFallback = r.PRTitle == ""
	return r
}

// parseKV splits "key: value" into (key, value, true).
// Returns ("", "", false) if the line doesn't match key: value format.
func parseKV(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx < 1 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	if strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	val := strings.TrimSpace(line[idx+1:])
	return key, val, true
}
