package tui

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/tui/adapters"
)

// RenderCheckReportSnapshot renders a deterministic plain-text view of the
// gate-check report for non-interactive parity validation.
func RenderCheckReportSnapshot(report gatecheck.Report) string {
	return renderCheckReport(adapters.FromCheckReport(report))
}

func renderCheckReport(vm adapters.CheckReportViewModel) string {
	var b strings.Builder
	s := vm.Summary

	fmt.Fprintln(&b, "GATE CHECKS")
	fmt.Fprintf(&b, "Summary  overall=%s  pass=%d  fail=%d  skip=%d  infra=%d  disabled=%d  total=%d  profile=%s\n\n",
		s.Overall, s.Passed, s.Failed, s.Skipped, s.InfraBypassed, s.Disabled, s.Total, s.Profile)

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tGROUP\tDISPLAY\tSTATUS\tREQ\tREASON")
	fmt.Fprintln(tw, "----------------------\t----------\t----------\t------------\t---\t----------------------------------")
	for _, check := range vm.Checks {
		req := "opt"
		if check.Required {
			req = "req"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			check.ID,
			check.Group,
			check.DisplayState,
			check.Status,
			req,
			gatecheck.DisplayReason(gatecheck.ReasonCode(check.ReasonCode), check.Reason),
		)
	}
	tw.Flush()

	return b.String()
}
