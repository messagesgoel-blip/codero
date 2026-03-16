package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// defaultGatePipeline is the canonical gate pipeline configuration.
// The enabled/blocks_commit fields may be overridden by saved settings.
var defaultGatePipeline = []GateConfig{
	{Name: "gitleaks", Enabled: true, BlocksCommit: true, TimeoutSec: 30, Provider: "built-in"},
	{Name: "ruff", Enabled: true, BlocksCommit: false, TimeoutSec: 60, Provider: "built-in"},
	{Name: "semgrep", Enabled: true, BlocksCommit: true, TimeoutSec: 120, Provider: "Semgrep Cloud"},
	{Name: "llm-review", Enabled: true, BlocksCommit: true, TimeoutSec: 180, Provider: "LiteLLM"},
	{Name: "coderabbit", Enabled: true, BlocksCommit: false, TimeoutSec: 120, Provider: "CodeRabbit"},
	{Name: "pr-agent", Enabled: true, BlocksCommit: false, TimeoutSec: 60, Provider: "PR Agent"},
}

// defaultIntegrations is the baseline integration state.
// Connected status may be overridden by saved settings.
var defaultIntegrations = []IntegrationCard{
	{ID: "coderabbit", Name: "CodeRabbit", Desc: "AI code review on PRs", Connected: false},
	{ID: "pr-agent", Name: "PR Agent", Desc: "Automated PR workflows", Connected: false},
	{ID: "gh-actions", Name: "GitHub Actions", Desc: "CI/CD pipeline triggers", Connected: false},
	{ID: "gitlab-ci", Name: "GitLab CI", Desc: "GitLab pipeline integration", Connected: false},
	{ID: "semgrep-cloud", Name: "Semgrep Cloud", Desc: "Cloud rule management", Connected: false},
}

// PersistedSettings is the JSON schema written to the settings file.
// It is also the return type of SettingsStore.Load so tests and callers
// can access the fields without going through SettingsResponse.
type PersistedSettings struct {
	Integrations []IntegrationCard `json:"integrations"`
	GatePipeline []GateConfig      `json:"gate_pipeline"`
}

// SettingsStore provides thread-safe read/write access to dashboard settings.
// Settings are persisted as a JSON file alongside the state database.
type SettingsStore struct {
	path string
	mu   sync.RWMutex
}

// NewSettingsStore creates a SettingsStore backed by a file at dataDir/dashboard-settings.json.
func NewSettingsStore(dataDir string) *SettingsStore {
	return &SettingsStore{
		path: filepath.Join(dataDir, "dashboard-settings.json"),
	}
}

// Load reads current settings, merging persisted overrides onto the defaults.
func (s *SettingsStore) Load() (*PersistedSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ps := &PersistedSettings{
		Integrations: cloneIntegrations(defaultIntegrations),
		GatePipeline: cloneGatePipeline(defaultGatePipeline),
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return ps, nil
		}
		return nil, fmt.Errorf("settings: read: %w", err)
	}

	var saved PersistedSettings
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("settings: parse: %w", err)
	}

	// Merge saved integrations.
	if len(saved.Integrations) > 0 {
		savedMap := make(map[string]IntegrationCard, len(saved.Integrations))
		for _, c := range saved.Integrations {
			savedMap[c.ID] = c
		}
		for i, d := range ps.Integrations {
			if ov, ok := savedMap[d.ID]; ok {
				ps.Integrations[i].Connected = ov.Connected
			}
		}
	}

	// Merge saved gate pipeline.
	if len(saved.GatePipeline) > 0 {
		savedMap := make(map[string]GateConfig, len(saved.GatePipeline))
		for _, g := range saved.GatePipeline {
			savedMap[g.Name] = g
		}
		for i, d := range ps.GatePipeline {
			if ov, ok := savedMap[d.Name]; ok {
				ps.GatePipeline[i].Enabled = ov.Enabled
				ps.GatePipeline[i].BlocksCommit = ov.BlocksCommit
			}
		}
	}

	return ps, nil
}

// Save validates and persists new settings atomically.
func (s *SettingsStore) Save(req *SettingsUpdateRequest) error {
	if err := validateSettingsUpdate(req); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ps := &PersistedSettings{
		Integrations: req.Integrations,
		GatePipeline: req.GatePipeline,
	}

	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: marshal: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("settings: mkdir: %w", err)
	}

	// Write atomically via temp file + rename.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return fmt.Errorf("settings: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("settings: rename: %w", err)
	}
	return nil
}

// validateSettingsUpdate returns an error if the request contains invalid data.
func validateSettingsUpdate(req *SettingsUpdateRequest) error {
	if req == nil {
		return fmt.Errorf("request body required")
	}
	for _, g := range req.GatePipeline {
		if g.Name == "" {
			return fmt.Errorf("gate pipeline: gate name must not be empty")
		}
		if g.TimeoutSec < 0 || g.TimeoutSec > 3600 {
			return fmt.Errorf("gate pipeline: timeout_sec for %q must be 0–3600", g.Name)
		}
	}
	for _, c := range req.Integrations {
		if c.ID == "" {
			return fmt.Errorf("integrations: id must not be empty")
		}
	}
	return nil
}

func cloneIntegrations(src []IntegrationCard) []IntegrationCard {
	out := make([]IntegrationCard, len(src))
	copy(out, src)
	return out
}

func cloneGatePipeline(src []GateConfig) []GateConfig {
	out := make([]GateConfig, len(src))
	copy(out, src)
	return out
}
