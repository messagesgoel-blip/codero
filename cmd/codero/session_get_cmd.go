package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	daemongrpc "github.com/codero/codero/internal/daemon/grpc"
	"github.com/codero/codero/internal/session"
	"github.com/spf13/cobra"
)

func sessionGetCmd(configPath *string) *cobra.Command {
	var (
		sessionID string
		agentID   string
		jsonOut   bool
	)

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get session state (read-only)",
		Long: `Get retrieves the current session state including active assignment if any.
This is a read-only operation that does not modify any state.
When a daemon address is configured, the command routes through daemon gRPC.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				sessionID = resolveSessionIDFromEnv()
			}
			if sessionID == "" {
				return fmt.Errorf("--session is required (or set CODERO_SESSION_ID)")
			}
			if agentID == "" {
				agentID = resolveAgentIDFromEnv()
			}

			ctx := cmd.Context()
			info, err := loadSessionInfo(ctx, cmd, sessionID, agentID)
			if err != nil {
				return fmt.Errorf("session get: %w", err)
			}

			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(info); err != nil {
					return fmt.Errorf("encode JSON: %w", err)
				}
				return nil
			}

			// Human-readable output
			s := &info.Session
			fmt.Printf("Session ID:    %s\n", s.SessionID)
			fmt.Printf("Agent ID:      %s\n", s.AgentID)
			fmt.Printf("Mode:          %s\n", s.Mode)
			if !s.StartedAt.IsZero() {
				fmt.Printf("Started At:    %s\n", s.StartedAt.Format("2006-01-02 15:04:05 MST"))
			}
			if !s.LastSeenAt.IsZero() {
				fmt.Printf("Last Seen At:  %s\n", s.LastSeenAt.Format("2006-01-02 15:04:05 MST"))
			}
			if s.LastProgressAt != nil {
				fmt.Printf("Last Progress: %s\n", s.LastProgressAt.Format("2006-01-02 15:04:05 MST"))
			}
			fmt.Printf("Status:        %s\n", sessionDisplayStatus(info))
			if s.TmuxSessionName != "" {
				fmt.Printf("Tmux Session:  %s\n", s.TmuxSessionName)
			}
			if s.EndedAt != nil {
				fmt.Printf("Ended At:      %s\n", s.EndedAt.Format("2006-01-02 15:04:05 MST"))
				fmt.Printf("End Reason:    %s\n", s.EndReason)
			}

			if info.Assignment != nil {
				a := info.Assignment
				fmt.Printf("\nActive Assignment:\n")
				fmt.Printf("  Assignment ID: %s\n", a.ID)
				fmt.Printf("  Repo:          %s\n", a.Repo)
				fmt.Printf("  Branch:        %s\n", a.Branch)
				if a.Worktree != "" {
					fmt.Printf("  Worktree:      %s\n", a.Worktree)
				}
				if a.TaskID != "" {
					fmt.Printf("  Task ID:       %s\n", a.TaskID)
				}
				fmt.Printf("  State:         %s\n", a.State)
				if a.Substatus != "" {
					fmt.Printf("  Substatus:     %s\n", a.Substatus)
				}
				if !a.StartedAt.IsZero() {
					fmt.Printf("  Started At:    %s\n", a.StartedAt.Format("2006-01-02 15:04:05 MST"))
				}
			} else {
				fmt.Printf("\nNo active assignment\n")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session ID (defaults to $CODERO_SESSION_ID)")
	cmd.Flags().StringVar(&agentID, "agent-id", "", "agent ID for verification (defaults to $CODERO_AGENT_ID)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output as JSON")

	return cmd
}

func loadSessionInfo(ctx context.Context, cmd *cobra.Command, sessionID, agentID string) (*session.SessionInfo, error) {
	if daemonAddr := resolveDaemonAddr(cmd); daemonAddr != "" {
		client, err := daemongrpc.NewSessionClient(daemonAddr)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return client.Get(ctx, sessionID, agentID)
	}

	store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
	if err != nil {
		return nil, err
	}
	defer cleanup()

	return store.Get(ctx, sessionID, agentID)
}

func sessionDisplayStatus(info *session.SessionInfo) string {
	if info == nil {
		return ""
	}
	if info.Session.InferredStatus != "" {
		return info.Session.InferredStatus
	}
	if info.Session.EndReason != "" {
		return info.Session.EndReason
	}
	if info.Session.EndedAt != nil {
		return "ended"
	}
	return "active"
}
