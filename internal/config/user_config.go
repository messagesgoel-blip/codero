package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// UserConfig is the per-user config at ~/.codero/config.yaml.
// It is separate from the daemon Config and holds agent-side settings.
type UserConfig struct {
	Version    int                      `yaml:"version"`
	DaemonAddr string                   `yaml:"daemon_addr"`
	SetupAt    time.Time                `yaml:"setup_at,omitempty"`
	Wrappers   map[string]WrapperConfig `yaml:"wrappers,omitempty"`
}

// WrapperConfig records a wrapped agent binary discovered during setup.
type WrapperConfig struct {
	RealBinary  string    `yaml:"real_binary"`
	InstalledAt time.Time `yaml:"installed_at,omitempty"`
}

// UserConfigDir returns ~/.codero, creating it if needed.
func UserConfigDir() (string, error) {
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
