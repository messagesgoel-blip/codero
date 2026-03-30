package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func agentHooksCmd(_ *string) *cobra.Command {
	var (
		install bool
		print   bool
	)

	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Generate or install agent hook configuration",
		Long:  "Generates Claude Code hooks that report agent status to codero via session heartbeat.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !install && !print {
				print = true // default to print
			}

			hooks := generateClaudeHooks()
			hooksJSON, err := json.MarshalIndent(hooks, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal hooks: %w", err)
			}

			if print {
				fmt.Println(string(hooksJSON))
				return nil
			}

			// Install: merge into ~/.claude/settings.json
			settingsPath := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
			return mergeClaudeSettings(settingsPath, hooks)
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "install hooks into ~/.claude/settings.json")
	cmd.Flags().BoolVar(&print, "print", false, "print hooks JSON to stdout (default)")

	return cmd
}

func generateClaudeHooks() map[string]interface{} {
	// Claude Code hooks format: https://docs.anthropic.com/en/docs/claude-code/hooks
	heartbeatBase := "codero session heartbeat"

	return map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": heartbeatBase + " --status=working --progress"},
					},
				},
			},
			"PostToolUse": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": heartbeatBase + " --status=working --progress"},
					},
				},
			},
			"Notification": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": heartbeatBase + " --status=waiting_for_input"},
					},
				},
			},
		},
	}
}

func mergeClaudeSettings(path string, hooks map[string]interface{}) error {
	existing := make(map[string]interface{})

	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parse existing settings at %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read settings: %w", err)
	}

	// Merge hooks section
	for k, v := range hooks {
		existing[k] = v
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ensure settings dir: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Hooks installed to %s\n", path)
	return nil
}
