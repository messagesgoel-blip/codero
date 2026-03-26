package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/tmux"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	agentLaunchNow  = time.Now
	agentLaunchUUID = func() string { return uuid.New().String() }
)

// agentCmd is the parent command for agent management.
func agentCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agent lifecycle",
	}
	cmd.AddCommand(agentLaunchCmd(configPath))
	return cmd
}

// AgentLaunchConfig holds all parameters for the 14-step wrapper sequence.
// Spec reference: Session Lifecycle v1 §8.2, SL-13.
type AgentLaunchConfig struct {
	AgentID      string
	RepoPath     string
	Branch       string
	Mode         string
	AgentCommand []string
	WriteLog     bool
	TmuxExecutor tmux.Executor
}

// agentLaunchCmd implements `codero agent launch` — the Go wrapper that owns
// the complete session lifecycle inside a tmux session.
// Spec reference: Session Lifecycle v1 §8.2, SL-9, SL-11, SL-12, SL-13.
func agentLaunchCmd(configPath *string) *cobra.Command {
	var (
		agentID  string
		repoPath string
		branch   string
		mode     string
		writeLog bool
	)

	cmd := &cobra.Command{
		Use:   "launch [-- agent_command...]",
		Short: "Launch an agent session inside a tmux session",
		Long: `Implements the 14-step wrapper sequence (Session Lifecycle v1 §8.2).
The tmux session IS the Codero session (SL-9). Its existence proves the agent is alive.
The wrapper handles all Codero integration — the agent never calls session APIs (SL-12).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentID == "" {
				agentID = resolveAgentIDFromEnv()
			}
			if agentID == "" {
				return fmt.Errorf("--agent is required (or set CODERO_AGENT_ID)")
			}
			if repoPath == "" {
				repoPath, _ = os.Getwd()
			}

			cfg := &AgentLaunchConfig{
				AgentID:      agentID,
				RepoPath:     repoPath,
				Branch:       branch,
				Mode:         mode,
				AgentCommand: args,
				WriteLog:     writeLog,
				TmuxExecutor: tmux.RealExecutor{},
			}

			// Override from env if not set via flag
			if !writeLog {
				if os.Getenv("CODERO_AGENT_WRITE_SESSION_LOG") == "true" || os.Getenv("CODERO_AGENT_WRITE_SESSION_LOG") == "1" {
					cfg.WriteLog = true
				}
			}

			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			return runAgentLaunch(cmd.Context(), cfg, store)
		},
	}

	cmd.Flags().StringVar(&agentID, "agent", "", "agent identifier (required)")
	cmd.Flags().StringVar(&repoPath, "repo", "", "repository path (defaults to cwd)")
	cmd.Flags().StringVar(&branch, "branch", "", "target branch (optional)")
	cmd.Flags().StringVar(&mode, "mode", "agent", "session mode label")
	cmd.Flags().BoolVar(&writeLog, "write-log", false, "capture session log (CODERO_AGENT_WRITE_SESSION_LOG)")

	return cmd
}

// runAgentLaunch executes the 14-step wrapper sequence.
// Each step maps directly to Session Lifecycle v1 §8.1/§8.2.
func runAgentLaunch(ctx context.Context, cfg *AgentLaunchConfig, store *session.Store) error {
	exec := cfg.TmuxExecutor
	if exec == nil {
		exec = tmux.RealExecutor{}
	}

	// Step 1: Parse arguments (already done by cobra)
	// Step 2: Generate session UUID
	sessionID := agentLaunchUUID()

	// Step 3: Compute tmux session name (SL-11)
	tmuxName := tmux.SessionName(cfg.AgentID, sessionID)

	// Step 4: Resolve worktree
	worktreePath := cfg.RepoPath
	if cfg.Branch != "" && cfg.Branch != "main" && cfg.Branch != "master" {
		worktreePath = resolveWorktree(cfg.RepoPath, cfg.Branch)
	}

	// Step 5: Create tmux session (SL-9)
	if err := exec.NewSession(ctx, tmuxName, worktreePath); err != nil {
		return fmt.Errorf("step 5: create tmux session: %w", err)
	}

	// Step 6: Register session with daemon (SL-12)
	if _, err := store.RegisterWithTmux(ctx, sessionID, cfg.AgentID, cfg.Mode, tmuxName); err != nil {
		// Cleanup tmux on registration failure
		_ = exec.KillSession(ctx, tmuxName)
		return fmt.Errorf("step 6: register session: %w", err)
	}

	// Step 7: Write SESSION.md to worktree
	coderoDir := filepath.Join(worktreePath, ".codero")
	_ = os.MkdirAll(coderoDir, 0o755)
	startedAt := agentLaunchNow().UTC()
	sessionMD := fmt.Sprintf(`# Codero Session
- CODERO_SESSION_ID=%s
- CODERO_AGENT_ID=%s
- CODERO_TMUX_NAME=%s
- CODERO_STARTED_AT=%s
`, sessionID, cfg.AgentID, tmuxName, startedAt.Format(time.RFC3339))
	_ = os.WriteFile(filepath.Join(coderoDir, "SESSION.md"), []byte(sessionMD), 0o644)

	// Step 8: Write AGENT.md to worktree
	agentMD := fmt.Sprintf(`# Codero Agent Instructions
Agent: %s | Session: %s
Mode: codero-driven — do NOT run git commands directly.
Use 'codero submit' to deliver your work.
`, cfg.AgentID, sessionID)
	_ = os.WriteFile(filepath.Join(coderoDir, "AGENT.md"), []byte(agentMD), 0o644)

	// Step 9: Configure notification hook
	hooksDir := filepath.Join(coderoDir, "hooks")
	_ = os.MkdirAll(hooksDir, 0o755)
	hookScript := fmt.Sprintf(`#!/bin/bash
WORKTREE="$1"
TYPE="$2"
TMUX_NAME="%s"
tmux display-message -t "$TMUX_NAME" "Codero: $TYPE update available" 2>/dev/null || true
touch "$WORKTREE/.codero/feedback/pending" 2>/dev/null || true
`, tmuxName)
	_ = os.WriteFile(filepath.Join(hooksDir, "on-feedback"), []byte(hookScript), 0o755)

	// Step 10: Launch agent inside tmux
	agentCmd := strings.Join(cfg.AgentCommand, " ")
	if agentCmd == "" {
		agentCmd = "bash" // default shell if no command given
	}
	if err := exec.SendKeys(ctx, tmuxName, agentCmd); err != nil {
		// Non-fatal; the session is alive, agent command just failed to send
		fmt.Fprintf(os.Stderr, "warning: failed to send agent command: %v\n", err)
	}

	// Step 11: Wait for agent exit (monitor tmux session)
	exitCode := waitForTmuxExit(ctx, exec, tmuxName)

	// Step 12: On exit — report to Codero (SL-14: unclean exit reporting)
	result := "ended"
	if exitCode != 0 {
		result = "lost"
	}
	endErr := store.Finalize(ctx, sessionID, cfg.AgentID, session.Completion{
		Status:     result,
		Substatus:  "terminal_finished",
		Summary:    fmt.Sprintf("wrapper exit (code=%d)", exitCode),
		FinishedAt: agentLaunchNow().UTC(),
	})
	if endErr != nil {
		fmt.Fprintf(os.Stderr, "warning: session end failed: %v\n", endErr)
	}

	// Step 13: Archive — optional session log capture (SL-15)
	if cfg.WriteLog {
		logContent, err := exec.CapturePane(ctx, tmuxName)
		if err == nil && logContent != "" {
			logPath := filepath.Join(coderoDir, "session-log.txt")
			_ = os.WriteFile(logPath, []byte(logContent), 0o644)
		}
	}

	// Step 14: Cleanup — kill tmux session
	_ = exec.KillSession(ctx, tmuxName)

	fmt.Printf("session %s finished (result=%s, exit=%d)\n", sessionID, result, exitCode)
	return nil
}

// waitForTmuxExit polls tmux session existence until it disappears.
// Returns 0 for clean exit (session gone), non-zero for context cancellation.
func waitForTmuxExit(ctx context.Context, exec tmux.Executor, name string) int {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 1
		case <-ticker.C:
			if !exec.HasSession(ctx, name) {
				return 0
			}
		}
	}
}

// resolveWorktree returns the worktree path for a branch, or falls back to repoPath.
func resolveWorktree(repoPath, branch string) string {
	// Check for existing worktree
	cmd := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return repoPath
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			wtPath := strings.TrimPrefix(line, "worktree ")
			if strings.Contains(wtPath, branch) {
				return wtPath
			}
		}
	}
	return repoPath
}
