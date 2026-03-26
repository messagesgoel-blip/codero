package gatecheck

import (
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion     = "1"
	DefaultReportPath = ".codero/gate-check/last-report.json"

	// MaxFindingsPerCheck is the per-check cap on structured findings.
	// findings_count always reflects the true total; Truncated is set when capped.
	MaxFindingsPerCheck = 50
)

type CheckStatus string

const (
	StatusPass     CheckStatus = "pass"
	StatusFail     CheckStatus = "fail"
	StatusSkip     CheckStatus = "skip"
	StatusDisabled CheckStatus = "disabled"
)

func (s CheckStatus) IsTerminalPass() bool {
	return s == StatusPass || s == StatusSkip || s == StatusDisabled
}
func (s CheckStatus) IsFailure() bool { return s == StatusFail }

// DisplayState is the normalized UI display state for a gate-check step.
// It maps the raw engine statuses to three deterministic presentation states
// so the dashboard and TUI can render every step without inferring state from
// freeform text. See docs/contracts/LOG-001.md for the canonical mapping.
type DisplayState string

const (
	// DisplayPassing means the check ran and produced a passing result.
	DisplayPassing DisplayState = "passing"
	// DisplayFailing means the check ran and produced a failing result.
	DisplayFailing DisplayState = "failing"
	// DisplayDisabled means the check was not run (disabled or skipped).
	DisplayDisabled DisplayState = "disabled"
)

// ToDisplayState maps a raw CheckStatus to the deterministic UI DisplayState.
// Both "skip" and "disabled" map to DisplayDisabled because from the UI
// perspective, a step that did not run is not actionable regardless of the
// reason — the reason_code/reason fields carry the distinction.
func (s CheckStatus) ToDisplayState() DisplayState {
	switch s {
	case StatusPass:
		return DisplayPassing
	case StatusFail:
		return DisplayFailing
	default: // StatusSkip, StatusDisabled, or any unknown value
		return DisplayDisabled
	}
}

type ReasonCode string

const (
	ReasonUserDisabled   ReasonCode = "user_disabled"
	ReasonMissingTool    ReasonCode = "missing_tool"
	ReasonNotApplicable  ReasonCode = "not_applicable"
	ReasonNotInScope     ReasonCode = "not_in_scope"
	ReasonTimeout        ReasonCode = "timeout"
	ReasonInfraBypass    ReasonCode = "infra_bypass"
	ReasonInfraAuth      ReasonCode = "infra_auth"
	ReasonInfraRateLimit ReasonCode = "infra_rate_limit"
	ReasonInfraNetwork   ReasonCode = "infra_network"
	ReasonExecError      ReasonCode = "exec_error"
	ReasonCheckFailed    ReasonCode = "check_failed"
)

type Group string

const (
	GroupFormat   Group = "format"
	GroupLint     Group = "lint"
	GroupSecurity Group = "security"
	GroupConfig   Group = "config"
	GroupTests    Group = "tests"
	GroupAI       Group = "ai"
	GroupOther    Group = "other"
)

