package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	ghclient "github.com/codero/codero/internal/github"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/webhook"
	"github.com/spf13/cobra"
)

// pollCmd triggers an on-demand reconciliation cycle for a single branch by
// fetching GitHub state and applying any corrections to the local state store.
// This is the operator escape-hatch when the 60-second polling interval is
// too slow (e.g. after manually merging a PR).
func pollCmd(configPath *string) *cobra.Command {
	var (
		repo   string
		branch string
	)

	cmd := &cobra.Command{
		Use:   "poll",
		Short: "Force an immediate GitHub reconciliation for a branch",
		Long: `Force an on-demand reconciliation cycle for a specific branch.

codero poll fetches the current GitHub state (PR open/closed, CI status,
approval status) and applies any corrections to the local state store —
the same work the background reconciler does every 60 seconds, but on demand.

Useful when:
  - A PR was just merged and you want the daemon to pick it up immediately
  - CI just turned green and you want to unblock a merge_ready transition
  - You suspect drift between GitHub and the local state store

Examples:
  codero poll --repo owner/repo --branch feat/my-feature
  codero poll -R owner/repo -b feat/my-feature`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}

			if repo == "" {
				if len(cfg.Repos) == 0 {
					return fmt.Errorf("--repo is required (or set repos in config)")
				}
				repo = cfg.Repos[0]
			}

			if branch == "" {
				branch, err = getCurrentBranch()
				if err != nil {
					return fmt.Errorf("--branch is required (could not detect current branch: %w)", err)
				}
			}

			if cfg.GitHubToken == "" {
				return fmt.Errorf("github_token is not configured; poll requires GitHub API access")
			}

			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			rec, err := state.GetBranch(db, repo, branch)
			if err != nil {
				if errors.Is(err, state.ErrBranchNotFound) {
					return fmt.Errorf("branch %s/%s not registered in local state store", repo, branch)
				}
				return fmt.Errorf("get branch: %w", err)
			}

			stateBefore := rec.State
			fmt.Printf("Polling GitHub state for %s/%s (current state: %s)...\n", repo, branch, stateBefore)

			gh := ghclient.NewClient(cfg.GitHubToken)
			ghState, err := gh.GetPRState(cmd.Context(), repo, branch)
			if err != nil {
				return fmt.Errorf("github GetPRState: %w", err)
			}

			if ghState == nil {
				fmt.Println("No open PR found on GitHub for this branch.")
				applyNoPR(db, rec)
				return nil
			}

			fmt.Printf("  PR open:            %v\n", ghState.PROpen)
			fmt.Printf("  Head SHA:           %s\n", ghState.HeadHash)
			fmt.Printf("  Approved:           %v\n", ghState.Approved)
			fmt.Printf("  CI green:           %v\n", ghState.CIGreen)
			fmt.Printf("  Unresolved threads: %d\n", ghState.UnresolvedThreads)
			fmt.Println()

			reconciler := webhook.NewReconciler(db, gh, []string{repo}, cfg.Webhook.Enabled)
			reconciler.RunOnce(cmd.Context())

			recAfter, err := state.GetBranch(db, repo, branch)
			if err != nil {
				return fmt.Errorf("get branch after reconcile: %w", err)
			}

			if recAfter.State != stateBefore {
				fmt.Printf("State transition: %s -> %s\n", stateBefore, recAfter.State)
			} else {
				fmt.Printf("No state change (still: %s)\n", recAfter.State)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "branch name (default: current git branch)")

	return cmd
}

// applyNoPR transitions the branch to merged if it is in a post-PR state,
// matching the reconciler's behaviour when a PR is not found.
func applyNoPR(db *state.DB, rec *state.BranchRecord) {
	if rec.State == state.StateSubmitted || rec.State == state.StateWaiting {
		fmt.Printf("Branch is in %s — no PR expected yet, nothing to do.\n", rec.State)
		return
	}
	if rec.State == state.StateMerged {
		fmt.Println("Branch is already merged.")
		return
	}
	if err := state.TransitionBranch(db, rec.ID, rec.State, state.StateMerged, "poll_pr_not_found"); err != nil {
		fmt.Printf("Warning: could not transition to merged: %v\n", err)
		return
	}
	fmt.Printf("Transitioned %s -> merged (no open PR found).\n", rec.State)
}

