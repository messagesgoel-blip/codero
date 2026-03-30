package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func agentNextCmd(_ *string) *cobra.Command {
	var (
		jsonOutput bool
		printID    bool
		printURL   bool
	)

	cmd := &cobra.Command{
		Use:   "next",
		Short: "Print the highest-priority session needing attention",
		Long: `Returns the session most likely to need operator attention.
Priority: waiting_for_input (oldest first) > working (most recent first).
Idle and unknown sessions are excluded.
Exit code 1 if no sessions need attention.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			sessions, err := store.ListActiveSessions(cmd.Context())
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}

			prioritized := listSessionsByPriority(sessions)
			if len(prioritized) == 0 {
				fmt.Fprintln(os.Stderr, "no sessions need attention")
				cmd.SilenceErrors = true
				return fmt.Errorf("exit 1")
			}

			top := prioritized[0]

			if printID {
				fmt.Println(top.SessionID)
				return nil
			}

			if printURL {
				baseURL := os.Getenv("CODERO_DASHBOARD_URL")
				if baseURL == "" {
					baseURL = "http://127.0.0.1:8110"
				}
				fmt.Printf("%s/#sessions?id=%s\n", baseURL, top.SessionID)
				return nil
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]interface{}{
					"session_id":      top.SessionID,
					"agent_id":        top.AgentID,
					"inferred_status": top.InferredStatus,
					"started_at":      top.StartedAt,
					"last_seen_at":    top.LastSeenAt,
					"tmux_session":    top.TmuxSessionName,
				})
			}

			// Default: human-readable one-liner
			fmt.Printf("%s  %s  %s  %s\n",
				top.AgentID,
				top.InferredStatus,
				truncateID(top.SessionID, 12),
				formatElapsed(time.Since(top.StartedAt)),
			)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON object")
	cmd.Flags().BoolVar(&printID, "print-id", false, "print session ID only")
	cmd.Flags().BoolVar(&printURL, "print-url", false, "print dashboard deep link")

	return cmd
}
