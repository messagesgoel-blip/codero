package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/codero/codero/internal/config"
	"github.com/spf13/cobra"
)

const (
	defaultDaemonAddr = "127.0.0.1:8110"
	shimTemplate      = `#!/usr/bin/env bash
# Codero shim for %s — do not edit (managed by codero setup)
exec codero agent run --agent-id %s -- %q "$@"
`
)

// knownAgents is the list of agent binaries that setup scans for.
var knownAgents = []string{
	"claude",
	"codex",
	"aider",
	"opencode",
}

func setupCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up codero agent tracking (one-time)",
		Long: `Interactive setup that configures the codero daemon and installs
transparent agent shims so every agent launch is automatically tracked.

Safe to rerun — reports what changed. Use --force to overwrite everything.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite all config and shims")
	return cmd
}

func runSetup(force bool) error {
	fmt.Println()
	fmt.Println("  codero agent orchestrator — setup (v1)")
	fmt.Println()

	// Step 1: Check Docker
	fmt.Print("  [1/4] Checking Docker... ")
	if err := checkDocker(); err != nil {
		fmt.Println("✗")
		return fmt.Errorf("Docker is required: %w", err)
	}
	fmt.Println("✓ running")

	// Step 2: Check/start daemon
	fmt.Print("  [2/4] Checking daemon... ")
	daemonAddr := defaultDaemonAddr
	if daemonReachable(daemonAddr) {
		fmt.Printf("✓ running at %s\n", daemonAddr)
	} else {
		fmt.Println("not running")
		fmt.Println("        → Start the codero daemon and rerun setup.")
		fmt.Printf("        → Expected at %s\n", daemonAddr)
		fmt.Println("        → See: docker compose up -d (in your codero deploy directory)")
		fmt.Println()
		fmt.Println("  Setup will continue without daemon verification.")
		fmt.Println()
	}

	// Step 3: Write config
	fmt.Print("  [3/4] Writing config... ")
	configResult, err := writeUserConfig(daemonAddr, force)
	if err != nil {
		fmt.Println("✗")
		return fmt.Errorf("write config: %w", err)
	}
	if path, err := config.UserConfigPath(); err == nil {
		fmt.Printf("%s %s\n", configResult, path)
	} else {
		fmt.Printf("%s\n", configResult)
	}

	// Step 4: Install shims
	fmt.Println("  [4/4] Installing agent shims...")
	shimDir, err := shimBinDir()
	if err != nil {
		return fmt.Errorf("shim dir: %w", err)
	}

	uc, err := config.LoadUserConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if uc.Wrappers == nil {
		uc.Wrappers = make(map[string]config.WrapperConfig)
	}

	found := 0
	for _, agent := range knownAgents {
		realBinary := findRealBinary(agent, shimDir)
		if realBinary == "" {
			continue
		}
		found++
		result, err := installShim(shimDir, agent, realBinary, uc, force)
		if err != nil {
			fmt.Printf("        → %s: ✗ %v\n", agent, err)
			continue
		}
		fmt.Printf("        → %s at %s (%s)\n", agent, realBinary, result)
	}

	if found == 0 {
		fmt.Println("        → No known agents found in PATH")
		fmt.Println("        → You can still use: codero agent run --agent-id <name> -- /path/to/binary")
	}

	// Save updated config with wrapper entries
	if err := uc.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// PATH instruction
	fmt.Println()
	if !pathContains(shimDir) {
		fmt.Printf("  Add to your shell profile:\n")
		fmt.Printf("    export PATH=\"%s:$PATH\"\n", shimDir)
		fmt.Println()
	}

	fmt.Println("  ✓ Setup complete. Launch any agent normally — sessions auto-track.")
	if daemonReachable(daemonAddr) {
		fmt.Printf("    Dashboard: http://%s/dashboard/\n", daemonAddr)
	}
	fmt.Println()
	return nil
}

func checkDocker() error {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func daemonReachable(addr string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func writeUserConfig(daemonAddr string, force bool) (string, error) {
	existing, err := config.LoadUserConfig()
	if err != nil {
		return "", err
	}

	if existing.Version > 0 && !force {
		if existing.DaemonAddr == daemonAddr {
			return "unchanged", nil
		}
		existing.DaemonAddr = daemonAddr
		if err := existing.Save(); err != nil {
			return "", err
		}
		return "updated daemon_addr", nil
	}

	uc := &config.UserConfig{
		Version:    1,
		DaemonAddr: daemonAddr,
		SetupAt:    time.Now().UTC(),
		Wrappers:   existing.Wrappers,
	}
	if uc.Wrappers == nil {
		uc.Wrappers = make(map[string]config.WrapperConfig)
	}
	if err := uc.Save(); err != nil {
		return "", err
	}
	return "created", nil
}

func shimBinDir() (string, error) {
	dir, err := config.UserConfigDir()
	if err != nil {
		return "", err
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create shim dir: %w", err)
	}
	return binDir, nil
}

// findRealBinary scans PATH for the agent binary, excluding the shim dir.
func findRealBinary(agent, shimDir string) string {
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == shimDir {
			continue
		}
		candidate := filepath.Join(dir, agent)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			// Resolve symlinks
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				resolved = candidate
			}
			return resolved
		}
	}
	return ""
}

func installShim(shimDir, agent, realBinary string, uc *config.UserConfig, force bool) (string, error) {
	shimPath := filepath.Join(shimDir, agent)

	// Check existence before writing
	_, statErr := os.Stat(shimPath)
	existedBefore := statErr == nil

	// Skip if shim already exists with same real binary
	if !force && existedBefore {
		if existing, ok := uc.Wrappers[agent]; ok && existing.RealBinary == realBinary {
			return "unchanged", nil
		}
	}

	content := fmt.Sprintf(shimTemplate, agent, agent, realBinary)
	if err := os.WriteFile(shimPath, []byte(content), 0o755); err != nil {
		return "", fmt.Errorf("write shim: %w", err)
	}

	uc.Wrappers[agent] = config.WrapperConfig{
		RealBinary:  realBinary,
		InstalledAt: time.Now().UTC(),
	}

	if existedBefore {
		return "updated", nil
	}
	return "created", nil
}

func pathContains(dir string) bool {
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d == dir {
			return true
		}
	}
	return false
}
