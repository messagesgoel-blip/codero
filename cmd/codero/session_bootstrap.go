package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codero/codero/internal/session"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type sessionBootstrapConfig struct {
	SessionID      string
	AgentID        string
	Mode           string
	Worktree       string
	RuntimeRoot    string
	BaseURL        string
	TailnetBaseURL string
	CLIPath        string
	ConfigPath     string
	Repo           string
	Branch         string
	TaskID         string
}

type sessionBootstrapResult struct {
	RuntimeDir       string
	RuntimeAgentMD   string
	RuntimeSessionMD string
	Exports          map[string]string
}

func sessionBootstrapCmd(configPath *string) *cobra.Command {
	cfg := &sessionBootstrapConfig{}

	cmd := &cobra.Command{
		Use:   "bootstrap-env",
		Short: "Register a session, write runtime notes, and print shell exports",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			cfg.ConfigPath = *configPathForCmd(cmd)
			resolved, err := cfg.resolve()
			if err != nil {
				return err
			}

			if err := store.Register(cmd.Context(), resolved.SessionID, resolved.AgentID, resolved.Mode); err != nil {
				return err
			}

			result, err := writeSessionBootstrap(resolved)
			if err != nil {
				return err
			}

			fmt.Print(renderBootstrapExports(result.Exports))
			return nil
		},
	}

	cmd.Flags().StringVar(&cfg.SessionID, "session-id", "", "session identifier (defaults to CODERO_SESSION_ID or a fresh UUID)")
	cmd.Flags().StringVar(&cfg.AgentID, "agent-id", "", "agent identifier (defaults to CODERO_AGENT_ID)")
	cmd.Flags().StringVar(&cfg.Mode, "mode", "", "session mode label (default: agent)")
	cmd.Flags().StringVar(&cfg.Worktree, "worktree", "", "worktree path (defaults to CODERO_WORKTREE or cwd)")
	cmd.Flags().StringVar(&cfg.RuntimeRoot, "runtime-root", "", "root directory for runtime AGENT.md and SESSION.md notes")
	cmd.Flags().StringVar(&cfg.BaseURL, "base-url", "", "Codero base URL for runtime notes (defaults to CODERO_BASE_URL)")
	cmd.Flags().StringVar(&cfg.TailnetBaseURL, "tailnet-base-url", "", "Codero tailnet base URL for runtime notes (defaults to CODERO_TAILNET_BASE_URL)")
	cmd.Flags().StringVar(&cfg.Repo, "repo", "", "repository (owner/repo) for runtime notes (defaults to TEST_REPO)")
	cmd.Flags().StringVar(&cfg.Branch, "branch", "", "branch name for runtime notes (defaults to TEST_BRANCH)")
	cmd.Flags().StringVar(&cfg.TaskID, "task-id", "", "task identifier for runtime notes (defaults to TEST_TASK_ID)")

	return cmd
}

func (c *sessionBootstrapConfig) resolve() (*sessionBootstrapConfig, error) {
	out := *c

	if out.SessionID == "" {
		out.SessionID = resolveSessionIDFromEnv()
	}
	if out.SessionID == "" {
		out.SessionID = uuid.New().String()
	}
	if out.AgentID == "" {
		out.AgentID = resolveAgentIDFromEnv()
	}
	if out.AgentID == "" {
		return nil, session.ErrMissingAgentID
	}
	if out.Mode == "" {
		out.Mode = resolveSessionModeFromEnv("agent")
	}
	if out.Worktree == "" {
		out.Worktree = resolveWorktreeFromEnv()
	}
	if out.Worktree == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve bootstrap worktree: %w", err)
		}
		out.Worktree = cwd
	}
	if out.RuntimeRoot == "" {
		if v := os.Getenv("CODERO_RUNTIME_ROOT"); v != "" {
			out.RuntimeRoot = v
		} else {
			out.RuntimeRoot = filepath.Join(os.TempDir(), "codero-runtime")
		}
	}
	if out.BaseURL == "" {
		out.BaseURL = os.Getenv("CODERO_BASE_URL")
	}
	if out.TailnetBaseURL == "" {
		out.TailnetBaseURL = os.Getenv("CODERO_TAILNET_BASE_URL")
	}
	if out.CLIPath == "" {
		out.CLIPath = os.Getenv("CODERO_PILOT_CLI")
	}
	if out.CLIPath == "" {
		exe, err := os.Executable()
		if err == nil {
			out.CLIPath = exe
		}
	}
	if out.CLIPath == "" {
		return nil, fmt.Errorf("resolve bootstrap CLI path: CODERO_PILOT_CLI could not be resolved; mandatory session confirm command would be unusable")
	}
	if out.Repo == "" {
		out.Repo = os.Getenv("TEST_REPO")
	}
	if out.Branch == "" {
		out.Branch = os.Getenv("TEST_BRANCH")
	}
	if out.TaskID == "" {
		out.TaskID = os.Getenv("TEST_TASK_ID")
	}

	return &out, nil
}

