package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codero/codero/internal/state"
	"github.com/spf13/cobra"
)

// ExitCriterion represents one measurable exit-gate requirement.
type ExitCriterion struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Threshold   string `json:"threshold"` // human-readable requirement, e.g. "≥ 30 days"
	Current     int    `json:"current"`
	Target      int    `json:"target"`
	Required    bool   `json:"required"`     // false = advisory
	ZeroIsPass  bool   `json:"zero_is_pass"` // true when target is = 0 (no incidents)
	Ready       bool   `json:"ready"`
	Gap         string `json:"gap,omitempty"` // how far from ready, empty when ready
}

// ExitGateReport is the full Phase 1F progress tracker output.
type ExitGateReport struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Overall     string          `json:"overall"` // "READY" | "NOT_READY"
	Criteria    []ExitCriterion `json:"criteria"`
	GapsToClose []string        `json:"gaps_to_close"`
	Summary     string          `json:"summary"` // one-line prose
}

// exitGateThresholds centralises all Phase 1F exit gate numeric requirements.
// These are derived from docs/runbooks/proving-scorecard-operations.md and
// docs/contracts/proving-scorecard-contract.md.
var exitGateThresholds = struct {
	ConsecutiveDays       int
	ActiveRepos           int
	BranchesReviewed7Days int
	StaleDetections30Days int
	LeaseExpiryRecoveries int
	PrecommitReviews7Days int
}{
	ConsecutiveDays:       30,
	ActiveRepos:           2,
	BranchesReviewed7Days: 3,
	StaleDetections30Days: 2,
	LeaseExpiryRecoveries: 1,
	PrecommitReviews7Days: 10,
}

// exitGateCmd computes Phase 1F exit-gate readiness and produces operator reports.
func exitGateCmd(configPath *string) *cobra.Command {
	var (
		outputFmt    string
		weeklyReport bool
		reposFile    string
		toolsDir     string
	)

	cmd := &cobra.Command{
		Use:   "exit-gate",
		Short: "Compute Phase 1F exit-gate readiness and produce proving report",
		Long: `Evaluate all Phase 1F exit criteria from current DB data and produce a
structured report showing READY / NOT_READY per criterion plus a "gaps to close"
section with concrete deltas.

Output formats:
  human   — aligned table (default)
  json    — machine-readable JSON
  markdown — paste-ready block for evidence docs

Use --weekly-report to generate a named evidence block that can be committed
directly to docs/runbooks/proving-evidence-2026-03.md.

Exit codes:
  0  all required criteria are READY
  1  one or more required criteria are NOT_READY

Examples:
  codero exit-gate
  codero exit-gate --output json
  codero exit-gate --output markdown --weekly-report`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExitGate(cmd.Context(), *configPath, exitGateOpts{
				outputFmt:    outputFmt,
				weeklyReport: weeklyReport,
				reposFile:    reposFile,
				toolsDir:     toolsDir,
			})
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "human",
		"output format: human | json | markdown")
	cmd.Flags().BoolVar(&weeklyReport, "weekly-report", false,
		"include weekly report block with evidence header")
	cmd.Flags().StringVar(&reposFile, "repos-file", "docs/managed-repos.txt",
		"managed repos list (used to verify preflight baseline)")
	cmd.Flags().StringVar(&toolsDir, "tools-dir", "/srv/storage/shared/tools/bin",
		"shared tools directory")

	return cmd
}

type exitGateOpts struct {
	outputFmt    string
	weeklyReport bool
	reposFile    string
	toolsDir     string
}

func runExitGate(ctx context.Context, configPath string, opts exitGateOpts) error {
	// Dependency checks
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("exit-gate: config: %w", err)
	}
	db, err := state.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("exit-gate: DB unavailable: %w", err)
	}
	defer db.Close()

	report, err := computeExitGateReport(ctx, db)
	if err != nil {
		return fmt.Errorf("exit-gate: compute: %w", err)
	}

	switch opts.outputFmt {
	case "json":
		return printExitGateJSON(report)
	case "markdown":
		return printExitGateMarkdown(report, opts.weeklyReport)
	default:
		printExitGateHuman(report, opts.weeklyReport)
	}

	if report.Overall == "NOT_READY" {
		return fmt.Errorf("exit-gate: NOT_READY — %d criteria unmet", countUnready(report))
	}
	return nil
}

