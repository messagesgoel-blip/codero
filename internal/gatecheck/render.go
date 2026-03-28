package gatecheck

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// CheckViewModel is the display-ready model for a single gate check row.
type CheckViewModel struct {
	ID           string
	Name         string
	Group        string
	Status       string
	DisplayState string // normalized: "passing", "failing", or "disabled"
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

// CheckReportViewModel is the top-level display model for a gate-check report.
type CheckReportViewModel struct {
	Summary CheckSummaryViewModel
	Checks  []CheckViewModel
}

// FromCheckReport converts a Report into a CheckReportViewModel.
func FromCheckReport(r Report) CheckReportViewModel {
	checks := make([]CheckViewModel, 0, len(r.Checks))
	for _, c := range r.Checks {
		reasonCode := c.ReasonCode
		if reasonCode == "" && c.Reason == "" && (c.Status == StatusSkip || c.Status == StatusDisabled) {
			reasonCode = ReasonNotApplicable
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

// DisplayStateIcon returns a monospace-friendly icon for a display state.
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

const snapshotSep = "════════════════════════════════════════════════════════════"

// RenderCheckReportSnapshot renders a deterministic plain-text view of the
// gate-check report for non-interactive parity validation.
// Output contains no ANSI escape sequences and is stable across runs.
func RenderCheckReportSnapshot(report Report) string {
	return renderCheckReport(FromCheckReport(report))
}

func renderCheckReport(vm CheckReportViewModel) string {
	var b strings.Builder
	s := vm.Summary

	// ── header ──────────────────────────────────────────────────────────────
	fmt.Fprintln(&b, "GATE CHECKS")
	fmt.Fprintln(&b, snapshotSep)

	// ── summary block ────────────────────────────────────────────────────────
	fmt.Fprintf(&b, "OVERALL  %s\n", strings.ToUpper(s.Overall))
	fmt.Fprintf(&b, "PROFILE  %s\n", s.Profile)
	fmt.Fprintf(&b, "COUNTS   pass=%-3d  fail=%-3d  skip=%-3d  disabled=%-3d  total=%d",
		s.Passed, s.Failed, s.Skipped, s.Disabled, s.Total)
	if s.InfraBypassed > 0 {
		fmt.Fprintf(&b, "  infra=%d", s.InfraBypassed)
	}
	fmt.Fprintln(&b)
	if s.RequiredFailed > 0 || s.RequiredDisabled > 0 {
		fmt.Fprintf(&b, "REQUIRED failed=%d  disabled=%d\n", s.RequiredFailed, s.RequiredDisabled)
	}

	fmt.Fprintln(&b, snapshotSep)
	fmt.Fprintln(&b)

	// ── per-check table ──────────────────────────────────────────────────────
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tGROUP\tDISPLAY\tSTATUS\tREQ\tDUR\tREASON")
	fmt.Fprintln(tw, "----------------------\t----------\t----------\t------------\t---\t-------\t----------------------------------")
	for _, check := range vm.Checks {
		req := "opt"
		if check.Required {
			req = "req"
		}
		dur := FormatDurationMS(check.DurMS)
		if dur == "" {
			dur = "—"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			check.ID,
			check.Group,
			check.DisplayState,
			check.Status,
			req,
			dur,
			DisplayReason(ReasonCode(check.ReasonCode), check.Reason),
		)
	}
	tw.Flush()

	return b.String()
}
