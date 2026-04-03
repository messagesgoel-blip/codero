package main

import (
	"fmt"

	"github.com/codero/codero/internal/state"
	"github.com/spf13/cobra"
)

// prCmd is the parent "codero pr" command group.
func prCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Pull request tracking commands",
	}

	cmd.AddCommand(prTrackCmd(configPath))
	return cmd
}

// prTrackCmd registers a PR number against a repo/branch in the state store.
//
//	codero pr track --repo=codero --branch=feat/foo --pr=123
func prTrackCmd(configPath *string) *cobra.Command {
	var (
		repo     string
		branch   string
		prNumber int
	)

	cmd := &cobra.Command{
		Use:   "track",
		Short: "Track a PR created outside Codero",
		Long: `Associates a GitHub PR number with a repo/branch pair in the Codero state store.

Idempotent: calling twice with the same PR produces one row.
Updatable: calling with a different PR number updates the existing row.

Example:
  codero pr track --repo=codero --branch=feat/new-feature --pr=42`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if repo == "" {
				return usageErrorf("--repo is required")
			}
			if branch == "" {
				return usageErrorf("--branch is required")
			}
			if prNumber <= 0 {
				return usageErrorf("--pr must be a positive integer")
			}

			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer func() { _ = db.Close() }()

			if err := state.UpsertPRTracking(cmd.Context(), db, repo, branch, prNumber); err != nil {
				return fmt.Errorf("pr track: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Tracked PR #%d for %s/%s\n", prNumber, repo, branch)
			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "repository name (e.g. codero)")
	cmd.Flags().StringVar(&branch, "branch", "", "branch name (e.g. feat/foo)")
	cmd.Flags().IntVar(&prNumber, "pr", 0, "GitHub PR number")

	return cmd
}
