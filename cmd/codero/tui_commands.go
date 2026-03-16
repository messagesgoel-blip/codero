package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui"
	"github.com/codero/codero/internal/tui/adapters"
	"github.com/spf13/cobra"
)

// queueCmd displays the current queue state for a repository.
func queueCmd(configPath *string) *cobra.Command {
	var repo string

	cmd := &cobra.Command{
		Use:   "queue [repo]",
		Short: "Show queue state for a repository",
		Long:  "Displays all branches currently in the queue for a repository, ordered by priority and wait time.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			if len(args) == 1 {
				repo = args[0]
			}

			// Resolve repo from args/flag or config default
			if repo == "" {
				if len(cfg.Repos) == 0 {
					return fmt.Errorf("no repositories configured")
				}
				repo = cfg.Repos[0]
			}

			// Open state store
			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			// Get queued branches
			branches, err := state.ListBranchesByState(db, state.StateQueuedCLI)
			if err != nil {
				return fmt.Errorf("list queued branches: %w", err)
			}

			// Filter by repo
			var filtered []state.BranchRecord
			for _, b := range branches {
				if b.Repo == repo {
					filtered = append(filtered, b)
				}
			}

			sort.SliceStable(filtered, func(i, j int) bool {
				if filtered[i].QueuePriority != filtered[j].QueuePriority {
					return filtered[i].QueuePriority > filtered[j].QueuePriority
				}
				return filtered[i].UpdatedAt.Before(filtered[j].UpdatedAt)
			})

			if len(filtered) == 0 {
				fmt.Printf("No branches in queue for %s\n", repo)
				return nil
			}

			// Print table
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "BRANCH\tPRIORITY\tRETRY\tSTATE\n")
			fmt.Fprintf(w, "-------\t--------\t-----\t-----\n")
			for _, b := range filtered {
				fmt.Fprintf(w, "%s\t%d\t%d/%d\t%s\n",
					b.Branch, b.QueuePriority, b.RetryCount, b.MaxRetries, b.State)
			}
			w.Flush()

			fmt.Printf("\nTotal: %d branches\n", len(filtered))
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")

	return cmd
}