// Finding captures a single structured issue from a check runner.
type Finding struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Severity string `json:"severity,omitempty"`
	RuleID   string `json:"rule_id,omitempty"`
	Message  string `json:"message,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
}

type Profile string

const (
	ProfileStrict   Profile = "strict"
	ProfilePortable Profile = "portable"
	ProfileOff      Profile = "off"
)

// CheckResult is the canonical per-check state. Never omit disabled/skipped checks.
type CheckResult struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Group         Group       `json:"group"`
	Tier          string      `json:"tier,omitempty"`
	Required      bool        `json:"required"`
	Enabled       bool        `json:"enabled"`
	Status        CheckStatus `json:"status"`
	ExitCode      int         `json:"exit_code,omitempty"`
	ReasonCode    ReasonCode  `json:"reason_code,omitempty"`
	Reason        string      `json:"reason,omitempty"`
	ToolName      string      `json:"tool_name,omitempty"`
	ToolPath      string      `json:"tool_path,omitempty"`
	ToolVersion   string      `json:"tool_version,omitempty"`
	DurationMS    int64       `json:"duration_ms"`
	Details       string      `json:"details,omitempty"`
	FindingsCount int         `json:"findings_count"`
	Truncated     bool        `json:"truncated,omitempty"`
	Findings      []Finding   `json:"findings,omitempty"`
}

// Summary aggregates counts and overall status from a set of CheckResults.
type Summary struct {
	OverallStatus    CheckStatus `json:"overall_status"`
	Passed           int         `json:"passed"`
	Failed           int         `json:"failed"`
	Skipped          int         `json:"skipped"`
	InfraBypassed    int         `json:"infra_bypassed"`
	Disabled         int         `json:"disabled"`
	Total            int         `json:"total"`
	RequiredFailed   int         `json:"required_failed"`
	RequiredDisabled int         `json:"required_disabled"`
	Profile          Profile     `json:"profile"`
	SchemaVersion    string      `json:"schema_version"`
}

// Report is the top-level output of a gate engine run.
type Report struct {
	Summary       Summary       `json:"summary"`
	Checks        []CheckResult `json:"checks"`
	RunAt         time.Time     `json:"run_at"`
	Invocation    string        `json:"invocation,omitempty"`
	Result        CheckStatus   `json:"result,omitempty"`
	TotalFindings int           `json:"total_findings,omitempty"`
	DurationMS    int64         `json:"duration_ms,omitempty"`
	Blocked       bool          `json:"blocked,omitempty"`
}

// ComputeSummary builds a Summary from a slice of CheckResults and profile.
func ComputeSummary(checks []CheckResult, profile Profile, allowRequiredSkip bool) Summary {
	s := Summary{Profile: profile, SchemaVersion: SchemaVersion, Total: len(checks)}
	for _, c := range checks {
		switch c.Status {
		case StatusPass:
			s.Passed++
		case StatusFail:
			s.Failed++
			if c.Required {
				s.RequiredFailed++
			}
		case StatusSkip:
			s.Skipped++
		case StatusDisabled:
			s.Disabled++
			if c.Required && !allowRequiredSkip {
				s.RequiredDisabled++
			}
		default:
			continue
		}
		if isInfraReason(c.ReasonCode) {
			s.InfraBypassed++
		}
	}
	if s.Failed > 0 || s.RequiredFailed > 0 {
		s.OverallStatus = StatusFail
	} else if s.RequiredDisabled > 0 && profile == ProfileStrict {
		s.OverallStatus = StatusFail
	} else {
		s.OverallStatus = StatusPass
	}
	return s
}

// FinalizeReport derives pipeline-level metadata and structured findings.
func FinalizeReport(report Report) Report {
	report.Checks = FinalizeChecks(report.Checks)
	report.TotalFindings = totalFindings(report.Checks)
	report.DurationMS = totalDurationMS(report)
	report.Blocked = report.Summary.Failed > 0 || report.Summary.RequiredFailed > 0
	if report.Summary.OverallStatus == StatusFail {
		report.Result = StatusFail
	} else {
		report.Result = StatusPass
	}
	return report
}

// FinalizeChecks annotates checks with tiers, exit codes, and structured findings.
func FinalizeChecks(checks []CheckResult) []CheckResult {
	for i := range checks {
		if checks[i].Tier == "" {
			if checks[i].Required {
				checks[i].Tier = "required"
			} else {
				checks[i].Tier = "optional"
			}
		}
		if checks[i].ExitCode == 0 && checks[i].Status == StatusFail {
			checks[i].ExitCode = 1
		}
		if checks[i].Details != "" {
			checks[i].Findings = buildFindings(checks[i].Details)
		}
	}
	return checks
}

func buildFindings(details string) []Finding {
	lines := strings.Split(details, "\n")
	findings := make([]Finding, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		findings = append(findings, Finding{Message: line})
		if len(findings) >= MaxFindingsPerCheck {
			break
		}
	}
	return findings
}

func totalFindings(checks []CheckResult) int {
	total := 0
	for _, c := range checks {
		total += c.FindingsCount
	}
	return total
}

func isInfraReason(code ReasonCode) bool {
	switch code {
	case ReasonInfraBypass, ReasonInfraAuth, ReasonInfraRateLimit, ReasonInfraNetwork, ReasonTimeout:
		return true
	default:
		return false
	}
}

// DisplayReason renders a stable non-pass reason string that preserves both the
// canonical reason code and the human-readable reason when both are available.
func DisplayReason(code ReasonCode, reason string) string {
	switch {
	case code == "" && reason == "":
		return ""
	case code == "":
		return reason
	case reason == "" || reason == string(code):
		return string(code)
	default:
		return fmt.Sprintf("%s - %s", code, reason)
	}
}
