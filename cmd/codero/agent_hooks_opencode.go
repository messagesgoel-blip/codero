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
	status, err := installTextFile(pluginPath, plugin, force)
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
	f := buildHeartbeatFragments()

	working := assembleHeartbeat(f, "working", false)
	workingPost := assembleHeartbeat(f, "working", true)
	waiting := assembleHeartbeat(f, "waiting_for_input", false)

	// escapeForJS escapes characters that are special in JS template literals.
	// Backticks close the literal; \${ prevents ${VAR} from being interpreted as
	// a JS template expression. Single-quote handling is done in jsShellCall.
	escapeForJS := func(s string) string {
		s = strings.ReplaceAll(s, "`", "\\`")
		s = strings.ReplaceAll(s, "${", "\\${")
		return s
	}

	return fmt.Sprintf(`// codero-heartbeat.js — managed by codero (do not edit)
// Regenerate with: codero agent hooks --kind=opencode --install
import { exec } from "node:child_process";
const fire = (cmd) => exec(cmd, () => {});

export default async () => ({
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
`, jsShellCall(escapeForJS(working)),
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
	return filepath.Join(homeDir, ".config", "opencode", "plugin", "codero-heartbeat.js")
}
