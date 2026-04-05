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
		kind    string
	)

	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Generate or install agent hook configuration",
		Long: `Generates heartbeat hooks that report agent status to Codero.

Supports multiple agent families via --kind:
  claude    Claude Code hooks (PreToolUse/PostToolUse/Notification)
  codex     Codex CLI hooks.json (PreToolUse/PostToolUse/Stop)
  opencode  OpenCode JS plugin (tool.execute/session.idle)
  kilocode Kilo Code JS plugin (OpenCode-compatible)
  copilot  GitHub Copilot CLI hooks (session/tool lifecycle)
  gemini   Gemini CLI settings hooks (BeforeTool/AfterAgent)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !install && !print {
				print = true // default to print
			}

			normalized := config.NormalizeAgentKind(kind)
			if normalized == "" {
				return fmt.Errorf("unsupported --kind %q; supported: claude, codex, opencode, kilocode, copilot, gemini", kind)
			}

			switch normalized {
			case config.AgentKindClaude:
				return handleClaudeHooks(print, install, force)
			case config.AgentKindCodex:
				return handleCodexHooks(print, install, force)
			case config.AgentKindOpenCode:
				return handleOpenCodeHooks(print, install, force)
			case config.AgentKindKiloCode:
				return handleKiloCodeHooks(print, install, force)
			case config.AgentKindCopilot:
				return handleCopilotHooks(print, install, force)
			case config.AgentKindGemini:
				return handleGeminiHooks(print, install, force)
			default:
				return fmt.Errorf("hooks not yet supported for agent kind %q (supported: claude, codex, opencode, kilocode, copilot, gemini)", kind)
			}
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "install hooks into the agent's config directory")
	cmd.Flags().BoolVar(&print, "print", false, "print hooks to stdout (default)")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall hooks even if already up to date")
	cmd.Flags().StringVar(&kind, "kind", "claude", "agent family: claude, codex, opencode, kilocode, copilot, gemini")

	return cmd
}

// --- Shared heartbeat shell fragments ---

// heartbeatFragments holds reusable shell script building blocks
// for heartbeat hooks across all agent families.
type heartbeatFragments struct {
	ScratchInit   string // creates the per-session scratch directory
	RepoDetect    string // detects repo name + branch from git
	RepoFlags     string // --repo=X --branch=Y flag expansion
	OutputTrack   string // reads accumulated output bytes from counter file
	OutputFlags   string // --output-bytes=N flag expansion
	ToolTrack     string // reads accumulated tool call count
	ToolFlags     string // --tool-calls=N flag expansion
	PostToolAccum string // accumulates stdin byte count to counter file
	PreToolCount  string // increments tool call count
	AutoRecover   string // _hb() function with re-register fallback
}

func buildHeartbeatFragments() heartbeatFragments {
	scratchInit := `_sd="${TMPDIR:-/tmp}/codero-${CODERO_SESSION_ID:-unknown}"; ` +
		`mkdir -p "$_sd" 2>/dev/null || true; chmod 700 "$_sd" 2>/dev/null || true; `
	repoDetect := `_cr=$(git remote get-url origin 2>/dev/null | sed 's|.*/||;s|\.git$||'); ` +
		`[ -z "$_cr" ] && _cr=$(git rev-parse --show-toplevel 2>/dev/null) && _cr=$(basename "$_cr") || true; ` +
		`_cb=$(git branch --show-current 2>/dev/null) || _cb=""; `
	repoFlags := `$([ -n "$_cr" ] && echo "--repo=$_cr") $([ -n "$_cb" ] && echo "--branch=$_cb")`

	outputTrack := `_ob=0; _of="$_sd/output-bytes"; ` +
		`[ -f "$_of" ] && _ob=$(cat "$_of" 2>/dev/null || echo 0); `
	outputFlags := `$([ "$_ob" -gt 0 ] 2>/dev/null && echo "--output-bytes=$_ob")`

	toolTrack := `_tc=0; _tf="$_sd/tool-calls"; ` +
		`[ -f "$_tf" ] && _tc=$(cat "$_tf" 2>/dev/null || echo 0); `
	toolFlags := `$([ "$_tc" -gt 0 ] 2>/dev/null && echo "--tool-calls=$_tc")`

	postToolAccum := `_of="$_sd/output-bytes"; ` +
		`_nb=$(wc -c | tr -d '[:space:]'); ` +
		`_ob=0; [ -f "$_of" ] && _ob=$(cat "$_of" 2>/dev/null || echo 0); ` +
		`echo $((_ob + _nb)) > "$_of"; chmod 600 "$_of" 2>/dev/null || true; `

	preToolCount := `_tf="$_sd/tool-calls"; ` +
		`_tc=0; [ -f "$_tf" ] && _tc=$(cat "$_tf" 2>/dev/null || echo 0); ` +
		`echo $((_tc + 1)) > "$_tf"; chmod 600 "$_tf" 2>/dev/null || true; `

	autoRecover := `_hb() { ` +
		`_sf="$_sd/secret"; _idf="$_sd/session-id"; ` +
		`_hs="${CODERO_HEARTBEAT_SECRET}"; [ -f "$_sf" ] && _hs=$(cat "$_sf" 2>/dev/null); ` +
		`_sid="${CODERO_SESSION_ID}"; [ -f "$_idf" ] && _sid=$(cat "$_idf" 2>/dev/null); ` +
		`CODERO_HEARTBEAT_SECRET="$_hs" codero session heartbeat --session-id="$_sid" "$@" 2>/dev/null && return 0; ` +
		`_out=$(codero session register --session-id="$_sid" --agent-id="${CODERO_AGENT_ID:-unknown}" 2>&1); ` +
		`_ns=$(echo "$_out" | grep heartbeat_secret | awk '{print $2}'); ` +
		`if [ -z "$_ns" ]; then ` +
		`_sid="${CODERO_SESSION_ID:-unknown}-r$(date +%s)"; ` +
		`_out=$(codero session register --session-id="$_sid" --agent-id="${CODERO_AGENT_ID:-unknown}" 2>&1); ` +
		`_ns=$(echo "$_out" | grep heartbeat_secret | awk '{print $2}'); ` +
		`fi; ` +
		`[ -n "$_ns" ] && echo "$_ns" > "$_sf" && echo "$_sid" > "$_idf" && ` +
		`CODERO_HEARTBEAT_SECRET="$_ns" codero session heartbeat --session-id="$_sid" "$@" 2>/dev/null; ` +
		`}; `

	return heartbeatFragments{
		ScratchInit:   scratchInit,
		RepoDetect:    repoDetect,
		RepoFlags:     repoFlags,
		OutputTrack:   outputTrack,
		OutputFlags:   outputFlags,
		ToolTrack:     toolTrack,
		ToolFlags:     toolFlags,
		PostToolAccum: postToolAccum,
		PreToolCount:  preToolCount,
		AutoRecover:   autoRecover,
	}
}

// assembleHeartbeat composes a full heartbeat shell command.
// status is "working" or "waiting_for_input".
// If includeAccum is true, PostToolUse stdin byte accumulation is prepended.
// If incrementToolCount is true, a per-session tool-call counter is incremented first.
func assembleHeartbeat(f heartbeatFragments, status string, includeAccum, incrementToolCount bool) string {
	prefix := f.ScratchInit
	if incrementToolCount {
		prefix += f.PreToolCount
	}
	if includeAccum {
		prefix += f.PostToolAccum
	}
	return prefix + f.RepoDetect + f.OutputTrack + f.ToolTrack + f.AutoRecover +
		`_hb --status=` + status + ` --progress ` + f.RepoFlags + " " + f.OutputFlags + " " + f.ToolFlags
}

// --- Claude Code hooks ---

func handleClaudeHooks(print, install, force bool) error {
	hooks := generateClaudeHooks()
	hooksJSON, err := json.MarshalIndent(hooks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hooks: %w", err)
	}

	if print {
		fmt.Println(string(hooksJSON))
		return nil
	}

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

	return recordHookInstall("claude", settingsPath)
}

func generateClaudeHooks() map[string]interface{} {
	f := buildHeartbeatFragments()

	return map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": assembleHeartbeat(f, "working", false, true)},
					},
				},
			},
			"PostToolUse": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": assembleHeartbeat(f, "working", true, false)},
					},
				},
			},
			"Notification": []map[string]interface{}{
				{
					"matcher": "",
					"hooks": []map[string]string{
						{"type": "command", "command": assembleHeartbeat(f, "waiting_for_input", false, false)},
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
	return installMergedJSONConfig(path, hooks, force, false)
}

// shallowCopy returns a shallow copy of m.
func shallowCopy(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// installMergedJSONConfig merges top-level keys into an existing JSON config.
// When allowJSONC is true, comments and trailing commas in the existing file are
// stripped before parsing.
func installMergedJSONConfig(path string, updates map[string]interface{}, force, allowJSONC bool) (string, error) {
	existing := make(map[string]interface{})
	fileExisted := false

	data, err := os.ReadFile(path)
	if err == nil {
		fileExisted = true
		if err := unmarshalJSONObject(data, &existing, allowJSONC); err != nil {
			return "", fmt.Errorf("parse existing settings at %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read settings: %w", err)
	}

	if !force && fileExisted {
		merged := shallowCopy(existing)
		for k, v := range updates {
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

	for k, v := range updates {
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

func unmarshalJSONObject(data []byte, out *map[string]interface{}, allowJSONC bool) error {
	if allowJSONC {
		data = normalizeJSONC(data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return err
	}
	if *out == nil {
		*out = make(map[string]interface{})
	}
	return nil
}

func normalizeJSONC(data []byte) []byte {
	return stripTrailingCommas(stripJSONComments(data))
}

func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(data); i++ {
		ch := data[i]
		next := byte(0)
		if i+1 < len(data) {
			next = data[i+1]
		}

		switch {
		case inLineComment:
			if ch == '\n' {
				inLineComment = false
				out = append(out, ch)
			}
		case inBlockComment:
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
		case inString:
			out = append(out, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
		default:
			if ch == '/' && next == '/' {
				inLineComment = true
				i++
				continue
			}
			if ch == '/' && next == '*' {
				inBlockComment = true
				i++
				continue
			}
			out = append(out, ch)
			if ch == '"' {
				inString = true
			}
		}
	}

	return out
}

func stripTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		ch := data[i]
		out = append(out, ch)

		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}
		if ch != ',' {
			continue
		}

		j := i + 1
		for j < len(data) {
			switch data[j] {
			case ' ', '\t', '\n', '\r':
				j++
				continue
			case '}', ']':
				out = out[:len(out)-1]
			}
			break
		}
	}

	return out
}

// --- Shared installation helpers ---

// recordHookInstall updates ~/.codero/config.yaml with hook installation metadata.
func recordHookInstall(familyKey, settingsPath string) error {
	uc, err := config.LoadUserConfig()
	if err != nil {
		return fmt.Errorf("load user config: %w", err)
	}
	if uc.Hooks == nil {
		uc.Hooks = make(map[string]config.HooksConfig)
	}
	uc.Hooks[familyKey] = config.HooksConfig{
		SettingsPath: settingsPath,
		InstalledAt:  time.Now().UTC(),
	}
	if err := uc.Save(); err != nil {
		return fmt.Errorf("save user config: %w", err)
	}
	return nil
}

// installStandaloneJSON writes a standalone JSON file with idempotency.
// Returns "created", "updated", or "unchanged".
func installStandaloneJSON(path string, content map[string]interface{}, force bool) (string, error) {
	newJSON, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	fileExisted := false
	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		fileExisted = true
		if !force && string(existing) == string(newJSON) {
			return "unchanged", nil
		}
	} else if !os.IsNotExist(readErr) {
		return "", fmt.Errorf("read existing file: %w", readErr)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("ensure dir: %w", err)
	}
	if err := os.WriteFile(path, newJSON, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	if fileExisted {
		return "updated", nil
	}
	return "created", nil
}

// installTextFile writes a text file with idempotency.
// Returns "created", "updated", or "unchanged".
func installTextFile(path string, content string, force bool) (string, error) {
	fileExisted := false
	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		fileExisted = true
		if !force && string(existing) == content {
			return "unchanged", nil
		}
	} else if !os.IsNotExist(readErr) {
		return "", fmt.Errorf("read existing file: %w", readErr)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("ensure dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	if fileExisted {
		return "updated", nil
	}
	return "created", nil
}