// branchCmd displays detailed information about a specific branch.
func branchCmd(configPath *string) *cobra.Command {
	var (
		repo   string
		branch string
	)

	cmd := &cobra.Command{
		Use:   "branch [branch]",
		Short: "Show branch details",
		Long:  "Displays detailed information about a specific branch including state, retry count, and merge readiness.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			// Resolve branch from args or git
			if len(args) > 0 {
				branch = args[0]
			} else {
				branch, err = getCurrentBranch()
				if err != nil {
					return fmt.Errorf("get current branch: %w", err)
				}
			}

			// Resolve repo
			if repo == "" {
				if len(cfg.Repos) == 0 {
					return fmt.Errorf("no repositories configured")
				}
				repo = cfg.Repos[0]
			}

			// Open state store
			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			// Get branch
			b, err := state.GetBranch(db, repo, branch)
			if err != nil {
				if errors.Is(err, state.ErrBranchNotFound) {
					return fmt.Errorf("branch %s/%s not found", repo, branch)
				}
				return fmt.Errorf("get branch: %w", err)
			}

			// Print branch details
			head := "(none)"
			if len(b.HeadHash) >= 8 {
				head = b.HeadHash[:8]
			} else if len(b.HeadHash) > 0 {
				head = b.HeadHash
			}

			fmt.Printf("Branch: %s/%s\n", b.Repo, b.Branch)
			fmt.Printf("State: %s\n", b.State)
			fmt.Printf("HEAD: %s\n", head)
			fmt.Printf("Priority: %d\n", b.QueuePriority)
			fmt.Printf("Retries: %d/%d\n", b.RetryCount, b.MaxRetries)
			fmt.Printf("Approved: %v\n", b.Approved)
			fmt.Printf("CI Green: %v\n", b.CIGreen)
			fmt.Printf("Pending Events: %d\n", b.PendingEvents)
			fmt.Printf("Unresolved Threads: %d\n", b.UnresolvedThreads)

			if b.LeaseID != nil {
				fmt.Printf("Lease: %s (expires %v)\n", *b.LeaseID, b.LeaseExpiresAt)
			}

			// Compute merge ready
			mergeReady := b.Approved && b.CIGreen && b.PendingEvents == 0 && b.UnresolvedThreads == 0
			fmt.Printf("Merge Ready: %v\n", mergeReady)

			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")

	return cmd
}

// eventsCmd displays delivery events for a branch.
func eventsCmd(configPath *string) *cobra.Command {
	var (
		repo   string
		branch string
		since  int64
		limit  int
	)

	cmd := &cobra.Command{
		Use:   "events [branch]",
		Short: "Show delivery events for a branch",
		Long:  "Displays delivery events for a specific branch, useful for debugging and auditing.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			// Resolve branch from args or git
			if len(args) > 0 {
				branch = args[0]
			} else {
				branch, err = getCurrentBranch()
				if err != nil {
					return fmt.Errorf("get current branch: %w", err)
				}
			}

			// Resolve repo
			if repo == "" {
				if len(cfg.Repos) == 0 {
					return fmt.Errorf("no repositories configured")
				}
				repo = cfg.Repos[0]
			}

			// Open state store
			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			// Get events
			events, err := state.ListDeliveryEvents(db, repo, branch, since)
			if err != nil {
				return fmt.Errorf("list events: %w", err)
			}

			if len(events) == 0 {
				fmt.Printf("No events found for %s/%s\n", repo, branch)
				return nil
			}

			// Apply limit
			if limit > 0 && len(events) > limit {
				events = events[len(events)-limit:]
			}

			// Print events
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "SEQ\tTYPE\tTIMESTAMP\n")
			fmt.Fprintf(w, "---\t----\t---------\n")
			for _, e := range events {
				fmt.Fprintf(w, "%d\t%s\t%s\n", e.Seq, e.EventType, e.CreatedAt.Format("2006-01-02 15:04:05"))
			}
			w.Flush()

			fmt.Printf("\nTotal: %d events\n", len(events))
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")
	cmd.Flags().Int64Var(&since, "since", 0, "show events with seq > since")
	cmd.Flags().IntVar(&limit, "limit", 20, "limit number of events shown")

	return cmd
}

// scorecardCmd generates and displays the proving period scorecard.
func scorecardCmd(configPath *string) *cobra.Command {
	var (
		outputFormat string
		saveSnapshot bool
		snapshotDir  string
	)

	cmd := &cobra.Command{
		Use:   "scorecard",
		Short: "Generate proving period scorecard",
		Long: `Generates a daily scorecard for Phase 1F proving period tracking.

The scorecard aggregates metrics from the last 7-30 days:
  - branches reviewed (7 days)
  - stale detections (30 days)
  - lease-expiry recoveries (30 days)
  - pre-commit reviews per project per week
  - missed feedback deliveries
  - queue stall incidents (30 days)
  - unresolved-thread failures (30 days)
  - manual DB repair incidents (30 days)

Output formats:
  - human: human-readable table (default)
  - json: machine-readable JSON for scripts
  - both: both human and JSON output

Use --save to persist a snapshot for 30-day sign-off evidence.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			card, err := state.ComputeProvingScorecard(cmd.Context(), db)
			if err != nil {
				return fmt.Errorf("compute scorecard: %w", err)
			}

			switch outputFormat {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(card); err != nil {
					return fmt.Errorf("encode json: %w", err)
				}

			case "human":
				printScorecardHuman(card)

			case "both":
				printScorecardHuman(card)
				fmt.Println("\n--- JSON ---")
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(card); err != nil {
					return fmt.Errorf("encode json: %w", err)
				}

			default:
				return fmt.Errorf("unknown output format: %s (use: human, json, both)", outputFormat)
			}

			if saveSnapshot {
				snapshotDate := time.Now().Format("2006-01-02")
				cardJSON, err := json.Marshal(card)
				if err != nil {
					return fmt.Errorf("marshal scorecard: %w", err)
				}

				if err := state.SaveProvingSnapshot(cmd.Context(), db, snapshotDate, string(cardJSON)); err != nil {
					return fmt.Errorf("save snapshot: %w", err)
				}

				snapshotPath := snapshotDate
				if snapshotDir != "" {
					if err := os.MkdirAll(snapshotDir, 0755); err != nil {
						return fmt.Errorf("create snapshot directory: %w", err)
					}
					snapshotPath = filepath.Join(snapshotDir, snapshotDate+".json")
					if err := os.WriteFile(snapshotPath, cardJSON, 0644); err != nil {
						return fmt.Errorf("write snapshot file: %w", err)
					}
				}
				fmt.Fprintln(os.Stderr, "\nSnapshot saved:", snapshotPath)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "human", "output format: human, json, both")
	cmd.Flags().BoolVar(&saveSnapshot, "save", false, "save snapshot to database and optionally to file")
	cmd.Flags().StringVar(&snapshotDir, "snapshot-dir", "", "directory to write snapshot file (optional)")

	return cmd
}

func printScorecardHuman(card *state.ProvingScorecard) {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║           CODERO Phase 1F Proving Period Scorecard          ║")
	fmt.Println("╠════════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Generated: %-48s║\n", card.GeneratedAt.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("║ Period:    %s to %s  ║\n", card.PeriodStart.Format("2006-01-02"), card.PeriodEnd.Format("2006-01-02"))
	fmt.Println("╠════════════════════════════════════════════════════════════╣")
	fmt.Println("║ METRIC                              VALUE                  ║")
	fmt.Println("╠════════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Branches reviewed (7 days)          %-22d║\n", card.BranchesReviewed7Days)
	fmt.Printf("║ Stale detections (30 days)          %-22d║\n", card.StaleDetections30Days)
	fmt.Printf("║ Lease-expiry recoveries (30 days)   %-22d║\n", card.LeaseExpiryRecoveries)
	fmt.Printf("║ Pre-commit reviews (7 days)         %-22d║\n", card.PrecommitReviews7Days)
	fmt.Printf("║ Missed feedback deliveries          %-22d║\n", card.MissedFeedbackDeliveries)
	fmt.Printf("║ Queue stall incidents (30 days)     %-22d║\n", card.QueueStallIncidents)
	fmt.Printf("║ Unresolved thread failures (30 days)%-22d║\n", card.UnresolvedThreadFailures)
	fmt.Printf("║ Manual DB repairs (30 days)         %-22d║\n", card.ManualDBRepairs)
	fmt.Println("╠════════════════════════════════════════════════════════════╣")

	if len(card.PrecommitReviewsByRepo) > 0 {
		fmt.Println("║ PRE-COMMIT REVIEWS BY REPO                                  ║")
		for repo, count := range card.PrecommitReviewsByRepo {
			fmt.Printf("║   %-34s %-18d║\n", repo, count)
		}
		fmt.Println("╠════════════════════════════════════════════════════════════╣")
	}

	if len(card.BranchesReviewedByRepo) > 0 {
		fmt.Println("║ BRANCHES REVIEWED BY REPO                                   ║")
		for repo, count := range card.BranchesReviewedByRepo {
			fmt.Printf("║   %-34s %-18d║\n", repo, count)
		}
		fmt.Println("╠════════════════════════════════════════════════════════════╣")
	}

	phase1ExitStatus := "IN PROGRESS"
	if card.ManualDBRepairs > 0 || card.MissedFeedbackDeliveries > 0 {
		phase1ExitStatus = "NEEDS ATTENTION"
	} else if card.BranchesReviewed7Days >= 3 && card.StaleDetections30Days >= 2 && card.LeaseExpiryRecoveries >= 1 && card.PrecommitReviews7Days >= 10 {
		phase1ExitStatus = "ON TRACK"
	}

	fmt.Printf("║ Phase 1 Exit Status: %-39s║\n", phase1ExitStatus)
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
}

// recordProvingEventCmd records a proving event for tracking operational incidents.
func recordProvingEventCmd(configPath *string) *cobra.Command {
	var (
		repo      string
		eventType string
		details   string
	)

	cmd := &cobra.Command{
		Use:   "record-event",
		Short: "Record a proving period event",
		Long: `Records an explicit proving period event for metrics tracking.

Event types:
  - queue_stall: queue was stalled (all eligible items blocked)
  - manual_db_repair: manual database repair was performed
  - unresolved_thread_failure: unresolved thread check failed
  - missed_delivery: a feedback delivery was missed

These events are tracked for Phase 1F exit gate compliance.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if eventType == "" {
				return fmt.Errorf("--type is required")
			}

			validTypes := map[string]bool{
				"queue_stall":               true,
				"manual_db_repair":          true,
				"unresolved_thread_failure": true,
				"missed_delivery":           true,
			}
			if !validTypes[eventType] {
				return fmt.Errorf("invalid event type: %s (valid: queue_stall, manual_db_repair, unresolved_thread_failure, missed_delivery)", eventType)
			}

			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			if err := state.CreateProvingEvent(cmd.Context(), db, eventType, repo, details); err != nil {
				return fmt.Errorf("create proving event: %w", err)
			}

			fmt.Printf("Recorded proving event: type=%s repo=%s\n", eventType, repo)
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")
	cmd.Flags().StringVarP(&eventType, "type", "t", "", "event type (required)")
	cmd.Flags().StringVar(&details, "details", "", "additional details (JSON)")

	return cmd
}

// recordPrecommitCmd records a pre-commit review for proving period tracking.
func recordPrecommitCmd(configPath *string) *cobra.Command {
	var (
		repo     string
		branch   string
		provider string
		status   string
	)

	cmd := &cobra.Command{
		Use:   "record-precommit",
		Short: "Record a pre-commit review result",
		Long: `Records a pre-commit review result for Phase 1F proving period tracking.

This command records the outcome of a pre-commit review (LiteLLM or CodeRabbit)
so the precommit_reviews_7_days metric can track pre-commit enforcement activity.

Provider: "litellm" or "coderabbit"
Status: "passed", "failed", or "error"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if repo == "" || branch == "" || provider == "" || status == "" {
				return fmt.Errorf("--repo, --branch, --provider, and --status are required")
			}

			validProviders := map[string]bool{"litellm": true, "coderabbit": true, "copilot": true}
			validStatuses := map[string]bool{"passed": true, "failed": true, "error": true}
			if !validProviders[provider] {
				return fmt.Errorf("invalid provider: %s (valid: copilot, litellm, coderabbit)", provider)
			}
			if !validStatuses[status] {
				return fmt.Errorf("invalid status: %s (valid: passed, failed, error)", status)
			}

			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			review := &state.PrecommitReview{
				ID:       generatePrecommitID(),
				Repo:     repo,
				Branch:   branch,
				Provider: provider,
				Status:   status,
			}

			if err := state.CreatePrecommitReview(cmd.Context(), db, review); err != nil {
				return fmt.Errorf("create precommit review: %w", err)
			}

			fmt.Printf("Recorded precommit review: repo=%s branch=%s provider=%s status=%s\n",
				repo, branch, provider, status)
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo) (required)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "branch name (required)")
	cmd.Flags().StringVar(&provider, "provider", "", "review provider: copilot, litellm, or coderabbit (required)")
	cmd.Flags().StringVar(&status, "status", "", "review result: passed, failed, or error (required)")

	return cmd
}

func generatePrecommitID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("pc-fallback-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("pc-%s", hex.EncodeToString(b))
}

// gateStatusCmd shows the current pre-commit gate status in a rich terminal TUI.
//
// It reads from the same .codero/gate-heartbeat/progress.env source as the /gate
// dashboard endpoint, guaranteeing that TUI and dashboard display identical state.
//
// Flags:
//
//	--watch        Poll progress.env every interval and redraw until terminal state
//	--interval     Polling interval in seconds for --watch (default: 5)
//	--logs         Print the gate log directory path and last entries
//	--repo-path    Path to repository root (default: current directory)
func gateStatusCmd() *cobra.Command {
	var (
		repoPath    string
		watchMode   bool
		intervalSec int
		showLogs    bool
		jsonOutput  bool
		noPrompt    bool
	)

	cmd := &cobra.Command{
		Use:   "gate-status",
		Short: "Show pre-commit gate status in TUI view",
		Long: `Display the current pre-commit gate status with a rich terminal layout.

Reads from .codero/gate-heartbeat/progress.env — the same source used by the
/gate observability endpoint — ensuring CLI/TUI/dashboard display parity.

The TUI shows:
  - Overall STATUS (PENDING / PASS / FAIL) with visual indicator
  - Per-gate progress bar (copilot and litellm), icons identical to commit-gate
  - Current active gate and elapsed time
  - Blocker comments explaining why the gate failed (if FAIL)
  - Actionable next-step hints for common interventions

In non-interactive (pipe/CI) contexts the command never prompts for input and
exits with code 1 when the gate is in FAIL state, 0 for PASS/PENDING.

Flags:
  --watch         poll and redraw until PASS or FAIL (Bubble Tea TUI)
  --interval      polling interval in seconds (for --watch, default 5)
  --logs          print gate log directory path and last entries
  --json          emit machine-readable JSON to stdout (one-shot, no TUI; if combined with --watch, --json wins)
  --no-prompt     disable interactive action prompt even when in a TTY

Examples:
  codero gate-status                    # one-shot display
  codero gate-status --watch            # live display, redraws every 5s
  codero gate-status --watch --interval 10
  codero gate-status --logs             # show gate log path and last entries
  codero gate-status --json             # scriptable JSON output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if repoPath == "" {
				absPath, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getwd: %w", err)
				}
				repoPath = absPath
			}
			if jsonOutput && showLogs {
				return fmt.Errorf("--json cannot be combined with --logs")
			}
			// When --json is set alongside --watch, --json wins (non-blocking, no TUI).
			if jsonOutput && watchMode {
				watchMode = false
			}

			if showLogs {
				return printGateLogs(repoPath)
			}

			if watchMode {
				return runGateStatusWatch(repoPath, intervalSec)
			}

			result := readProgressEnvAsResult(repoPath)

			if jsonOutput {
				if err := printGateStatusJSON(result); err != nil {
					return err
				}
			} else {
				fmt.Print(RenderGateStatusBox(result, repoPath))
			}

			ttyInteractive := tui.IsInteractiveTTY()
			promptInteractive := ttyInteractive && !noPrompt && !jsonOutput
			if promptInteractive {
				printGateActions(result, repoPath)
			}

			// In non-interactive mode, exit 1 on FAIL so scripts can detect it.
			if !ttyInteractive && result.Status == gate.StatusFail {
				return fmt.Errorf("gate status: FAIL")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&repoPath, "repo-path", "r", "", "path to repository (default: current directory)")
	cmd.Flags().BoolVarP(&watchMode, "watch", "w", false, "poll and redraw until PASS or FAIL")
	cmd.Flags().IntVar(&intervalSec, "interval", 5, "polling interval in seconds (for --watch)")
	cmd.Flags().BoolVarP(&showLogs, "logs", "l", false, "show gate log directory path and last log entries")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit gate status as JSON (scriptable, non-interactive)")
	cmd.Flags().BoolVar(&noPrompt, "no-prompt", false, "disable interactive action prompt even in a TTY")

	return cmd
}

// printGateStatusJSON emits the gate result as a JSON object to stdout.
// This is the machine-readable path used by --json; it never prompts.
func printGateStatusJSON(r gate.Result) error {
	out := struct {
		Status        string   `json:"status"`
		CopilotStatus string   `json:"copilot_status"`
		LiteLLMStatus string   `json:"litellm_status"`
		CurrentGate   string   `json:"current_gate"`
		RunID         string   `json:"run_id"`
		Comments      []string `json:"comments"`
		ProgressBar   string   `json:"progress_bar"`
	}{
		Status:        string(r.Status),
		CopilotStatus: r.CopilotStatus,
		LiteLLMStatus: r.LiteLLMStatus,
		CurrentGate:   r.CurrentGate,
		RunID:         r.RunID,
		Comments:      r.Comments,
		ProgressBar:   r.ProgressBar,
	}
	if out.Comments == nil {
		out.Comments = []string{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// runGateStatusWatch runs the Bubble Tea TUI to display gate status.
// When stdin or stdout is not a TTY (CI, pipe, Docker non-interactive), the TUI
// cannot be initialised safely.  In that case the function falls back to
// emitting a single JSON object using the same schema as gate-status --json
// and returns immediately.
func runGateStatusWatch(repoPath string, intervalSec int) error {
	if !tui.IsInteractiveTTY() {
		// Non-TTY fallback: emit one JSON snapshot and exit cleanly.
		result := readProgressEnvAsResult(repoPath)
		return printGateStatusJSON(result)
	}

	if intervalSec < 1 {
		intervalSec = 5
	}
	interval := time.Duration(intervalSec) * time.Second

	initialVM := tui.AdapterFromPath(repoPath)
	cfg := tui.Config{
		RepoPath:  repoPath,
		Interval:  interval,
		Theme:     tui.DefaultTheme,
		WatchMode: true,
		InitialVM: initialVM,
	}
	p := tea.NewProgram(tui.New(cfg), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// readProgressEnvAsResult reads .codero/gate-heartbeat/progress.env and converts
// it to a gate.Result. Uses the same parsing logic as the /gate endpoint.
// Returns a default pending result when no progress file exists.
func readProgressEnvAsResult(repoPath string) gate.Result {
	progressFile := filepath.Join(repoPath, ".codero", "gate-heartbeat", "progress.env")
	data, err := os.ReadFile(progressFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return gate.Result{
				Status:        gate.StatusFail,
				CopilotStatus: "error",
				LiteLLMStatus: "error",
				Comments:      []string{fmt.Sprintf("failed to read progress file: %v", err)},
			}
		}
		return gate.Result{
			Status:        gate.StatusPending,
			CopilotStatus: "pending",
			LiteLLMStatus: "pending",
		}
	}
	return parseEnvToResult(string(data))
}

// parseEnvToResult converts KEY=VALUE pairs from progress.env into a gate.Result.
// This mirrors the field mapping used by the /gate observability endpoint.
func parseEnvToResult(envContent string) gate.Result {
	return adapters.ParseProgressEnv(envContent)
}

// RenderGateStatusBox renders a full-height terminal box displaying gate state.
// The progress bar icons are produced by gate.RenderBar, identical to commit-gate.
// The STATUS line and icon match the /gate JSON response fields exactly.
//
// Exported so it can be unit-tested without file I/O.
func RenderGateStatusBox(r gate.Result, repoPath string) string {
	const width = 64

	statusLabel, statusIcon, statusColor := gateStatusDisplay(r.Status)
	bar := r.ProgressBar
	if bar == "" {
		bar = gate.RenderBar(r.CopilotStatus, r.LiteLLMStatus, r.CurrentGate)
	}

	currentGate := r.CurrentGate
	if currentGate == "" {
		currentGate = "none"
	}

	var sb strings.Builder

	// ── header ──────────────────────────────────────────────────────────────
	header := fmt.Sprintf(" CODERO PRE-COMMIT GATE   STATUS: %s %s%s\033[0m",
		statusColor, statusLabel, statusIcon)
	sb.WriteString(boxTop(width))
	sb.WriteString(boxRow(header, width))
	sb.WriteString(boxMid(width))

	// ── gate progress bar (identical to commit-gate CLI output) ─────────────
	sb.WriteString(boxRow(fmt.Sprintf(" Gate:    %s", bar), width))
	sb.WriteString(boxRow(fmt.Sprintf(" Current: %-12s │ Run ID: %s",
		currentGate, truncate(r.RunID, 28)), width))

	// ── blocker comments ─────────────────────────────────────────────────────
	if len(r.Comments) > 0 {
		sb.WriteString(boxMid(width))
		sb.WriteString(boxRow(" Blockers:", width))
		for _, c := range r.Comments {
			sb.WriteString(boxRow(fmt.Sprintf("   • %s", c), width))
		}
		sb.WriteString(boxMid(width))
		sb.WriteString(boxRow(" Next steps: fix blockers, then run: codero commit-gate", width))
	} else if r.Status == gate.StatusPass {
		sb.WriteString(boxMid(width))
		sb.WriteString(boxRow(" ✅ Gate passed — commit is allowed.", width))
	} else if r.Status == gate.StatusPending {
		sb.WriteString(boxMid(width))
		sb.WriteString(boxRow(" Gate run in progress — polling…", width))
	}

	// ── progress.env path hint ───────────────────────────────────────────────
	if repoPath != "" {
		envPath := filepath.Join(repoPath, ".codero", "gate-heartbeat", "progress.env")
		sb.WriteString(boxMid(width))
		sb.WriteString(boxRow(fmt.Sprintf(" State file: %s", truncate(envPath, width-15)), width))
	}

	sb.WriteString(boxBot(width))
	return sb.String()
}

// printGateActions prints the available intervention actions after the TUI box.
func printGateActions(r gate.Result, repoPath string) {
	fmt.Println()
	fmt.Println("  Actions:")
	fmt.Println("    r   retry gate     →  codero commit-gate")
	fmt.Println("    l   gate logs      →  codero gate-status --logs")
	fmt.Println("    b   branch view    →  codero branch")
	fmt.Println("    q   quit")
	fmt.Println()
	if r.Status == gate.StatusFail {
		fmt.Println("  Run: codero commit-gate   (after fixing blockers above)")
	}

	// Guard interactive prompt behind IsInteractiveTTY (centralised check).
	if !tui.IsInteractiveTTY() {
		return
	}

	fmt.Print("  Enter action (r/l/b/q, or Enter to skip): ")
	var input string
	_, _ = fmt.Scanln(&input)
	input = strings.TrimSpace(strings.ToLower(input))
	switch input {
	case "r":
		fmt.Println("\n  Launching: codero commit-gate …")
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		cmd := exec.Command(os.Args[0], "commit-gate", "--repo-path", repoPath) //nolint:gosec
		if repoPath != "" {
			cmd.Dir = repoPath
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	case "l":
		_ = printGateLogs(repoPath)
	case "b":
		fmt.Println("\n  Launching: codero branch …")
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		cmd := exec.Command(os.Args[0], "branch") //nolint:gosec
		if repoPath != "" {
			cmd.Dir = repoPath
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
}

// printGateLogs prints the gate log directory path and last 20 lines of the
// most recent log file, if available.
func printGateLogs(repoPath string) error {
	if repoPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		repoPath = wd
	}
	logDir := filepath.Join(repoPath, ".codero", "gate-heartbeat")
	fmt.Printf("Gate log directory: %s\n\n", logDir)

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("  (no gate runs recorded yet)")
			return nil
		}
		return fmt.Errorf("read log dir: %w", err)
	}

	var logFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			logFiles = append(logFiles, filepath.Join(logDir, e.Name()))
		}
	}

	if len(logFiles) == 0 {
		fmt.Println("  (no .log files found in gate directory)")
		return nil
	}

	// Show the last log file (alphabetically last = most recent by convention).
	lastLog := logFiles[len(logFiles)-1]
	fmt.Printf("Latest log: %s\n", lastLog)

	data, err := os.ReadFile(lastLog)
	if err != nil {
		return fmt.Errorf("read log file: %w", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	const maxLines = 20
	start := 0
	if len(lines) > maxLines {
		start = len(lines) - maxLines
		fmt.Printf("  (showing last %d of %d lines)\n", maxLines, len(lines))
	}
	fmt.Println()
	for _, l := range lines[start:] {
		fmt.Println("  " + l)
	}
	return nil
}

// gateStatusDisplay returns human label, icon, and ANSI color code for a status.
func gateStatusDisplay(s gate.Status) (label, icon, ansiColor string) {
	switch s {
	case gate.StatusPass:
		return "PASS", " ✅", "\033[32m" // green
	case gate.StatusFail:
		return "FAIL", " ⚠️ ", "\033[31m" // red
	default:
		return "PENDING", " 🔄", "\033[33m" // yellow
	}
}

// GateStateToPrecommitStatus maps a gate-level state string to the three-valued
// precommit review status used in the proving scorecard.
//
// Exported so it is accessible from tests and from the commit-gate auto-write path.
//
// Mapping rationale:
//   - "pass"                → "passed"  (gate completed with no findings)
//   - "blocked", "timeout"  → "failed"  (gate found blockers or exceeded budget)
//   - all others            → "error"   (infra_fail, pending, running, unknown)
func GateStateToPrecommitStatus(gateState string) string {
	switch gateState {
	case "pass":
		return "passed"
	case "blocked", "timeout":
		return "failed"
	default:
		return "error"
	}
}

// ── box drawing helpers ──────────────────────────────────────────────────────

func boxTop(w int) string {
	return "┌" + strings.Repeat("─", w-2) + "┐\n"
}
func boxBot(w int) string {
	return "└" + strings.Repeat("─", w-2) + "┘\n"
}
func boxMid(w int) string {
	return "├" + strings.Repeat("─", w-2) + "┤\n"
}

// boxRow pads content to fill the box width and adds side borders.
func boxRow(content string, w int) string {
	// Strip ANSI codes to compute visible length.
	visible := ansiStrip(content)
	pad := w - 2 - len([]rune(visible))
	if pad < 0 {
		pad = 0
	}
	return "│" + content + strings.Repeat(" ", pad) + "│\n"
}

// ansiStrip removes ANSI escape sequences for length calculation.
func ansiStrip(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\033' {
			inEsc = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// truncate shortens a string to max characters, adding "…" if truncated.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max < 4 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
