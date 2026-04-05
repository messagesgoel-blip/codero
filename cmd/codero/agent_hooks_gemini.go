package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// handleGeminiHooks generates or installs Gemini CLI hooks into settings.json.
func handleGeminiHooks(print, install, force bool) error {
	settings := generateGeminiSettings()
	settingsJSON, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal gemini hooks: %w", err)
	}

	if print {
		fmt.Println(string(settingsJSON))
		return nil
	}
	if !install {
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	settingsPath := geminiSettingsPath(homeDir)
	status, err := installMergedJSONConfig(settingsPath, settings, force, false)
	if err != nil {
		return fmt.Errorf("install gemini hooks: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Gemini hooks %s to %s\n", status, settingsPath)

	if status == "unchanged" {
		return nil
	}

	return recordHookInstall("gemini", settingsPath)
}

func generateGeminiSettings() map[string]interface{} {
	f := buildHeartbeatFragments()

	return map[string]interface{}{
		"hooks": map[string]interface{}{
			"SessionStart": []map[string]interface{}{
				geminiHookGroup("", assembleHeartbeat(f, "waiting_for_input", false, false)),
			},
			"BeforeTool": []map[string]interface{}{
				geminiHookGroup("", assembleHeartbeat(f, "working", false, true)),
			},
			"AfterTool": []map[string]interface{}{
				geminiHookGroup("", assembleHeartbeat(f, "working", true, false)),
			},
			"AfterAgent": []map[string]interface{}{
				geminiHookGroup("", assembleHeartbeat(f, "waiting_for_input", false, false)),
			},
			"Notification": []map[string]interface{}{
				geminiHookGroup("", assembleHeartbeat(f, "waiting_for_input", false, false)),
			},
		},
	}
}

func geminiHookGroup(matcher, command string) map[string]interface{} {
	return map[string]interface{}{
		"matcher": matcher,
		"hooks": []map[string]interface{}{
			{
				"type":    "command",
				"command": geminiShellCommand(command),
				"timeout": 10000,
				"name":    "codero-heartbeat",
			},
		},
	}
}

func geminiShellCommand(shellCmd string) string {
	shellCmd = strings.ReplaceAll(shellCmd, "'", `'"'"'`)
	return "bash -lc '(" + shellCmd + `) >/dev/null 2>&1 || true; printf "{}"'`
}

func geminiSettingsPath(homeDir string) string {
	return filepath.Join(homeDir, ".gemini", "settings.json")
}
