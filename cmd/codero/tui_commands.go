package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/codero/codero/internal/state"
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

			validProviders := map[string]bool{"litellm": true, "coderabbit": true}
			validStatuses := map[string]bool{"passed": true, "failed": true, "error": true}
			if !validProviders[provider] {
				return fmt.Errorf("invalid provider: %s (valid: litellm, coderabbit)", provider)
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
	cmd.Flags().StringVar(&provider, "provider", "", "review provider: litellm or coderabbit (required)")
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
