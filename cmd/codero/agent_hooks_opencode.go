package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// handleOpenCodeHooks generates or installs an OpenCode JS plugin.
// install is accepted for API parity with handleClaudeHooks/handleCodexHooks;
// when print is false the plugin is always written (install is implied).
func handleOpenCodeHooks(print, install, force bool) error { //nolint:unparam // install kept for parity
	plugin := generateOpenCodePlugin()

	if print {
		fmt.Println(plugin)
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	pluginPath := openCodePluginPath(homeDir)
	status, err := installOpenCodeLikePlugin(pluginPath, legacyOpenCodePluginPath(homeDir), plugin, force)
	if err != nil {
		return fmt.Errorf("install opencode plugin: %w", err)
	}
	fmt.Fprintf(os.Stderr, "OpenCode plugin %s to %s\n", status, pluginPath)

	if status == "unchanged" {
		return nil
	}

	return recordHookInstall("opencode", pluginPath)
}

// generateOpenCodePlugin returns the JavaScript plugin source for OpenCode.
// The plugin shells out to bash for heartbeat commands, reusing the same
// shared shell fragments as Claude and Codex hooks.
func generateOpenCodePlugin() string {
	return generateOpenCodePluginSource("opencode")
}

func generateOpenCodePluginSource(kind string) string {
	f := buildHeartbeatFragments()

	working := assembleHeartbeat(f, "working", false, true)
	workingPost := assembleHeartbeat(f, "working", true, false)
	waiting := assembleHeartbeat(f, "waiting_for_input", false, false)

	// escapeForJS escapes characters that are special in JS template literals.
	// Backticks close the literal; \${ prevents ${VAR} from being interpreted as
	// a JS template expression. Single-quote handling is done in jsShellCall.
	escapeForJS := func(s string) string {
		s = strings.ReplaceAll(s, "`", "\\`")
		s = strings.ReplaceAll(s, "${", "\\${")
		return s
	}

	return fmt.Sprintf(`// codero-heartbeat.js — managed by codero (do not edit)
// Regenerate with: codero agent hooks --kind=%s --install
import { exec } from "node:child_process";
const fire = (cmd) => exec(cmd, () => {});

export const CoderoHeartbeatPlugin = async () => ({
  "tool.execute.before": async () => {
    fire(%s);
  },
  "tool.execute.after": async () => {
    fire(%s);
  },
  "session.idle": async () => {
    fire(%s);
  }
});
`, kind,
		jsShellCall(escapeForJS(working)),
		jsShellCall(escapeForJS(workingPost)),
		jsShellCall(escapeForJS(waiting)))
}

// jsShellCall wraps a shell command in a JS template literal bash invocation.
// Single-quote escaping for shell quoting is handled here (not in escapeForJS).
func jsShellCall(shellCmd string) string {
	return "`bash -c '" + strings.ReplaceAll(shellCmd, "'", `'"'"'`) + "'`"
}

// openCodePluginPath returns the installation path for the OpenCode plugin.
func openCodePluginPath(homeDir string) string {
	return openCodeLikePluginPath(homeDir, "opencode")
}

func kiloCodePluginPath(homeDir string) string {
	return openCodeLikePluginPath(homeDir, "kilo")
}

func openCodeLikePluginPath(homeDir, dir string) string {
	return filepath.Join(homeDir, ".config", dir, "plugins", "codero-heartbeat.js")
}

func legacyOpenCodePluginPath(homeDir string) string {
	return legacyOpenCodeLikePluginPath(homeDir, "opencode")
}

func legacyKiloCodePluginPath(homeDir string) string {
	return legacyOpenCodeLikePluginPath(homeDir, "kilo")
}

func legacyOpenCodeLikePluginPath(homeDir, dir string) string {
	return filepath.Join(homeDir, ".config", dir, "plugin", "codero-heartbeat.js")
}

func installOpenCodeLikePlugin(primaryPath, legacyPath, content string, force bool) (string, error) {
	primaryStatus, err := installTextFile(primaryPath, content, force)
	if err != nil {
		return "", err
	}

	legacyStatus, err := installTextFile(legacyPath, content, force)
	if err != nil {
		return "", err
	}

	if primaryStatus == "unchanged" && legacyStatus == "unchanged" {
		return "unchanged", nil
	}
	if primaryStatus == "created" && legacyStatus == "created" {
		return "created", nil
	}
	return "updated", nil
}
