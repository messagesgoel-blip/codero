package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/codero/codero/internal/state"
	"github.com/spf13/cobra"
)

// sessionMetricsCmd implements `codero session metrics [session_id]`.
// Prints token usage and context pressure for a session.
func sessionMetricsCmd(configPath *string) *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "metrics [session_id]",
		Short: "Show token usage and context pressure for a session",
		Example: `  codero session metrics
  codero session metrics <session-id>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sid := sessionID
			if len(args) > 0 {
				sid = args[0]
			}
			if sid == "" {
				sid = resolveSessionIDFromEnv()
			}
			if sid == "" {
				return fmt.Errorf("session ID required (pass as argument or set $CODERO_SESSION_ID / $CODERO_AGENT_SESSION_ID)")
			}

			cfg, err := loadConfig(*configPathForCmd(cmd))
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}
			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()

			summary, err := state.GetTokenMetricSummary(cmd.Context(), db, sid)
			if err != nil {
				return fmt.Errorf("session metrics: %w", err)
			}

			// ── Summary header ──────────────────────────────────────────────
			pressureLabel := summary.ContextPressure
			if pressureLabel == "" {
				pressureLabel = "normal"
			}
			pressureIcon := "✓"
			switch pressureLabel {
			case "warning":
				pressureIcon = "⚠"
			case "critical":
				pressureIcon = "✗"
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Session: %s\n", sid)
			fmt.Fprintf(out, "Context pressure: %s %s", pressureIcon, pressureLabel)
			if summary.CompactCount > 0 {
				fmt.Fprintf(out, "  (compacted %d time(s)", summary.CompactCount)
				if summary.LastCompactAt != nil {
					fmt.Fprintf(out, ", last %s", summary.LastCompactAt.Format("2006-01-02 15:04:05"))
				}
				fmt.Fprint(out, ")")
			}
			fmt.Fprintln(out)
			fmt.Fprintln(out)

			// ── Token totals ─────────────────────────────────────────────────
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Metric\tValue\n")
			fmt.Fprintf(w, "------\t-----\n")
			fmt.Fprintf(w, "Total requests\t%d\n", summary.TotalRequests)
			fmt.Fprintf(w, "Total prompt tokens\t%s\n", fmtTokens(summary.TotalPromptTokens))
			fmt.Fprintf(w, "Total completion tokens\t%s\n", fmtTokens(summary.TotalCompletionTokens))
			fmt.Fprintf(w, "Peak cumulative prompt\t%s\n", fmtTokens(summary.PeakCumulativePromptTokens))
			if summary.TotalRequests > 0 {
				fmt.Fprintf(w, "Avg prompt/request\t%.0f\n", summary.AvgPromptPerRequest)
			}
			fmt.Fprintf(w, "Models used\t%s\n", strings.Join(summary.Models, ", "))
			_ = w.Flush()

			// ── Per-request breakdown (last 10) ──────────────────────────────
			if summary.TotalRequests > 0 {
				rows, err := state.GetTokenMetrics(cmd.Context(), db, sid)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not load per-request metrics for session %s: %v\n", sid, err)
				} else if len(rows) > 0 {
					fmt.Fprintln(out)
					fmt.Fprintf(out, "Recent requests (last %d of %d):\n", min(10, len(rows)), len(rows))
					w2 := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
					fmt.Fprintf(w2, "Time\tModel\tPrompt\tCompletion\tCumulative\n")
					fmt.Fprintf(w2, "----\t-----\t------\t----------\t----------\n")
					start := 0
					if len(rows) > 10 {
						start = len(rows) - 10
					}
					for _, r := range rows[start:] {
						fmt.Fprintf(w2, "%s\t%s\t%s\t%s\t%s\n",
							r.RequestTime.Format("15:04:05"),
							shortModel(r.Model),
							fmtTokens(r.PromptTokens),
							fmtTokens(r.CompletionTokens),
							fmtTokens(r.CumulativePromptTokens),
						)
					}
					_ = w2.Flush()
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (defaults to $CODERO_SESSION_ID / $CODERO_AGENT_SESSION_ID)")
	return cmd
}

func fmtTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func shortModel(model string) string {
	// Trim provider prefix (e.g. "openai/gpt-4o" → "gpt-4o")
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	if len(model) > 24 {
		return model[:21] + "..."
	}
	return model
}
