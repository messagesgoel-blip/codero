package webhook

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/codero/codero/internal/state"
	"github.com/google/uuid"
)

var (
	reFileLineColon = regexp.MustCompile(`(\S+\.\w+):(\d+):?\s+(.+)`)
	reDashFileLine  = regexp.MustCompile(`^[-*]\s+(\S+\.\w+)\s+line\s+(\d+)\s*(.*)`)
)

const maxFallbackMessageLen = 500

func reviewSeverity(stateStr string) string {
	switch stateStr {
	case "changes_requested":
		return "error"
	case "approved":
		return "info"
	default:
		return "warning"
	}
}

func normalizeReviewFindings(repo, branch, body, stateStr, source, runID string) []*state.FindingRecord {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	severity := reviewSeverity(stateStr)
	now := time.Now().UTC()
	var findings []*state.FindingRecord

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if m := reFileLineColon.FindStringSubmatch(line); m != nil {
			lineNum, _ := strconv.Atoi(m[2])
			findings = append(findings, &state.FindingRecord{
				ID: uuid.New().String(), RunID: runID,
				Repo: repo, Branch: branch, Severity: severity,
				Category: "review", File: m[1], Line: lineNum,
				Message: m[3], Source: source, Timestamp: now,
			})
			continue
		}

		if m := reDashFileLine.FindStringSubmatch(line); m != nil {
			lineNum, _ := strconv.Atoi(m[2])
			msg := strings.TrimSpace(m[3])
			if msg == "" {
				msg = line
			}
			findings = append(findings, &state.FindingRecord{
				ID: uuid.New().String(), RunID: runID,
				Repo: repo, Branch: branch, Severity: severity,
				Category: "review", File: m[1], Line: lineNum,
				Message: msg, Source: source, Timestamp: now,
			})
			continue
		}
	}

	if len(findings) == 0 {
		msg := body
		if len(msg) > maxFallbackMessageLen {
			msg = msg[:maxFallbackMessageLen]
		}
		findings = append(findings, &state.FindingRecord{
			ID: uuid.New().String(), RunID: runID,
			Repo: repo, Branch: branch, Severity: severity,
			Category: "review", Message: msg,
			Source: source, Timestamp: now,
		})
	}

	return findings
}
