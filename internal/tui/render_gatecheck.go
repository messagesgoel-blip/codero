package tui

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/tui/adapters"
)

const snapshotSep = "════════════════════════════════════════════════════════════"

// RenderCheckReportSnapshot renders a deterministic plain-text view of the
// gate-check report for non-interactive parity validation.
// Output contains no ANSI escape sequences and is stable across runs.
func RenderCheckReportSnapshot(report gatecheck.Report) string {
	return renderCheckReport(adapters.FromCheckReport(report))
}

func renderCheckReport(vm adapters.CheckReportViewModel) string {
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
		dur := adapters.FormatDurationMS(check.DurMS)
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
			gatecheck.DisplayReason(gatecheck.ReasonCode(check.ReasonCode), check.Reason),
		)
	}
	tw.Flush()

	return b.String()
}