// computeExitGateReport queries the DB and evaluates all exit criteria.
func computeExitGateReport(ctx context.Context, db *state.DB) (*ExitGateReport, error) {
	now := time.Now().UTC()
	t := exitGateThresholds

	// --- Collect all metric values ---
	consecutiveDays, err := state.CountConsecutiveDays(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("consecutive days: %w", err)
	}

	card, err := state.ComputeProvingScorecard(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("scorecard: %w", err)
	}

	activeRepos, err := state.CountActiveRepos(ctx, db, now.AddDate(0, 0, -7))
	if err != nil {
		return nil, fmt.Errorf("active repos: %w", err)
	}

	// --- Build criteria list ---
	criteria := []ExitCriterion{
		buildCrit("consecutive_days", "Consecutive days with daily snapshot", t.ConsecutiveDays,
			consecutiveDays, true, false),
		buildCrit("active_repos", "Active repos tracked (7d)", t.ActiveRepos,
			activeRepos, true, false),
		buildCrit("branches_reviewed_7d", "Branches reviewed per week", t.BranchesReviewed7Days,
			card.BranchesReviewed7Days, true, false),
		buildCrit("stale_detections_30d", "Stale detections observed (30d)", t.StaleDetections30Days,
			card.StaleDetections30Days, true, false),
		buildCrit("lease_expiry_recoveries", "Lease-expiry recoveries (30d)", t.LeaseExpiryRecoveries,
			card.LeaseExpiryRecoveries, true, false),
		buildCrit("precommit_reviews_7d", "Pre-commit reviews total (7d)", t.PrecommitReviews7Days,
			card.PrecommitReviews7Days, true, false),
		// Zero-incident requirements (required = true, ZeroIsPass = true)
		buildZeroCrit("zero_manual_db_repairs", "Zero manual DB repairs (30d, CRITICAL)",
			card.ManualDBRepairs, true),
		buildZeroCrit("zero_missed_deliveries", "Zero missed feedback deliveries (CRITICAL)",
			card.MissedFeedbackDeliveries, true),
		buildZeroCrit("zero_queue_stalls", "Zero queue stall incidents (30d)",
			card.QueueStallIncidents, false),
		buildZeroCrit("zero_unresolved_failures", "Zero unresolved thread failures (30d)",
			card.UnresolvedThreadFailures, false),
	}

	// Compute overall and gaps
	var gaps []string
	allReady := true
	for i, c := range criteria {
		if !c.Ready && c.Required {
			allReady = false
		}
		if !c.Ready {
			gaps = append(gaps, c.Gap)
		}
		criteria[i] = c
	}

	overall := "READY"
	if !allReady {
		overall = "NOT_READY"
	}

	summary := buildSummary(overall, criteria)

	return &ExitGateReport{
		GeneratedAt: now,
		Overall:     overall,
		Criteria:    criteria,
		GapsToClose: gaps,
		Summary:     summary,
	}, nil
}

func buildCrit(name, desc string, target, current int, required, zeroIsPass bool) ExitCriterion {
	ready := current >= target
	gap := ""
	if !ready {
		delta := target - current
		gap = fmt.Sprintf("%s: need %d more (have %d, need ≥ %d)", name, delta, current, target)
	}
	return ExitCriterion{
		Name:        name,
		Description: desc,
		Threshold:   fmt.Sprintf("≥ %d", target),
		Current:     current,
		Target:      target,
		Required:    required,
		ZeroIsPass:  zeroIsPass,
		Ready:       ready,
		Gap:         gap,
	}
}

