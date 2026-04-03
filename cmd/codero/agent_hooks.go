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
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			settingsPath := filepath.Join(homeDir, ".claude", "settings.json")
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
	//
	// Context detection: repo/branch from git, output bytes from counter file,
	// compact count from session data directory.
	//
	// Output tracking: PostToolUse hooks receive tool_response on stdin.
	// We accumulate the byte count in a per-session counter file and report
	// the running total on each heartbeat.
	//
	// Compact detection: Claude Code compresses context when approaching limits.
	// We detect this by tracking the number of compaction markers in the session
	// data directory. Each compaction event increments the counter.

	// Repo/branch detection (shared across all hook types).
	// Prefer repo name from git remote URL (handles worktree dirs named "main").
	// Falls back to basename of toplevel if no remote is configured.
	repoDetect := `_cr=$(git remote get-url origin 2>/dev/null | sed 's|.*/||;s|\.git$||'); ` +
		`[ -z "$_cr" ] && _cr=$(git rev-parse --show-toplevel 2>/dev/null) && _cr=$(basename "$_cr") || true; ` +
		`_cb=$(git branch --show-current 2>/dev/null) || _cb=""; `
	repoFlags := `$([ -n "$_cr" ] && echo "--repo=$_cr") $([ -n "$_cb" ] && echo "--branch=$_cb")`

	// Output byte tracking: read accumulated bytes from counter file.
	outputTrack := `_ob=0; _sd="${TMPDIR:-/tmp}/codero-${CODERO_SESSION_ID:-unknown}"; ` +
		`_of="$_sd/output-bytes"; ` +
		`[ -f "$_of" ] && _ob=$(cat "$_of" 2>/dev/null || echo 0); `
	outputFlags := `$([ "$_ob" -gt 0 ] 2>/dev/null && echo "--output-bytes=$_ob")`

	// PostToolUse: accumulate raw stdin bytes to counter file (streaming, no buffer).
	// stdin contains the full tool_response JSON — we measure total bytes as an
	// approximation of output volume (includes envelope overhead).
	// Uses tee to pass through stdin while wc counts bytes, avoiding buffering.
	postToolAccum := `_sd="${TMPDIR:-/tmp}/codero-${CODERO_SESSION_ID:-unknown}"; ` +
		`mkdir -p "$_sd" 2>/dev/null || true; chmod 700 "$_sd" 2>/dev/null || true; ` +
		`_of="$_sd/output-bytes"; ` +
		`_nb=$(wc -c | tr -d '[:space:]'); ` +
		`_ob=0; [ -f "$_of" ] && _ob=$(cat "$_of" 2>/dev/null || echo 0); ` +
		`echo $((_ob + _nb)) > "$_of"; chmod 600 "$_of" 2>/dev/null || true; `

	// Auto-recovery: if heartbeat fails (daemon restart, ended session, secret mismatch),
	// silently re-register. If the original session ID was already ended, generate a new
	// one derived from the original. Caches the working session ID + secret in mode-0600 files.
	autoRecover := `_hb() { ` +
		`_sd="${TMPDIR:-/tmp}/codero-${CODERO_SESSION_ID:-unknown}"; ` +
		`mkdir -p "$_sd" 2>/dev/null || true; chmod 700 "$_sd" 2>/dev/null || true; ` +
		`_sf="$_sd/secret"; _idf="$_sd/session-id"; ` +
		`_hs="${CODERO_HEARTBEAT_SECRET}"; [ -f "$_sf" ] && _hs=$(cat "$_sf" 2>/dev/null); ` +
		`_sid="${CODERO_SESSION_ID}"; [ -f "$_idf" ] && _sid=$(cat "$_idf" 2>/dev/null); ` +
		`CODERO_HEARTBEAT_SECRET="$_hs" codero session heartbeat --session-id="$_sid" "$@" 2>/dev/null && return 0; ` +
		// Try re-register with original ID first.
		`_out=$(codero session register --session-id="$_sid" --agent-id="${CODERO_AGENT_ID:-unknown}" 2>&1); ` +
		`_ns=$(echo "$_out" | grep heartbeat_secret | awk '{print $2}'); ` +
		// If that failed (ended session), try with a new derived ID.
		`if [ -z "$_ns" ]; then ` +
		`_sid="${CODERO_SESSION_ID:-unknown}-r$(date +%s)"; ` +
		`_out=$(codero session register --session-id="$_sid" --agent-id="${CODERO_AGENT_ID:-unknown}" 2>&1); ` +
		`_ns=$(echo "$_out" | grep heartbeat_secret | awk '{print $2}'); ` +
		`fi; ` +
		`[ -n "$_ns" ] && echo "$_ns" > "$_sf" && echo "$_sid" > "$_idf" && ` +
		`CODERO_HEARTBEAT_SECRET="$_ns" codero session heartbeat --session-id="$_sid" "$@" 2>/dev/null; ` +
		`}; `

	heartbeatWorking := repoDetect + outputTrack + autoRecover +
		`_hb --status=working --progress ` + repoFlags + " " + outputFlags
	heartbeatWorkingPost := postToolAccum + repoDetect + outputTrack + autoRecover +
		`_hb --status=working --progress ` + repoFlags + " " + outputFlags
	heartbeatWaiting := repoDetect + outputTrack + autoRecover +
		`_hb --status=waiting_for_input ` + repoFlags + " " + outputFlags

	return map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": heartbeatWorking},
					},
				},
			},
			"PostToolUse": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": heartbeatWorkingPost},
					},
				},
			},
			"Notification": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": heartbeatWaiting},
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
