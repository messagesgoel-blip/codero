package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentInfo describes a discovered agent shim with runtime status.
type AgentInfo struct {
	AgentID    string            `json:"agent_id"`
	ShimName   string            `json:"shim_name"`
	RealBinary string            `json:"real_binary"`
	Installed  bool              `json:"installed"`
	Disabled   bool              `json:"disabled"`
	EnvVars    map[string]string `json:"env_vars"`
}

// shimRe parses: exec codero agent run --agent-id <id> -- "<binary>" "$@"
var shimRe = regexp.MustCompile(`--agent-id\s+(\S+)\s+--\s+"?([^"$]+)"?`)

// DiscoverAgents scans ~/.codero/bin/ for shims and returns agent info.
func DiscoverAgents(uc *UserConfig) ([]AgentInfo, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("discover agents: %w", err)
	}
	binDir := filepath.Join(dir, "bin")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("discover agents: read bin dir: %w", err)
	}

	var agents []AgentInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(binDir, e.Name())
		agentID, realBin := parseShim(path)
		if agentID == "" {
			continue
		}
		_, statErr := os.Stat(realBin)
		var disabled bool
		var envVars map[string]string
		if uc != nil {
			disabled = uc.IsTrackingDisabled(agentID)
			if w, ok := uc.Wrappers[agentID]; ok && w.EnvVars != nil {
				envVars = w.EnvVars
			}
		}
		agents = append(agents, AgentInfo{
			AgentID:    agentID,
			ShimName:   e.Name(),
			RealBinary: realBin,
			Installed:  statErr == nil,
			Disabled:   disabled,
			EnvVars:    envVars,
		})
	}
	return agents, nil
}

func parseShim(path string) (agentID, realBinary string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "codero agent run") {
			continue
		}
		m := shimRe.FindStringSubmatch(line)
		if len(m) == 3 {
			return m[1], strings.TrimSpace(m[2])
		}
	}
	return "", ""
}

// UserConfig is the per-user config at ~/.codero/config.yaml.
// It is separate from the daemon Config and holds agent-side settings.
type UserConfig struct {
	Version        int                      `yaml:"version"`
	DaemonAddr     string                   `yaml:"daemon_addr"`
	SetupAt        time.Time                `yaml:"setup_at,omitempty"`
	Wrappers       map[string]WrapperConfig `yaml:"wrappers,omitempty"`
	DisabledAgents []string                 `yaml:"disabled_agents,omitempty"`
}

// IsTrackingDisabled returns true if the given agent ID is in the disabled list.
func (uc *UserConfig) IsTrackingDisabled(agentID string) bool {
	for _, a := range uc.DisabledAgents {
		if a == agentID {
			return true
		}
	}
	return false
}

// SetTrackingDisabled adds or removes an agent from the disabled list.
func (uc *UserConfig) SetTrackingDisabled(agentID string, disabled bool) {
	if disabled {
		if !uc.IsTrackingDisabled(agentID) {
			uc.DisabledAgents = append(uc.DisabledAgents, agentID)
		}
	} else {
		filtered := uc.DisabledAgents[:0]
		for _, a := range uc.DisabledAgents {
			if a != agentID {
				filtered = append(filtered, a)
			}
		}
		uc.DisabledAgents = filtered
	}
}

// WrapperConfig records a wrapped agent binary discovered during setup.
type WrapperConfig struct {
	RealBinary  string            `yaml:"real_binary"`
	InstalledAt time.Time         `yaml:"installed_at,omitempty"`
	EnvVars     map[string]string `yaml:"env_vars,omitempty"`
}

// UserConfigDir returns the codero config directory.
// Checks CODERO_USER_CONFIG_DIR first, then falls back to ~/.codero.
func UserConfigDir() (string, error) {
	if dir := os.Getenv("CODERO_USER_CONFIG_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("user config dir: mkdir: %w", err)
		}
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	dir := filepath.Join(home, ".codero")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("user config dir: mkdir: %w", err)
	}
	return dir, nil
}

// UserConfigPath returns ~/.codero/config.yaml.
func UserConfigPath() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// LoadUserConfig reads ~/.codero/config.yaml. Returns a zero-value
// UserConfig (not an error) if the file does not exist.
func LoadUserConfig() (*UserConfig, error) {
	path, err := UserConfigPath()
	if err != nil {
		return nil, fmt.Errorf("load user config: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserConfig{}, nil
		}
		return nil, fmt.Errorf("load user config: %w", err)
	}
	var uc UserConfig
	if err := yaml.Unmarshal(data, &uc); err != nil {
		return nil, fmt.Errorf("load user config: parse: %w", err)
	}
	return &uc, nil
}

// Save writes the UserConfig to ~/.codero/config.yaml.
func (uc *UserConfig) Save() error {
	path, err := UserConfigPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(uc)
	if err != nil {
		return fmt.Errorf("save user config: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("save user config: write: %w", err)
	}
	return nil
}
