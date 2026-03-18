// Package adapters maps Codero domain types to TUI view models.
package adapters

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/gatecheck"
)

// GateViewModel is the display-ready model for the gate progress pane.
type GateViewModel struct {
	Status        gate.Status
	RunID         string
	ElapsedSec    int
	PollAfterSec  int
	ProgressBar   string
	CurrentGate   string
	CopilotStatus string
	LiteLLMStatus string
	Comments      []string

	// Computed display fields
	StatusLabel string
	StatusIcon  string
	IsFinal     bool

	// Non-authoritative pipeline signal rows (display only, clearly labelled)
	PipelineRows []PipelineRow
}

// PipelineRow is a non-authoritative local pipeline check (gitleaks, semgrep etc.)
type PipelineRow struct {
	Name   string
	Status string
	Note   string
}

// FromGateResult converts a gate.Result into a GateViewModel.
func FromGateResult(r gate.Result) GateViewModel {
	bar := r.ProgressBar
	if bar == "" {
		bar = gate.RenderBar(r.CopilotStatus, r.LiteLLMStatus, r.CurrentGate)
	}

	var statusLabel, statusIcon string
	switch r.Status {
	case gate.StatusPass:
		statusLabel, statusIcon = "PASS", "✓"
	case gate.StatusFail:
		statusLabel, statusIcon = "FAIL", "✗"
	default:
		statusLabel, statusIcon = "PENDING", "○"
	}

	return GateViewModel{
		Status:        r.Status,
		RunID:         r.RunID,
		ElapsedSec:    r.ElapsedSec,
		PollAfterSec:  r.PollAfterSec,
		ProgressBar:   bar,
		CurrentGate:   r.CurrentGate,
		CopilotStatus: r.CopilotStatus,
		LiteLLMStatus: r.LiteLLMStatus,
		Comments:      r.Comments,
		StatusLabel:   statusLabel,
		StatusIcon:    statusIcon,
		IsFinal:       r.Status == gate.StatusPass || r.Status == gate.StatusFail,
		PipelineRows:  staticPipelineRows(),
	}
}

// FromProgressEnv reads .codero/gate-heartbeat/progress.env and returns a GateViewModel.
func FromProgressEnv(repoRoot string) GateViewModel {
	path := filepath.Join(repoRoot, ".codero", "gate-heartbeat", "progress.env")
	data, err := os.ReadFile(path)
	if err != nil {
		return FromGateResult(gate.Result{
			Status:        gate.StatusPending,
			CopilotStatus: "pending",
			LiteLLMStatus: "pending",
		})
	}
	return FromGateResult(ParseProgressEnv(string(data)))
}

// ParseProgressEnv converts KEY=VALUE pairs from progress.env content into a gate.Result.
func ParseProgressEnv(content string) gate.Result {
	fields := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	r := gate.Result{
		RunID:         fields["RUN_ID"],
		ProgressBar:   fields["PROGRESS_BAR"],
		CurrentGate:   fields["CURRENT_GATE"],
		CopilotStatus: fields["COPILOT_STATUS"],
		LiteLLMStatus: fields["LITELLM_STATUS"],
	}

	switch fields["STATUS"] {
	case "PASS":
		r.Status = gate.StatusPass
	case "FAIL":
		r.Status = gate.StatusFail
	default:
		r.Status = gate.StatusPending
	}

	if r.CopilotStatus == "" {
		r.CopilotStatus = "pending"
	}
	if r.LiteLLMStatus == "" {
		r.LiteLLMStatus = "pending"
	}

	if v, err := strconv.Atoi(fields["ELAPSED_SEC"]); err == nil {
		r.ElapsedSec = v
	}
	if v, err := strconv.Atoi(fields["POLL_AFTER_SEC"]); err == nil {
		r.PollAfterSec = v
	}
	if raw := fields["COMMENTS"]; raw != "" && raw != "none" {
		for _, c := range strings.Split(raw, "|") {
			if c = strings.TrimSpace(c); c != "" {
				r.Comments = append(r.Comments, c)
			}
		}
	}
	return r
}