func buildZeroCrit(name, desc string, current int, required bool) ExitCriterion {
	ready := current == 0
	gap := ""
	if !ready {
		gap = fmt.Sprintf("%s: must be 0 but is %d — investigate immediately", name, current)
	}
	return ExitCriterion{
		Name:        name,
		Description: desc,
		Threshold:   "= 0",
		Current:     current,
		Target:      0,
		Required:    required,
		ZeroIsPass:  true,
		Ready:       ready,
		Gap:         gap,
	}
}

func buildSummary(overall string, criteria []ExitCriterion) string {
	ready, total := 0, len(criteria)
	for _, c := range criteria {
		if c.Ready {
			ready++
		}
	}
	return fmt.Sprintf("Phase 1F exit-gate: %s — %d/%d criteria met", overall, ready, total)
}

func countUnready(r *ExitGateReport) int {
	n := 0
	for _, c := range r.Criteria {
		if !c.Ready && c.Required {
			n++
		}
	}
	return n
}

// printExitGateHuman renders a human-readable aligned table.
func printExitGateHuman(r *ExitGateReport, weekly bool) {
	if weekly {
		fmt.Printf("## Weekly Exit-Gate Report — %s\n\n", r.GeneratedAt.Format("2006-01-02"))
	}

	overallStyle := "✅ READY"
	if r.Overall == "NOT_READY" {
		overallStyle = "⚠️  NOT_READY"
	}
	fmt.Printf("Phase 1F Exit Gate: %s  (generated %s)\n\n",
		overallStyle, r.GeneratedAt.Format("2006-01-02 15:04 UTC"))

	fmt.Printf("%-32s  %-8s  %-9s  %-6s  %s\n", "CRITERION", "TARGET", "CURRENT", "STATUS", "REQ")
	fmt.Println(strings.Repeat("─", 78))
	for _, c := range r.Criteria {
		status := "✓ READY"
		if !c.Ready {
			if c.Required {
				status = "✗ UNMET"
			} else {
				status = "△ UNMET"
			}
		}
		req := "required"
		if !c.Required {
			req = "advisory"
		}
		fmt.Printf("%-32s  %-8s  %-9d  %-6s  %s\n",
			truncEG(c.Name, 32), c.Threshold, c.Current, status, req)
	}

	if len(r.GapsToClose) > 0 {
		fmt.Println("\nGaps to close:")
		for _, g := range r.GapsToClose {
			fmt.Printf("  • %s\n", g)
		}
	} else {
		fmt.Println("\nNo gaps — all criteria met.")
	}

	fmt.Printf("\n%s\n", r.Summary)
}

// printExitGateJSON emits machine-readable JSON to stdout.
func printExitGateJSON(r *ExitGateReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// printExitGateMarkdown emits a paste-ready Markdown block.
func printExitGateMarkdown(r *ExitGateReport, weekly bool) error {
	header := "## Exit-Gate Status"
	if weekly {
		header = fmt.Sprintf("## Weekly Proving Report — %s", r.GeneratedAt.Format("2006-01-02"))
	}

	fmt.Println(header)
	fmt.Printf("\n**Generated:** %s  \n", r.GeneratedAt.Format("2006-01-02 15:04 UTC"))
	overall := "✅ **READY**"
	if r.Overall == "NOT_READY" {
		overall = "⚠️ **NOT_READY**"
	}
	fmt.Printf("**Overall:** %s\n\n", overall)
	fmt.Println("| Criterion | Target | Current | Status |")
	fmt.Println("|-----------|--------|---------|--------|")
	for _, c := range r.Criteria {
		status := "✅ READY"
		if !c.Ready {
			if c.Required {
				status = "❌ UNMET"
			} else {
				status = "⚠️ UNMET"
			}
		}
		fmt.Printf("| %s | %s | %d | %s |\n", c.Name, c.Threshold, c.Current, status)
	}

	if len(r.GapsToClose) > 0 {
		fmt.Println("\n**Gaps to close:**")
		for _, g := range r.GapsToClose {
			fmt.Printf("- %s\n", g)
		}
	}

	fmt.Printf("\n*%s*\n", r.Summary)
	return nil
}

func truncEG(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
