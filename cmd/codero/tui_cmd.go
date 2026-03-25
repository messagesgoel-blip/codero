package main

// tui_cmd.go — "codero tui" canonical interactive terminal UI entrypoint.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui"
	"github.com/spf13/cobra"
)

// tuiCmd is the canonical interactive operator shell for Codero.
// It replaces the ad-hoc "gate-status --watch" entry point with a first-class
// command that supports explicit view selection, theme, and refresh control.
func tuiCmd(configPath *string) *cobra.Command {
	var (
		repoPath    string
		repoSlug    string
		branchName  string
		intervalSec int
		themeName   string
		viewName    string
		noAltScreen bool
	)

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive Codero terminal UI",
		Long: `Launch the Bubble Tea operator shell with agents, logs, pipeline, findings, and review prompt panes.

The TUI provides a full-screen interactive overview of the codero control plane:
  - Left pane:   agents and relay orchestration
  - Middle panes: live logs, pipeline progress, and findings
  - Bottom pane: review prompt and local review commands

Refreshes automatically at --interval seconds. Press q or Ctrl+C to quit.

Keyboard shortcuts:
  h / ?          toggle help
  H/J/K/L        move focus between panes
  r              force refresh
  q / Ctrl+C     quit

Examples:
  codero tui
  codero tui --view queue --interval 3
  codero tui --theme dracula
  codero tui --no-alt-screen          # useful in tmux or terminals that don't support alt screen`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tui.IsInteractiveTTY() {
				return fmt.Errorf("codero tui requires an interactive terminal (stdin and stdout must be a TTY)")
			}

			if repoPath == "" {
				absPath, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getwd: %w", err)
				}
				repoPath = absPath
			}

			if intervalSec < 1 {
				intervalSec = 5
			}

			cfgFile, err := loadConfig(*configPathForCmd(cmd))
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			if repoSlug == "" {
				repoSlug = resolveTUIRepoSlug(repoPath, cfgFile.Repos)
			}
			if branchName == "" {
				if currentBranch, branchErr := getCurrentBranchAt(repoPath); branchErr == nil {
					branchName = currentBranch
				}
			}

			stateDB, err := state.Open(cfgFile.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer stateDB.Close()

			theme := resolveTheme(themeName)
			initialTab := resolveInitialTab(viewName)

			initialVM := tui.AdapterFromPath(repoPath)
			cfg := tui.Config{
				RepoPath:   repoPath,
				Repo:       repoSlug,
				Branch:     branchName,
				Context:    cmd.Context(),
				Interval:   time.Duration(intervalSec) * time.Second,
				Theme:      theme,
				WatchMode:  true,
				InitialVM:  initialVM,
				InitialTab: initialTab,
				StateDB:    stateDB,
			}

			opts := []tea.ProgramOption{tea.WithContext(cmd.Context())}
			if !noAltScreen {
				opts = append(opts, tea.WithAltScreen())
			}

			p := tea.NewProgram(tui.New(cfg), opts...)
			_, err = p.Run()
			return err
		},
	}

	cmd.Flags().StringVarP(&repoPath, "repo-path", "r", "", "path to repository root (default: current directory)")
	cmd.Flags().StringVarP(&repoSlug, "repo", "R", "", "repository (owner/repo) for live dashboard/state queries")
	cmd.Flags().StringVarP(&branchName, "branch", "b", "", "branch name for live dashboard/state queries (default: current git branch)")
	cmd.Flags().IntVar(&intervalSec, "interval", 5, "auto-refresh interval in seconds")
	cmd.Flags().StringVar(&themeName, "theme", "dark",
		"UI theme: dark (default), light, system, dracula, vscode")
	cmd.Flags().StringVar(&viewName, "view", "gate",
		"review context for the assistant prompt: gate, logs, queue, events, output")
	cmd.Flags().BoolVar(&noAltScreen, "no-alt-screen", false,
		"disable alt-screen mode (useful in tmux or CI-adjacent terminals)")

	return cmd
}

func resolveTUIRepoSlug(repoPath string, configured []string) string {
	if v := strings.TrimSpace(os.Getenv("TEST_REPO")); v != "" {
		return v
	}
	if len(configured) > 0 && strings.TrimSpace(configured[0]) != "" {
		return configured[0]
	}
	remoteURL := gitRemoteOriginURL(repoPath)
	if remoteURL == "" {
		return ""
	}
	return parseRepoSlugFromRemote(remoteURL)
}

func gitRemoteOriginURL(repoPath string) string {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func parseRepoSlugFromRemote(remoteURL string) string {
	raw := strings.TrimSpace(strings.TrimSuffix(remoteURL, ".git"))
	if raw == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(raw, "git@"):
		if _, path, ok := strings.Cut(raw, ":"); ok {
			return trimRepoSlug(path)
		}
	case strings.Contains(raw, "://"):
		if idx := strings.Index(raw, "github.com/"); idx >= 0 {
			return trimRepoSlug(raw[idx+len("github.com/"):])
		}
		if idx := strings.Index(raw, "/"); idx >= 0 {
			return trimRepoSlug(raw[idx+1:])
		}
	}
	return trimRepoSlug(raw)
}

func trimRepoSlug(path string) string {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// resolveTheme maps a theme name string to a tui.Theme.
// "dark" and "dracula" → DefaultTheme; "light", "system", "vscode" → AltTheme.
func resolveTheme(name string) tui.Theme {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "light", "vscode":
		return tui.AltTheme
	default:
		// dark, dracula, system all use the default dark theme.
		return tui.DefaultTheme
	}
}

// resolveInitialTab converts a view name to the corresponding tui.Tab constant.
func resolveInitialTab(view string) tui.Tab {
	switch strings.ToLower(strings.TrimSpace(view)) {
	case "events":
		return tui.TabEvents
	case "queue":
		return tui.TabQueue
	case "output":
		return tui.TabOutput
	default:
		// "logs", "gate", "findings", and unknown values default to the primary
		// logs & architecture view.
		return tui.TabLogs
	}
}
