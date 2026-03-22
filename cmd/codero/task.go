package main

import (
	"errors"
	"fmt"

	"github.com/codero/codero/internal/state"
	"github.com/spf13/cobra"
)

// taskCmd returns the "task" subcommand group.
func taskCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage task assignments",
	}
	cmd.AddCommand(taskAcceptCmd(configPath))
	return cmd
}

// taskAcceptCmd returns the "task accept" subcommand.
//
// Usage:
//
//	codero task accept --session <session_id> --task <task_id>
//
// Exit behaviour:
//   - 0: assignment accepted (new or idempotent same-session).
//   - 1: conflict — task already claimed by another live session.
//   - other: unexpected error.
func taskAcceptCmd(configPath *string) *cobra.Command {
	var (
		sessionID string
		taskID    string
	)

	cmd := &cobra.Command{
		Use:   "accept",
		Short: "Atomically claim a task for a session",
		Long: `Accept claims task_id for session_id in a single DB transaction.

Repeated calls from the same session are idempotent and return success.
A live claim held by a different session returns a conflict error (exit 1).
Terminal (ended) assignments do not block a new claim.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				sessionID = resolveSessionIDFromEnv()
			}
			if sessionID == "" {
				return usageErrorf("--session is required (or set CODERO_SESSION_ID)")
			}
			if taskID == "" {
				return usageErrorf("--task is required")
			}

			cfg, err := loadConfig(*configPathForCmd(cmd))
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state db: %w", err)
			}
			defer func() { _ = db.Close() }()

			a, err := state.AcceptTask(cmd.Context(), db, sessionID, taskID)
			if err != nil {
				if errors.Is(err, state.ErrTaskAlreadyClaimed) {
					return fmt.Errorf("conflict: %w", err)
				}
				return err
			}

			fmt.Printf("assignment_id: %s\nsession_id: %s\ntask_id: %s\nstate: %s\nsubstatus: %s\n",
				a.ID, a.SessionID, a.TaskID, a.State, a.Substatus)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session identifier (defaults to CODERO_SESSION_ID)")
	cmd.Flags().StringVar(&taskID, "task", "", "task identifier to claim")

	return cmd
}
