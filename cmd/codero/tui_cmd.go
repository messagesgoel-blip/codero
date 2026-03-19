package main

// tui_cmd.go — "codero tui" canonical interactive terminal UI entrypoint.

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/codero/codero/internal/tui"
	"github.com/spf13/cobra"
)

// tuiCmd is the canonical interactive operator shell for Codero.
// It replaces the ad-hoc "gate-status --watch" entry point with a first-class
// command that supports explicit view selection, theme, and refresh control.
func tuiCmd() *cobra.Command {
	var (
		repoPath    string
		intervalSec int
		themeName   string
		viewName    string
		noAltScreen bool
	)

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive Codero terminal UI",
		Long: `Launch the Bubble Tea operator shell with queue, gate, events, and findings views.

The TUI provides a full-screen interactive overview of the codero control plane:
  - Left pane:   branch list with state and queue position
  - Center pane: tabbed views — output / events / queue / findings
  - Right pane:  gate status, progress bar, blocker comments

Refreshes automatically at --interval seconds. Press q or Ctrl+C to quit.

Keyboard shortcuts:
  h / ?          toggle help
  Tab / Shift+Tab  cycle center tabs
  1-4            jump to tab by number
  H/J/K/L        move focus between panes
  r              force refresh
  q / Ctrl+C     quit

Examples:
  codero tui
  codero tui --view gate --interval 3
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

			theme := resolveTheme(themeName)
			initialTab := resolveInitialTab(viewName)

			initialVM := tui.AdapterFromPath(repoPath)
			cfg := tui.Config{
				RepoPath:   repoPath,
				Interval:   time.Duration(intervalSec) * time.Second,
				Theme:      theme,
				WatchMode:  true,
				InitialVM:  initialVM,
				InitialTab: initialTab,
			}

			opts := []tea.ProgramOption{}
			if !noAltScreen {
				opts = append(opts, tea.WithAltScreen())
			}

			p := tea.NewProgram(tui.New(cfg), opts...)
			_, err := p.Run()
			return err
		},
	}

	cmd.Flags().StringVarP(&repoPath, "repo-path", "r", "", "path to repository root (default: current directory)")
	cmd.Flags().IntVar(&intervalSec, "interval", 5, "auto-refresh interval in seconds")
	cmd.Flags().StringVar(&themeName, "theme", "dark",
		"UI theme: dark (default), light, system, dracula, vscode")
	cmd.Flags().StringVar(&viewName, "view", "gate",
		"initial center-pane view: gate, logs, queue, events, output")
	cmd.Flags().BoolVar(&noAltScreen, "no-alt-screen", false,
		"disable alt-screen mode (useful in tmux or CI-adjacent terminals)")

	return cmd
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
