package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// handleCodexHooks generates or installs Codex CLI hooks.json.
func handleCodexHooks(print, install, force bool) error {
	hooks := generateCodexHooks()
	hooksJSON, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal codex hooks: %w", err)
	}

	if print {
		fmt.Println(string(hooksJSON))
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	hooksPath := codexHooksPath(homeDir)
	status, err := installStandaloneJSON(hooksPath, hooks, force)
	if err != nil {
		return fmt.Errorf("install codex hooks: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Codex hooks %s to %s\n", status, hooksPath)

	if status == "unchanged" {
		return nil
	}

	return recordHookInstall("codex", hooksPath)
}

// generateCodexHooks returns the hooks.json structure for Codex CLI.
// Events: PreToolUse (working), PostToolUse (working+accum), Stop (waiting_for_input).
func generateCodexHooks() map[string]interface{} {
	f := buildHeartbeatFragments()

	return map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": assembleHeartbeat(f, "working", false)},
					},
				},
			},
			"PostToolUse": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": assembleHeartbeat(f, "working", true)},
					},
				},
			},
			"Stop": []map[string]interface{}{
				{
					"hooks": []map[string]string{
						{"type": "command", "command": assembleHeartbeat(f, "waiting_for_input", false)},
					},
				},
			},
		},
	}
}

// codexHooksPath returns the installation path for Codex hooks.json.
func codexHooksPath(homeDir string) string {
	return filepath.Join(homeDir, ".codex", "hooks.json")
}
