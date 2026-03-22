package main

import (
	"errors"
	"fmt"
	"strconv"

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
	cmd.AddCommand(taskEmitCmd(configPath))
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

// taskEmitCmd returns the "task emit" subcommand.
//
// Usage:
//
//	codero task emit --assignment <id> --version <n> --substatus <substatus>
//
// Exit behaviour:
//   - 0: emit applied, assignment updated.
//   - 1: version conflict (stale version).
//   - 2: assignment already ended.
//   - other: unexpected error.
func taskEmitCmd(configPath *string) *cobra.Command {
	var (
		assignmentID string
		versionStr   string
		substatus    string
	)

	cmd := &cobra.Command{
		Use:   "emit",
		Short: "Emit a versioned state update to an assignment",
		Long: `Emit applies a state/substatus transition to an assignment, guarded by
optimistic concurrency on assignment_version.

The caller must provide the current assignment_version they believe the row
has. If the row's version matches, the update is applied and the version
is advanced to current+1. If the version is stale, the emit is rejected.

Terminal substatuses (terminal_*) also set ended_at on the assignment.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if assignmentID == "" {
				return usageErrorf("--assignment is required")
			}
			if versionStr == "" {
				return usageErrorf("--version is required")
			}
			if substatus == "" {
				return usageErrorf("--substatus is required")
			}

			version, err := strconv.Atoi(versionStr)
			if err != nil {
				return usageErrorf("--version must be an integer: %v", err)
			}
			if version < 1 {
				return usageErrorf("--version must be >= 1")
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

			a, err := state.EmitAssignmentUpdate(cmd.Context(), db, assignmentID, version, substatus)
			if err != nil {
				if errors.Is(err, state.ErrVersionConflict) {
					return fmt.Errorf("version conflict: %w", err)
				}
				if errors.Is(err, state.ErrAssignmentEnded) {
					return fmt.Errorf("assignment ended: %w", err)
				}
				if errors.Is(err, state.ErrInvalidEmitSubstatus) {
					return fmt.Errorf("invalid substatus: %w", err)
				}
				return err
			}

			fmt.Printf("assignment_id: %s\nstate: %s\nsubstatus: %s\nversion: %d\n",
				a.ID, a.State, a.Substatus, a.Version)
			return nil
		},
	}

	cmd.Flags().StringVar(&assignmentID, "assignment", "", "assignment identifier")
	cmd.Flags().StringVar(&versionStr, "version", "", "current assignment version (optimistic lock)")
	cmd.Flags().StringVar(&substatus, "substatus", "", "target substatus for the assignment")

	return cmd
}
