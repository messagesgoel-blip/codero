package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/codero/codero/internal/state"
	"github.com/spf13/cobra"
)

// submitCmd returns the top-level "codero submit" command.
// It sends a submit signal to the daemon API, which triggers the delivery pipeline.
func submitCmd(configPath *string) *cobra.Command {
	var (
		summary string
		files   string
	)

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit current assignment to the Codero delivery pipeline",
		Long: `Submit signals the daemon that the agent's current work is ready for review.

The daemon handles: push → CI → CodeRabbit review → feedback assembly.
Unlike "codero task submit" (which writes directly to the DB), this command
calls the daemon HTTP API so the full delivery pipeline is triggered.

Requires CODERO_SESSION_ID or CODERO_AGENT_SESSION_ID to be set.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := resolveSessionIDFromEnv()
			if sessionID == "" {
				return usageErrorf("CODERO_SESSION_ID or CODERO_AGENT_SESSION_ID must be set")
			}

			cfg, err := loadConfig(*configPathForCmd(cmd))
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			// Look up active assignment for this session.
			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state db: %w", err)
			}
			defer func() { _ = db.Close() }()

			var assignmentID string
			err = db.Unwrap().QueryRowContext(cmd.Context(),
				`SELECT assignment_id FROM agent_assignments WHERE session_id = ? AND ended_at IS NULL LIMIT 1`,
				sessionID,
			).Scan(&assignmentID)
			if err == sql.ErrNoRows {
				return fmt.Errorf("no active assignment for session %s", sessionID)
			}
			if err != nil {
				return fmt.Errorf("query active assignment: %w", err)
			}

			// Resolve daemon address.
			addr := cfg.APIServer.Addr
			if strings.HasPrefix(addr, ":") {
				addr = "127.0.0.1" + addr
			}
			url := fmt.Sprintf("http://%s/api/v1/assignments/%s/submit", addr, assignmentID)

			worktree, _ := os.Getwd()
			body, err := json.Marshal(map[string]string{
				"session_id": sessionID,
				"summary":    summary,
				"files":      files,
				"worktree":   worktree,
			})
			if err != nil {
				return fmt.Errorf("marshal request body: %w", err)
			}

			resp, err := http.Post(url, "application/json", bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("daemon request failed: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			respBody, _ := io.ReadAll(resp.Body)

			switch resp.StatusCode {
			case http.StatusAccepted:
				fmt.Fprintln(cmd.OutOrStdout(), "Submitted.")
			case http.StatusNotFound:
				return fmt.Errorf("assignment not found")
			case http.StatusForbidden:
				return fmt.Errorf("session not authorized")
			case http.StatusConflict:
				return fmt.Errorf("%s", strings.TrimSpace(string(respBody)))
			default:
				return fmt.Errorf("daemon returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&summary, "summary", "", "submission summary message")
	cmd.Flags().StringVar(&files, "files", "", "comma-separated list of files to include")

	return cmd
}
