package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/state"
)

func agentListCmd(_ *string) *cobra.Command {
	var (
		statusFilter string
		jsonOutput   bool
		quiet        bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active agent sessions",
		Long:  "Lists active agent sessions with inferred status, repo, branch, and timing.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if statusFilter != "" {
				normalized := state.NormalizeStatus(statusFilter)
				if normalized == "" {
					return fmt.Errorf("invalid --status value %q", statusFilter)
				}
				statusFilter = normalized
			}

			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			sessions, err := store.ListActiveSessions(cmd.Context())
			if err != nil {
				return fmt.Errorf("list sessions: %w", err)
			}

			// Filter by status if requested.
			if statusFilter != "" {
				var filtered []state.AgentSession
				for _, s := range sessions {
					if s.InferredStatus == statusFilter {
						filtered = append(filtered, s)
					}
				}
				sessions = filtered
			}

			if quiet {
				for _, s := range sessions {
					fmt.Println(s.SessionID)
				}
				return nil
			}

			// Discover agents from shims for installed status
			uc, ucErr := config.LoadUserConfig()
			if ucErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not load user config: %v\n", ucErr)
			}
			discovered, discErr := config.DiscoverAgents(uc)
			if discErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not discover agents: %v\n", discErr)
			}
			discoveredMap := make(map[string]config.AgentInfo)
			for _, d := range discovered {
				discoveredMap[d.AgentID] = d
			}

			if jsonOutput {
				type sessionJSON struct {
					SessionID      string `json:"session_id"`
					AgentID        string `json:"agent_id"`
					InferredStatus string `json:"inferred_status"`
					ElapsedSec     int64  `json:"elapsed_sec"`
					LastSeenAt     string `json:"last_seen_at"`
					TmuxSession    string `json:"tmux_session,omitempty"`
					Installed      bool   `json:"installed"`
				}
				var out []sessionJSON
				for _, s := range sessions {
					elapsed := int64(time.Since(s.StartedAt).Seconds())
					installed := false
					if info, ok := discoveredMap[s.AgentID]; ok {
						installed = info.Installed
					}
					out = append(out, sessionJSON{
						SessionID:      s.SessionID,
						AgentID:        s.AgentID,
						InferredStatus: s.InferredStatus,
						ElapsedSec:     elapsed,
						LastSeenAt:     s.LastSeenAt.Format(time.RFC3339),
						TmuxSession:    s.TmuxSessionName,
						Installed:      installed,
					})
				}
				if out == nil {
					out = []sessionJSON{}
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			// Plain text table.
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "AGENT\tSTATUS\tINSTALLED\tELAPSED\tLAST SEEN\tSESSION ID")
			for _, s := range sessions {
				elapsed := formatElapsed(time.Since(s.StartedAt))
				lastSeen := formatElapsed(time.Since(s.LastSeenAt))
				installed := "no"
				if info, ok := discoveredMap[s.AgentID]; ok && info.Installed {
					installed = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s ago\t%s\n",
					s.AgentID,
					s.InferredStatus,
					installed,
					elapsed,
					lastSeen,
					truncateID(s.SessionID, 12),
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&statusFilter, "status", "", "filter by inferred status: working, waiting_for_input, idle")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON array")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "output session IDs only")

	return cmd
}

func formatElapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func truncateID(id string, n int) string {
	if len(id) <= n {
		return id
	}
	return id[:n]
}

// listSessionsByPriority returns sessions sorted by attention priority:
// waiting_for_input first (oldest), working (most recent), idle excluded.
func listSessionsByPriority(sessions []state.AgentSession) []state.AgentSession {
	var actionable []state.AgentSession
	for _, s := range sessions {
		if state.IsActionableStatus(s.InferredStatus) {
			actionable = append(actionable, s)
		}
	}

	statusOrder := map[string]int{
		state.InferredStatusWaitingForInput: 0,
		state.InferredStatusWorking:         1,
	}

	// Sort: waiting first (oldest), working (most recent)
	for i := 0; i < len(actionable); i++ {
		for j := i + 1; j < len(actionable); j++ {
			oi := statusOrder[actionable[i].InferredStatus]
			oj := statusOrder[actionable[j].InferredStatus]
			swap := false
			if oi > oj {
				swap = true
			} else if oi == oj && oi == 0 {
				// waiting: oldest first
				swap = actionable[i].StartedAt.After(actionable[j].StartedAt)
			} else if oi == oj {
				// working: most recent first
				swap = actionable[i].StartedAt.Before(actionable[j].StartedAt)
			}
			if swap {
				actionable[i], actionable[j] = actionable[j], actionable[i]
			}
		}
	}
	return actionable
}
