package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

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
