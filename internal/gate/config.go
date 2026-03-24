package gate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Tier classifies how a gate check's enable/disable is governed.
type Tier string

const (
	// TierAlwaysOn checks cannot be disabled (security primitives).
	TierAlwaysOn Tier = "always_on"
	// TierConfigurable checks are ON by default but can be disabled.
	TierConfigurable Tier = "configurable"
	// TierOptIn checks are OFF by default, hard gates when enabled.
	TierOptIn Tier = "opt_in"
	// TierAISetting covers AI gate tuning knobs (quorum, timeouts, model).
	TierAISetting Tier = "ai_setting"
	// TierBehaviour covers gate runtime behaviour (infra-fail handling).
	TierBehaviour Tier = "behaviour"
)

// ConfigSource identifies where a resolved value came from.
type ConfigSource string

const (
	SourceDefault    ConfigSource = "default"
	SourceConfigFile ConfigSource = "config_file"
	SourceShellEnv   ConfigSource = "shell_env"
)

// ConfigEntry describes a single gate config variable from the spec.
type ConfigEntry struct {
	EnvVar       string // e.g. "CODERO_GOVET_ENABLED"
	DefaultValue string // e.g. "true"
	Tier         Tier
	Validate     func(string) error // nil means any string is valid
}

// ResolvedVar holds the effective value of a config variable plus its source.
type ResolvedVar struct {
	EnvVar       string       `json:"env_var"`
	Value        string       `json:"value"`
	DefaultValue string       `json:"default_value"`
	Source       ConfigSource `json:"source"`
	Tier         Tier         `json:"tier"`
}

// validateBool ensures the value is "true" or "false".
func validateBool(v string) error {
	if v != "true" && v != "false" {
		return fmt.Errorf("must be true or false, got %q", v)
	}
	return nil
}

// validatePositiveInt ensures the value is a positive integer.
func validatePositiveInt(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fmt.Errorf("must be a non-negative integer, got %q", v)
	}
	return nil
}

// validateNonNegativeInt ensures the value is a non-negative integer.
func validateNonNegativeInt(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fmt.Errorf("must be a non-negative integer, got %q", v)
	}
	return nil
}