// whyCmd explains why a branch is in its current state by showing the branch
// record, recent findings, and the last N delivery events.
func whyCmd(configPath *string) *cobra.Command {
	var (
		repo    string
		branch  string
		limit   int
		jsonFmt bool
	)

	cmd := &cobra.Command{
		Use:   "why",
		Short: "Explain why a branch is in its current state",
		Long: `Show the current state of a branch and the events that led to it.

codero why fetches the branch record from the local state store, lists recent
findings from the last review run, and replays the last N delivery events so
you can see exactly what happened and why the branch is where it is.

Examples:
  codero why --repo owner/repo --branch feat/my-feature
  codero why -R owner/repo -b feat/my-feature --limit 20
  codero why -R owner/repo -b feat/my-feature --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}

			if repo == "" {
				if len(cfg.Repos) == 0 {
					return fmt.Errorf("--repo is required (or set repos in config)")
				}
				repo = cfg.Repos[0]
			}

			if branch == "" {
				branch, err = getCurrentBranch()
				if err != nil {
					return fmt.Errorf("--branch is required (could not detect current branch: %w)", err)
				}
			}

			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			rec, err := state.GetBranch(db, repo, branch)
			if err != nil {
				if errors.Is(err, state.ErrBranchNotFound) {
					return fmt.Errorf("branch %s/%s not registered in local state store", repo, branch)
				}
				return fmt.Errorf("get branch: %w", err)
			}

			findings, err := state.ListFindings(db, repo, branch)
			if err != nil {
				findings = nil // best effort
			}

			events, err := state.ListDeliveryEvents(db, repo, branch, 0)
			if err != nil {
				events = nil // best effort
			}
			// Trim to most recent N events.
			if limit > 0 && len(events) > limit {
				events = events[len(events)-limit:]
			}

			if jsonFmt {
				return printWhyJSON(rec, findings, events)
			}
			printWhyText(rec, findings, events)
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "branch name (default: current git branch)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "number of recent delivery events to show")
	cmd.Flags().BoolVar(&jsonFmt, "json", false, "output as JSON")

	return cmd
}

func printWhyText(rec *state.BranchRecord, findings []state.FindingRecord, events []state.DeliveryEvent) {
	fmt.Printf("Branch: %s/%s\n", rec.Repo, rec.Branch)
	fmt.Printf("State:  %s\n", rec.State)
	if rec.HeadHash != "" {
		fmt.Printf("HEAD:   %s\n", rec.HeadHash)
	}
	fmt.Printf("Updated: %s\n", rec.UpdatedAt.Format(time.RFC3339))
	fmt.Println()

	// Merge-readiness conditions.
	fmt.Println("Merge-readiness:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "  approved\t%v\n", rec.Approved)
	fmt.Fprintf(w, "  ci_green\t%v\n", rec.CIGreen)
	fmt.Fprintf(w, "  pending_events\t%d\n", rec.PendingEvents)
	fmt.Fprintf(w, "  unresolved_threads\t%d\n", rec.UnresolvedThreads)
	_ = w.Flush()
	fmt.Println()

	// Findings.
	if len(findings) == 0 {
		fmt.Println("Findings: none")
	} else {
		fmt.Printf("Findings (%d):\n", len(findings))
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  SEVERITY\tCATEGORY\tFILE\tLINE\tMESSAGE")
		for _, f := range findings {
			msg := f.Message
			if len(msg) > 80 {
				msg = msg[:77] + "..."
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%d\t%s\n", f.Severity, f.Category, f.File, f.Line, msg)
		}
		_ = w.Flush()
	}
	fmt.Println()

	// Delivery events.
	if len(events) == 0 {
		fmt.Println("Recent events: none")
	} else {
		fmt.Printf("Recent events (%d shown):\n", len(events))
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  SEQ\tTYPE\tTIME\tPAYLOAD")
		for _, ev := range events {
			payload := ev.Payload
			if len(payload) > 60 {
				payload = payload[:57] + "..."
			}
			fmt.Fprintf(w, "  %d\t%s\t%s\t%s\n", ev.Seq, ev.EventType,
				ev.CreatedAt.Format("15:04:05"), payload)
		}
		_ = w.Flush()
	}
}

type whyOutput struct {
	Branch            string       `json:"branch"`
	Repo              string       `json:"repo"`
	State             string       `json:"state"`
	HeadHash          string       `json:"head_hash"`
	UpdatedAt         string       `json:"updated_at"`
	Approved          bool         `json:"approved"`
	CIGreen           bool         `json:"ci_green"`
	PendingEvents     int          `json:"pending_events"`
	UnresolvedThreads int          `json:"unresolved_threads"`
	Findings          []findingOut `json:"findings"`
	Events            []eventOut   `json:"events"`
}

type findingOut struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

type eventOut struct {
	Seq       int64  `json:"seq"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"`
	CreatedAt string `json:"created_at"`
}

func printWhyJSON(rec *state.BranchRecord, findings []state.FindingRecord, events []state.DeliveryEvent) error {
	out := whyOutput{
		Branch:            rec.Branch,
		Repo:              rec.Repo,
		State:             string(rec.State),
		HeadHash:          rec.HeadHash,
		UpdatedAt:         rec.UpdatedAt.Format(time.RFC3339),
		Approved:          rec.Approved,
		CIGreen:           rec.CIGreen,
		PendingEvents:     rec.PendingEvents,
		UnresolvedThreads: rec.UnresolvedThreads,
	}
	for _, f := range findings {
		out.Findings = append(out.Findings, findingOut{
			Severity: f.Severity,
			Category: f.Category,
			File:     f.File,
			Line:     f.Line,
			Message:  f.Message,
			Source:   f.Source,
		})
	}
	for _, ev := range events {
		out.Events = append(out.Events, eventOut{
			Seq:       ev.Seq,
			EventType: ev.EventType,
			Payload:   ev.Payload,
			CreatedAt: ev.CreatedAt.Format(time.RFC3339),
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
