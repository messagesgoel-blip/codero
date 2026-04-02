package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codero/codero/internal/config"
	"github.com/spf13/cobra"
)

func agentHooksCmd(_ *string) *cobra.Command {
	var (
		install bool
		print   bool
		force   bool
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
			status, err := installClaudeHooks(settingsPath, hooks, force)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Hooks %s to %s\n", status, settingsPath)

			if status == "unchanged" {
				return nil
			}

			// Record installation in ~/.codero/config.yaml
			uc, err := config.LoadUserConfig()
			if err != nil {
				return fmt.Errorf("load user config: %w", err)
			}
			if uc.Hooks == nil {
				uc.Hooks = make(map[string]config.HooksConfig)
			}
			uc.Hooks["claude"] = config.HooksConfig{
				SettingsPath: settingsPath,
				InstalledAt:  time.Now().UTC(),
			}
			if err := uc.Save(); err != nil {
				return fmt.Errorf("save user config: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "install hooks into ~/.claude/settings.json")
	cmd.Flags().BoolVar(&print, "print", false, "print hooks JSON to stdout (default)")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall hooks even if already up to date")

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

// installClaudeHooks merges the given hooks map into the Claude Code settings
// file at path. It returns one of "created", "updated", or "unchanged".
// If force is true, the hooks section is always rewritten even if identical.
func installClaudeHooks(path string, hooks map[string]interface{}, force bool) (string, error) {
	existing := make(map[string]interface{})
	fileExisted := false

	data, err := os.ReadFile(path)
	if err == nil {
		fileExisted = true
		if err := json.Unmarshal(data, &existing); err != nil {
			return "", fmt.Errorf("parse existing settings at %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read settings: %w", err)
	}

	// Idempotency check: compare new hooks JSON with what is already stored.
	if !force && fileExisted {
		newHooksJSON, err := json.Marshal(hooks)
		if err != nil {
			return "", fmt.Errorf("marshal new hooks: %w", err)
		}
		// Build the would-be merged map for comparison.
		merged := shallowCopy(existing)
		for k, v := range hooks {
			merged[k] = v
		}
		mergedJSON, err := json.Marshal(merged)
		if err != nil {
			return "", fmt.Errorf("marshal merged settings: %w", err)
		}
		existingJSON, err := json.Marshal(existing)
		if err != nil {
			return "", fmt.Errorf("marshal existing settings: %w", err)
		}
		_ = newHooksJSON // used indirectly via merged comparison
		if string(mergedJSON) == string(existingJSON) {
			return "unchanged", nil
		}
	}

	// Merge hooks section into existing map.
	for k, v := range hooks {
		existing[k] = v
	}

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("ensure settings dir: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return "", fmt.Errorf("write settings: %w", err)
	}

	if fileExisted {
		return "updated", nil
	}
	return "created", nil
}

// shallowCopy returns a shallow copy of m.
func shallowCopy(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
