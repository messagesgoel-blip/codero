package main

import (
	"fmt"
	"time"

	"github.com/codero/codero/internal/session"
	"github.com/spf13/cobra"
)

// sessionEndCmd implements `codero session end` — the clean-exit signal.
// Spec reference: Session Lifecycle v1 §2.13, SL-7.
func sessionEndCmd(configPath *string) *cobra.Command {
	var (
		sessionID string
		agentID   string
		result    string
	)

	cmd := &cobra.Command{
		Use:   "end",
		Short: "Signal clean session close",
		Long: `Signals a clean session close. The agent or launcher runs this on normal exit.
On unclean exit, heartbeat TTL handles cleanup automatically (SL-7).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			if sessionID == "" {
				sessionID = resolveSessionIDFromEnv()
			}
			if sessionID == "" {
				return fmt.Errorf("session-id is required (set --session-id or CODERO_SESSION_ID)")
			}
			if agentID == "" {
				agentID = resolveAgentIDFromEnv()
			}
			if agentID == "" {
				return fmt.Errorf("agent-id is required (set --agent-id or CODERO_AGENT_ID)")
			}
			if result == "" {
				result = "ended"
			}

			if err := store.Finalize(cmd.Context(), sessionID, agentID, session.Completion{
				Status:     result,
				Substatus:  "terminal_finished",
				Summary:    "clean session close via codero session end",
				FinishedAt: time.Now().UTC(),
			}); err != nil {
				return fmt.Errorf("session end: %w", err)
			}

			fmt.Printf("session %s ended (result=%s)\n", sessionID, result)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier (defaults to CODERO_SESSION_ID)")
	cmd.Flags().StringVar(&agentID, "agent-id", "", "agent identifier (defaults to CODERO_AGENT_ID)")
	cmd.Flags().StringVar(&result, "result", "ended", "terminal result (ended, lost, cancelled)")

	return cmd
}
