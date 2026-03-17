package gatecheck

import "time"

const SchemaVersion = "1"

type CheckStatus string

const (
	StatusPass        CheckStatus = "PASS"
	StatusFail        CheckStatus = "FAIL"
	StatusSkip        CheckStatus = "SKIP"
	StatusInfraBypass CheckStatus = "INFRA_BYPASS"
	StatusDisabled    CheckStatus = "DISABLED"
)

func (s CheckStatus) IsTerminalPass() bool {
	return s == StatusPass || s == StatusSkip || s == StatusInfraBypass || s == StatusDisabled
}
func (s CheckStatus) IsFailure() bool { return s == StatusFail }

type ReasonCode string

const (
	ReasonUserDisabled   ReasonCode = "USER_DISABLED"
	ReasonMissingTool    ReasonCode = "MISSING_TOOL"
	ReasonNotApplicable  ReasonCode = "NOT_APPLICABLE"
	ReasonNotInScope     ReasonCode = "NOT_IN_SCOPE"
	ReasonInfraTimeout   ReasonCode = "INFRA_TIMEOUT"
	ReasonInfraAuth      ReasonCode = "INFRA_AUTH"
	ReasonInfraRateLimit ReasonCode = "INFRA_RATE_LIMIT"
	ReasonInfraNetwork   ReasonCode = "INFRA_NETWORK"
	ReasonExecError      ReasonCode = "EXEC_ERROR"
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

type Profile string

const (
	ProfileStrict   Profile = "strict"
	ProfilePortable Profile = "portable"
	ProfileOff      Profile = "off"
)

// CheckResult is the canonical per-check state. Never omit disabled/skipped checks.
type CheckResult struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Group       Group       `json:"group"`
	Required    bool        `json:"required"`
	Enabled     bool        `json:"enabled"`
	Status      CheckStatus `json:"status"`
	ReasonCode  ReasonCode  `json:"reason_code,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	ToolName    string      `json:"tool_name,omitempty"`
	ToolPath    string      `json:"tool_path,omitempty"`
	ToolVersion string      `json:"tool_version,omitempty"`
	DurationMS  int64       `json:"duration_ms"`
	Details     string      `json:"details,omitempty"`
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
	Summary Summary       `json:"summary"`
	Checks  []CheckResult `json:"checks"`
	RunAt   time.Time     `json:"run_at"`
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
		case StatusInfraBypass:
			s.InfraBypassed++
		case StatusDisabled:
			s.Disabled++
			if c.Required && !allowRequiredSkip {
				s.RequiredDisabled++
			}
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