// Registry is the complete Gate Config v1 env matrix from the spec.
// Tier 1 (always-on) checks have no env var and are not in this registry.
var Registry = []ConfigEntry{
	// ── Tier 2: Configurable (ON by default) ──
	{EnvVar: "CODERO_GOVET_ENABLED", DefaultValue: "true", Tier: TierConfigurable, Validate: validateBool},
	{EnvVar: "CODERO_TSC_ENABLED", DefaultValue: "true", Tier: TierConfigurable, Validate: validateBool},
	{EnvVar: "CODERO_SEMGREP_ENABLED", DefaultValue: "true", Tier: TierConfigurable, Validate: validateBool},
	{EnvVar: "CODERO_LITELLM_ENABLED", DefaultValue: "true", Tier: TierConfigurable, Validate: validateBool},

	// ── Tier 3: Opt-In (OFF by default) ──
	{EnvVar: "CODERO_COPILOT_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},
	{EnvVar: "CODERO_GEMINI_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},
	{EnvVar: "CODERO_AIDER_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},
	{EnvVar: "CODERO_PRAGENT_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},
	{EnvVar: "CODERO_CODERABBIT_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},
	{EnvVar: "CODERO_VALE_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},
	{EnvVar: "CODERO_HADOLINT_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},
	{EnvVar: "CODERO_SHELLCHECK_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},
	{EnvVar: "CODERO_SQLI_ENABLED", DefaultValue: "false", Tier: TierOptIn, Validate: validateBool},

	// ── AI gate settings ──
	{EnvVar: "CODERO_AI_QUORUM", DefaultValue: "1", Tier: TierAISetting, Validate: validateNonNegativeInt},
	{EnvVar: "CODERO_AI_BUDGET_SECONDS", DefaultValue: "180", Tier: TierAISetting, Validate: validatePositiveInt},
	{EnvVar: "CODERO_LITELLM_TIMEOUT", DefaultValue: "45", Tier: TierAISetting, Validate: validatePositiveInt},
	{EnvVar: "CODERO_COPILOT_TIMEOUT", DefaultValue: "75", Tier: TierAISetting, Validate: validatePositiveInt},
	{EnvVar: "CODERO_AI_MODEL", DefaultValue: "", Tier: TierAISetting, Validate: nil},
	{EnvVar: "CODERO_MIN_AI_GATES", DefaultValue: "1", Tier: TierAISetting, Validate: validateNonNegativeInt},

	// ── Gate behaviour ──
	{EnvVar: "CODERO_SKIP_INFRA_FAIL", DefaultValue: "true", Tier: TierBehaviour, Validate: validateBool},
}

// registryIndex maps env var name to its Registry entry for O(1) lookup.
var registryIndex map[string]*ConfigEntry

func init() {
	registryIndex = make(map[string]*ConfigEntry, len(Registry))
	for i := range Registry {
		registryIndex[Registry[i].EnvVar] = &Registry[i]
	}
}

// LookupEntry returns the config entry for a given env var name, or nil.
func LookupEntry(envVar string) *ConfigEntry {
	return registryIndex[envVar]
}

// DefaultConfigFilePath returns the canonical gate config file location.
func DefaultConfigFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codero", "config.env")
}

// ParseConfigFile reads a config.env file and returns a map of key→value.
// Blank lines and lines starting with # are ignored.
// Lines without = are silently skipped.
// Values are not unquoted; the raw right-hand side is preserved.
func ParseConfigFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // missing file → empty map, no error
		}
		return nil, fmt.Errorf("gate config: open %s: %w", path, err)
	}
	defer f.Close()

	m := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue // no = sign → skip
		}
		m[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("gate config: read %s: %w", path, err)
	}
	return m, nil
}

// ResolveEffective computes the effective config for every registered variable
// using Gate Config v1 precedence: shell env > config file > built-in default.
func ResolveEffective(configFilePath string) ([]ResolvedVar, error) {
	fileVars, err := ParseConfigFile(configFilePath)
	if err != nil {
		return nil, err
	}
	if fileVars == nil {
		fileVars = make(map[string]string)
	}

	result := make([]ResolvedVar, 0, len(Registry))
	for _, entry := range Registry {
		rv := ResolvedVar{
			EnvVar:       entry.EnvVar,
			DefaultValue: entry.DefaultValue,
			Tier:         entry.Tier,
		}

		// Precedence: shell env > config file > default.
		if envVal, ok := os.LookupEnv(entry.EnvVar); ok && envVal != "" {
			rv.Value = envVal
			rv.Source = SourceShellEnv
		} else if fileVal, ok := fileVars[entry.EnvVar]; ok {
			rv.Value = fileVal
			rv.Source = SourceConfigFile
		} else {
			rv.Value = entry.DefaultValue
			rv.Source = SourceDefault
		}

		result = append(result, rv)
	}
	return result, nil
}

// ResolveEffectiveMap is like ResolveEffective but returns a map keyed by env var.
func ResolveEffectiveMap(configFilePath string) (map[string]ResolvedVar, error) {
	vars, err := ResolveEffective(configFilePath)
	if err != nil {
		return nil, err
	}
	m := make(map[string]ResolvedVar, len(vars))
	for _, v := range vars {
		m[v.EnvVar] = v
	}
	return m, nil
}

// configFileMu serialises writes to the config file.
var configFileMu sync.Mutex

// SaveConfigVar atomically updates a single variable in the config file.
// If the file does not exist, it is created with the full default template.
// If the key is not in the registry, an error is returned.
// If the value fails validation, an error is returned.
// Always-on tier keys cannot be modified (they have no env var in the registry).
func SaveConfigVar(path, envVar, value string) error {
	entry := LookupEntry(envVar)
	if entry == nil {
		return fmt.Errorf("gate config: unknown variable %q", envVar)
	}
	if entry.Validate != nil {
		if err := entry.Validate(value); err != nil {
			return fmt.Errorf("gate config: %s: %w", envVar, err)
		}
	}

	configFileMu.Lock()
	defer configFileMu.Unlock()

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("gate config: mkdir: %w", err)
	}

	// Read existing file or generate default template.
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			content = []byte(DefaultConfigFileContent())
		} else {
			return fmt.Errorf("gate config: read %s: %w", path, err)
		}
	}

	// Update or append the key.
	lines := strings.Split(string(content), "\n")
	found := false
	prefix := envVar + "="
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			lines[i] = envVar + "=" + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, envVar+"="+value)
	}

	// Atomic write via temp + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")), 0o640); err != nil {
		return fmt.Errorf("gate config: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("gate config: rename: %w", err)
	}
	return nil
}