func writeSessionBootstrap(cfg *sessionBootstrapConfig) (*sessionBootstrapResult, error) {
	runtimeDir := filepath.Join(cfg.RuntimeRoot, cfg.SessionID)
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return nil, fmt.Errorf("bootstrap session runtime dir: %w", err)
	}
	if cfg.Worktree != "" {
		if err := os.MkdirAll(cfg.Worktree, 0o755); err != nil {
			return nil, fmt.Errorf("bootstrap session worktree dir: %w", err)
		}
	}

	agentPath := filepath.Join(runtimeDir, "AGENT.md")
	sessionPath := filepath.Join(runtimeDir, "SESSION.md")

	agentBody := fmt.Sprintf(`# Runtime Agent Note

- Agent alias: %q
- Session id: %q
- Session mode: %q
- Codero base URL: %q

Use the existing CODERO_AGENT_ID and CODERO_SESSION_ID environment values for
all future Codero actions in this window.

This session is already claimed and registered for this window before startup.

First confirm that Codero sees the same session:
- "$CODERO_PILOT_CLI" --config "$CODERO_PILOT_CONFIG" session confirm --session-id "$CODERO_SESSION_ID" --agent-id "$CODERO_AGENT_ID"

When you claim or are assigned work, resend:
- CODERO_AGENT_ID
- CODERO_SESSION_ID
- repo / branch / worktree / task_id when available

Do not invent a new session id for this window.
Do not reuse this session id in a future window after this session ends.
`, cfg.AgentID, cfg.SessionID, cfg.Mode, cfg.BaseURL)

	sessionBody := fmt.Sprintf(`# Runtime Session Note

- CODERO_AGENT_ID=%s
- CODERO_SESSION_ID=%s
- CODERO_SESSION_MODE=%s
- CODERO_BASE_URL=%s
- CODERO_TAILNET_BASE_URL=%s
- CODERO_PILOT_CLI=%s
- CODERO_PILOT_CONFIG=%s
- CODERO_WORKTREE=%s
- TEST_REPO=%s
- TEST_BRANCH=%s
- TEST_TASK_ID=%s

This session is already registered by the launcher.
This session is already claimed for this window.
Do not use the codero binary from PATH for this session.
Before doing any other work in this window, run exactly this command and stop if it fails:
- "$CODERO_PILOT_CLI" --config "$CODERO_PILOT_CONFIG" session confirm --session-id "$CODERO_SESSION_ID" --agent-id "$CODERO_AGENT_ID"
Use these values unchanged when attaching or heartbeating.
`, cfg.AgentID, cfg.SessionID, cfg.Mode, cfg.BaseURL, cfg.TailnetBaseURL, cfg.CLIPath, cfg.ConfigPath, cfg.Worktree, cfg.Repo, cfg.Branch, cfg.TaskID)

	if err := os.WriteFile(agentPath, []byte(agentBody), 0o644); err != nil {
		return nil, fmt.Errorf("bootstrap write AGENT.md: %w", err)
	}
	if err := os.WriteFile(sessionPath, []byte(sessionBody), 0o644); err != nil {
		return nil, fmt.Errorf("bootstrap write SESSION.md: %w", err)
	}

	exports := map[string]string{
		"CODERO_AGENT_ID":           cfg.AgentID,
		"CODERO_SESSION_ID":         cfg.SessionID,
		"CODERO_SESSION_MODE":       cfg.Mode,
		"CODERO_WORKTREE":           cfg.Worktree,
		"CODERO_RUNTIME_ROOT":       cfg.RuntimeRoot,
		"CODERO_RUNTIME_DIR":        runtimeDir,
		"CODERO_RUNTIME_AGENT_MD":   agentPath,
		"CODERO_RUNTIME_SESSION_MD": sessionPath,
	}
	if cfg.BaseURL != "" {
		exports["CODERO_BASE_URL"] = cfg.BaseURL
	}
	if cfg.TailnetBaseURL != "" {
		exports["CODERO_TAILNET_BASE_URL"] = cfg.TailnetBaseURL
	}
	if cfg.Repo != "" {
		exports["TEST_REPO"] = cfg.Repo
	}
	if cfg.Branch != "" {
		exports["TEST_BRANCH"] = cfg.Branch
	}
	if cfg.TaskID != "" {
		exports["TEST_TASK_ID"] = cfg.TaskID
	}
	if cfg.Worktree != "" {
		exports["TEST_WORKTREE"] = cfg.Worktree
	}
		if cfg.ConfigPath != "" {
			exports["CODERO_PILOT_CONFIG"] = cfg.ConfigPath
		}
		exports["CODERO_PILOT_CLI"] = cfg.CLIPath

		return &sessionBootstrapResult{
		RuntimeDir:       runtimeDir,
		RuntimeAgentMD:   agentPath,
		RuntimeSessionMD: sessionPath,
		Exports:          exports,
	}, nil
}

func renderBootstrapExports(exports map[string]string) string {
	keys := make([]string, 0, len(exports))
	for key := range exports {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "export %s=%q\n", key, exports[key])
	}
	return b.String()
}
