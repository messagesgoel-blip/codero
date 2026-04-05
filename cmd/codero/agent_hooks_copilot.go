package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// handleCopilotHooks generates or installs GitHub Copilot CLI hooks.
func handleCopilotHooks(print, install, force bool) error {
	hooks := generateCopilotHooks()
	hooksJSON, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal copilot hooks: %w", err)
	}

	if print {
		fmt.Println(string(hooksJSON))
		return nil
	}
	if !install {
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	configPath := copilotConfigPath(homeDir)
	status, err := installMergedJSONConfig(configPath, map[string]interface{}{"hooks": hooks}, force, true)
	if err != nil {
		return fmt.Errorf("install copilot hooks: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Copilot hooks %s to %s\n", status, configPath)

	if status == "unchanged" {
		return nil
	}

	return recordHookInstall("copilot", configPath)
}

// generateCopilotHooks returns the inline hooks object for ~/.copilot/config.json.
func generateCopilotHooks() map[string]interface{} {
	f := buildHeartbeatFragments()

	return map[string]interface{}{
		"sessionStart": []map[string]interface{}{
			copilotCommandHook(assembleHeartbeat(f, "waiting_for_input", false, false)),
		},
		"userPromptSubmitted": []map[string]interface{}{
			copilotCommandHook(assembleHeartbeat(f, "working", false, false)),
		},
		"preToolUse": []map[string]interface{}{
			copilotCommandHook(assembleHeartbeat(f, "working", false, true)),
		},
		"postToolUse": []map[string]interface{}{
			copilotCommandHook(assembleHeartbeat(f, "working", true, false)),
		},
		"errorOccurred": []map[string]interface{}{
			copilotCommandHook(assembleHeartbeat(f, "waiting_for_input", false, false)),
		},
		"sessionEnd": []map[string]interface{}{
			copilotCommandHook(assembleHeartbeat(f, "waiting_for_input", false, false)),
		},
	}
}

func copilotCommandHook(command string) map[string]interface{} {
	return map[string]interface{}{
		"type":       "command",
		"bash":       command,
		"timeoutSec": 10,
	}
}

func copilotConfigPath(homeDir string) string {
	return filepath.Join(homeDir, ".copilot", "config.json")
}