// DefaultConfigFileContent returns the canonical default config.env content
// per Gate Config v1 spec §4.
func DefaultConfigFileContent() string {
	var b strings.Builder
	b.WriteString("# Codero Gate Configuration — $HOME/.codero/config.env\n")
	b.WriteString("# Precedence: shell env > this file > built-in defaults\n")
	b.WriteString("# See: Gate Config Spec v1\n\n")

	sections := []struct {
		header string
		tier   Tier
	}{
		{"# ── Configurable checks (on by default) ──────────────────────────", TierConfigurable},
		{"# ── Opt-in AI reviewers (off by default) ─────────────────────────", TierOptIn},
		{"# ── AI gate settings ──────────────────────────────────────────────", TierAISetting},
		{"# ── Gate behaviour ────────────────────────────────────────────────", TierBehaviour},
	}
	for _, sec := range sections {
		b.WriteString(sec.header + "\n")
		for _, e := range Registry {
			if e.Tier == sec.tier {
				b.WriteString(e.EnvVar + "=" + e.DefaultValue + "\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ConfigDrift describes a variable where the shell env overrides the file value.
type ConfigDrift struct {
	EnvVar   string `json:"env_var"`
	FileVal  string `json:"file_value"`
	ShellVal string `json:"shell_value"`
}

// DetectDrifts returns variables where the shell env overrides the config file value.
func DetectDrifts(configFilePath string) ([]ConfigDrift, error) {
	fileVars, err := ParseConfigFile(configFilePath)
	if err != nil {
		return nil, err
	}
	if fileVars == nil {
		return nil, nil
	}

	var drifts []ConfigDrift
	for _, entry := range Registry {
		fileVal, inFile := fileVars[entry.EnvVar]
		envVal, inEnv := os.LookupEnv(entry.EnvVar)
		if inFile && inEnv && envVal != "" && envVal != fileVal {
			drifts = append(drifts, ConfigDrift{
				EnvVar:   entry.EnvVar,
				FileVal:  fileVal,
				ShellVal: envVal,
			})
		}
	}
	return drifts, nil
}

// AlwaysOnChecks returns the names of checks that cannot be disabled (Tier 1).
func AlwaysOnChecks() []string {
	return []string{"path-guard", "gitleaks", "ruff"}
}

// EffectiveBool resolves a boolean config variable from the effective config.
// Returns the default if the variable is not found or cannot be parsed.
func EffectiveBool(vars map[string]ResolvedVar, envVar string) bool {
	if rv, ok := vars[envVar]; ok {
		return rv.Value == "true"
	}
	if entry := LookupEntry(envVar); entry != nil {
		return entry.DefaultValue == "true"
	}
	return false
}

// EffectiveInt resolves an integer config variable from the effective config.
func EffectiveInt(vars map[string]ResolvedVar, envVar string, fallback int) int {
	if rv, ok := vars[envVar]; ok {
		if n, err := strconv.Atoi(rv.Value); err == nil {
			return n
		}
	}
	return fallback
}

// SortedEnvVars returns all registered env var names in sorted order.
func SortedEnvVars() []string {
	names := make([]string, len(Registry))
	for i, e := range Registry {
		names[i] = e.EnvVar
	}
	sort.Strings(names)
	return names
}