// LoadCheckReport reads the last gate-check report from the canonical path
// inside repoRoot. Returns an error if the file does not exist or is invalid.
func LoadCheckReport(repoRoot string) (*gatecheck.Report, error) {
	path := filepath.Join(repoRoot, gatecheck.DefaultReportPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report gatecheck.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// ElapsedLabel formats elapsed seconds as a human-readable string.
func ElapsedLabel(sec int) string {
	if sec <= 0 {
		return "—"
	}
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	return fmt.Sprintf("%dm%ds", sec/60, sec%60)
}

func staticPipelineRows() []PipelineRow {
	return []PipelineRow{
		{Name: "gitleaks", Status: "pending", Note: "local · non-authoritative"},
		{Name: "semgrep", Status: "pending", Note: "local · non-authoritative"},
	}
}

// CheckViewModel is the display-ready model for a single gate check row.
type CheckViewModel struct {
	ID           string
	Name         string
	Group        string
	Status       string
	DisplayState string // normalized: "passing", "failing", or "disabled" — see LOG-001 contract
	Required     bool
	Enabled      bool
	ReasonCode   string
	Reason       string
	Tool         string
	DurMS        int64
}

// CheckSummaryViewModel holds the aggregated counters from a gate-check run.
type CheckSummaryViewModel struct {
	Overall          string
	Passed           int
	Failed           int
	Skipped          int
	InfraBypassed    int
	Disabled         int
	Total            int
	Profile          string
	RequiredFailed   int
	RequiredDisabled int
}

// CheckReportViewModel is the top-level TUI model for a gate-check report.
type CheckReportViewModel struct {
	Summary CheckSummaryViewModel
	Checks  []CheckViewModel
}

// StatusIcon returns an icon character for a check status string.
func StatusIcon(status string) string {
	switch status {
	case "pass":
		return "✓"
	case "fail":
		return "✗"
	case "skip":
		return "○"
	case "disabled":
		return "–"
	default:
		return "?"
	}
}

// FromCheckReport converts a gatecheck.Report into a CheckReportViewModel.
func FromCheckReport(r gatecheck.Report) CheckReportViewModel {
	checks := make([]CheckViewModel, 0, len(r.Checks))
	for _, c := range r.Checks {
		reasonCode := c.ReasonCode
		if reasonCode == "" && c.Reason == "" && (c.Status == gatecheck.StatusSkip || c.Status == gatecheck.StatusDisabled) {
			reasonCode = gatecheck.ReasonNotApplicable
		}
		reason := c.Reason
		if reason == "" && reasonCode != "" {
			reason = string(reasonCode)
		}
		tool := c.ToolName
		checks = append(checks, CheckViewModel{
			ID:           c.ID,
			Name:         c.Name,
			Group:        string(c.Group),
			Status:       string(c.Status),
			DisplayState: string(c.Status.ToDisplayState()),
			Required:     c.Required,
			Enabled:      c.Enabled,
			ReasonCode:   string(reasonCode),
			Reason:       reason,
			Tool:         tool,
			DurMS:        c.DurationMS,
		})
	}
	s := r.Summary
	return CheckReportViewModel{
		Summary: CheckSummaryViewModel{
			Overall:          string(s.OverallStatus),
			Passed:           s.Passed,
			Failed:           s.Failed,
			Skipped:          s.Skipped,
			InfraBypassed:    s.InfraBypassed,
			Disabled:         s.Disabled,
			Total:            s.Total,
			Profile:          string(s.Profile),
			RequiredFailed:   s.RequiredFailed,
			RequiredDisabled: s.RequiredDisabled,
		},
		Checks: checks,
	}
}

// DisplayStateIcon returns a monospace-friendly icon for a LOG-001 display state.
// Icons are chosen for readability on monospaced terminals and in diffing contexts.
func DisplayStateIcon(ds string) string {
	switch ds {
	case "passing":
		return "✓"
	case "failing":
		return "✗"
	case "disabled":
		return "–"
	default:
		return "?"
	}
}

// FormatDurationMS formats a duration in milliseconds as a short human-readable string.
// Returns an empty string for zero/negative values.
func FormatDurationMS(ms int64) string {
	if ms <= 0 {
		return ""
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
