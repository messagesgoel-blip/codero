package main

import (
	"fmt"
	"os"
)

// handleKiloCodeHooks generates or installs a Kilo Code JS plugin.
func handleKiloCodeHooks(print, install, force bool) error { //nolint:unparam // install kept for parity
	plugin := generateKiloCodePlugin()

	if print {
		fmt.Println(plugin)
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	pluginPath := kiloCodePluginPath(homeDir)
	status, err := installTextFile(pluginPath, plugin, force)
	if err != nil {
		return fmt.Errorf("install kilocode plugin: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Kilo Code plugin %s to %s\n", status, pluginPath)

	if status == "unchanged" {
		return nil
	}

	return recordHookInstall("kilocode", pluginPath)
}

func generateKiloCodePlugin() string {
	return generateOpenCodePluginSource("kilocode")
}
