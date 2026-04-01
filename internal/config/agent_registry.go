package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	loglib "github.com/codero/codero/internal/log"
)

const DefaultAgentRegistryScanInterval = 24 * time.Hour

// AgentRegistry is the cached Codero-side inventory of known agent wrappers and
// aliases. It is persisted in the per-user config and refreshed periodically by
// the daemon, rather than scanning shims on every dashboard request.
type AgentRegistry struct {
	LastScan time.Time                  `yaml:"last_scan,omitempty" json:"last_scan,omitempty"`
	Agents   map[string]RegisteredAgent `yaml:"agents,omitempty" json:"agents,omitempty"`
}

// RegisteredAgent is one durable registry entry for a known agent.
type RegisteredAgent struct {
	AgentID           string            `yaml:"agent_id" json:"agent_id"`
	AgentKind         string            `yaml:"agent_kind,omitempty" json:"agent_kind,omitempty"`
	DisplayName       string            `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	PrimaryAlias      string            `yaml:"primary_alias,omitempty" json:"primary_alias,omitempty"`
	Aliases           []string          `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	ShimName          string            `yaml:"shim_name,omitempty" json:"shim_name,omitempty"`
	ShimNames         []string          `yaml:"shim_names,omitempty" json:"shim_names,omitempty"`
	RealBinary        string            `yaml:"real_binary,omitempty" json:"real_binary,omitempty"`
	AuthMode          string            `yaml:"auth_mode,omitempty" json:"auth_mode,omitempty"`
	HomeStrategy      string            `yaml:"home_strategy,omitempty" json:"home_strategy,omitempty"`
	HomeDir           string            `yaml:"home_dir,omitempty" json:"home_dir,omitempty"`
	ConfigStrategy    string            `yaml:"config_strategy,omitempty" json:"config_strategy,omitempty"`
	ConfigPath        string            `yaml:"config_path,omitempty" json:"config_path,omitempty"`
	PermissionProfile string            `yaml:"permission_profile,omitempty" json:"permission_profile,omitempty"`
	DefaultArgs       []string          `yaml:"default_args,omitempty" json:"default_args,omitempty"`
	Installed         bool              `yaml:"installed" json:"installed"`
	Disabled          bool              `yaml:"disabled" json:"disabled"`
	EnvVars           map[string]string `yaml:"env_vars,omitempty" json:"env_vars,omitempty"`
	FirstSeenAt       time.Time         `yaml:"first_seen_at,omitempty" json:"first_seen_at,omitempty"`
	LastScanAt        time.Time         `yaml:"last_scan_at,omitempty" json:"last_scan_at,omitempty"`
	Source            string            `yaml:"source,omitempty" json:"source,omitempty"`
}

var knownAgentKindDisplayNames = map[string]string{
	AgentKindClaude:   "Claude Code",
	AgentKindCodex:    "Codex CLI",
	AgentKindOpenCode: "OpenCode",
	AgentKindCopilot:  "GitHub Copilot",
	AgentKindGemini:   "Gemini CLI",
}

// RegistryStale reports whether the persisted agent registry should be refreshed.
func (uc *UserConfig) RegistryStale(now time.Time, maxAge time.Duration) bool {
	if uc == nil {
		return true
	}
	if maxAge <= 0 {
		maxAge = DefaultAgentRegistryScanInterval
	}
	if uc.Registry.LastScan.IsZero() {
		return true
	}
	return now.Sub(uc.Registry.LastScan) >= maxAge
}

// RegisteredAgents returns the registry as a stable, agent_id-sorted slice.
func (uc *UserConfig) RegisteredAgents() []RegisteredAgent {
	if uc == nil || len(uc.Registry.Agents) == 0 {
		return nil
	}
	out := make([]RegisteredAgent, 0, len(uc.Registry.Agents))
	for _, agent := range uc.Registry.Agents {
		out = append(out, agent)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AgentID < out[j].AgentID
	})
	return out
}

// RefreshAgentRegistry rescans Codero-managed wrappers and shims and updates the
// persisted registry in memory. Call Save() afterwards to persist it.
func (uc *UserConfig) RefreshAgentRegistry(now time.Time) ([]RegisteredAgent, error) {
	if uc == nil {
		uc = &UserConfig{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	discovered, err := DiscoverAgents(uc)
	if err != nil {
		return nil, fmt.Errorf("refresh agent registry: discover agents: %w", err)
	}

	prev := uc.Registry.Agents
	next := make(map[string]RegisteredAgent)

	merge := func(agentID string, shimNames []string, realBinary, source string, installed bool, discoveredEnvVars map[string]string, wrapper WrapperConfig) {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			return
		}

		entry, ok := next[agentID]
		if !ok {
			entry = prev[agentID]
		}
		prior := entry
		if entry.AgentID == "" {
			entry.AgentID = agentID
		}
		if kind := NormalizeAgentKind(wrapper.AgentKind); kind != "" {
			entry.AgentKind = kind
		}
		if entry.AgentKind == "" {
			entry.AgentKind = InferAgentKind(agentID, firstNonEmpty(realBinary, entry.RealBinary))
		}
		if entry.FirstSeenAt.IsZero() {
			entry.FirstSeenAt = now
		}
		if wrapper.DisplayName != "" {
			entry.DisplayName = wrapper.DisplayName
		}
		if entry.DisplayName == "" {
			entry.DisplayName = displayNameForProfile(entry.AgentKind, agentID)
		}
		if realBinary != "" {
			entry.RealBinary = realBinary
		}
		entry.AuthMode = firstNonEmpty(wrapper.AuthMode, entry.AuthMode)
		entry.HomeStrategy = firstNonEmpty(wrapper.HomeStrategy, entry.HomeStrategy)
		entry.HomeDir = firstNonEmpty(wrapper.HomeDir, entry.HomeDir)
		entry.ConfigStrategy = firstNonEmpty(wrapper.ConfigStrategy, entry.ConfigStrategy)
		entry.ConfigPath = firstNonEmpty(wrapper.ConfigPath, entry.ConfigPath)
		entry.PermissionProfile = firstNonEmpty(wrapper.PermissionProfile, entry.PermissionProfile)
		if wrapper.DefaultArgs != nil {
			entry.DefaultArgs = copyStringSlice(wrapper.DefaultArgs)
		}
		entry.Installed = installed
		entry.Disabled = uc.IsTrackingDisabled(agentID)
		if wrapper.EnvVars != nil {
			entry.EnvVars = copyEnvVars(wrapper.EnvVars)
		} else if discoveredEnvVars != nil {
			entry.EnvVars = copyEnvVars(discoveredEnvVars)
		}
		entry.LastScanAt = now
		if source != "" {
			entry.Source = source
		}

		if shimNames != nil {
			entry.ShimNames = uniqueAliases(shimNames)
		} else {
			entry.ShimNames = uniqueAliases(entry.ShimNames)
		}
		entry.ShimName = choosePrimaryShimName(agentID, entry.ShimNames)

		combinedAliases := storedWrapperAliases(prior, agentID)
		if wrapper.Aliases != nil {
			combinedAliases = copyStringSlice(wrapper.Aliases)
		}
		combinedAliases = append(combinedAliases, agentID)
		combinedAliases = append(combinedAliases, entry.ShimNames...)
		if entry.RealBinary != "" {
			combinedAliases = append(combinedAliases, filepath.Base(entry.RealBinary))
		}
		entry.Aliases = uniqueAliases(combinedAliases)
		entry.PrimaryAlias = choosePrimaryAlias(agentID, entry.ShimNames, entry.Aliases, entry.RealBinary)

		next[agentID] = entry
	}

	type discoveredAggregate struct {
		shimNames  []string
		realBinary string
		installed  bool
		envVars    map[string]string
	}
	discoveredByID := make(map[string]discoveredAggregate)
	for _, info := range discovered {
		agg := discoveredByID[info.AgentID]
		agg.shimNames = append(agg.shimNames, info.ShimName)
		if agg.realBinary == "" && info.RealBinary != "" {
			agg.realBinary = info.RealBinary
		}
		agg.installed = agg.installed || info.Installed
		if len(agg.envVars) == 0 && len(info.EnvVars) > 0 {
			agg.envVars = copyEnvVars(info.EnvVars)
		}
		discoveredByID[info.AgentID] = agg
	}

	for agentID, info := range discoveredByID {
		wrapper := WrapperConfig{}
		if w, ok := uc.Wrappers[agentID]; ok {
			wrapper = w
		}
		merge(agentID, info.shimNames, info.realBinary, "shim_scan", info.installed, info.envVars, wrapper)
	}

	for agentID, wrapper := range uc.Wrappers {
		if _, ok := discoveredByID[agentID]; ok {
			continue
		}
		installed := false
		if strings.TrimSpace(wrapper.RealBinary) != "" {
			if _, err := os.Stat(wrapper.RealBinary); err == nil {
				installed = true
			}
		}
		merge(agentID, []string{}, wrapper.RealBinary, "wrapper_config", installed, nil, wrapper)
	}

	uc.Registry.LastScan = now
	uc.Registry.Agents = next
	return uc.RegisteredAgents(), nil
}

// LoadUserConfigWithRegistry loads the user config and refreshes the cached
// registry when it is stale. If refresh fails but a cached registry already
// exists, the cached registry is returned.
func LoadUserConfigWithRegistry(maxAge time.Duration) (*UserConfig, []RegisteredAgent, error) {
	return loadUserConfigWithRegistry(maxAge, false)
}

// LoadUserConfigWithFreshRegistry forces an immediate registry refresh. Use
// this for user-facing reads where agent availability should reflect the
// current machine state, even if the cached registry is still fresh.
func LoadUserConfigWithFreshRegistry() (*UserConfig, []RegisteredAgent, error) {
	return loadUserConfigWithRegistry(DefaultAgentRegistryScanInterval, true)
}

func loadUserConfigWithRegistry(maxAge time.Duration, forceRefresh bool) (*UserConfig, []RegisteredAgent, error) {
	ConfigMu.Lock()
	defer ConfigMu.Unlock()

	uc, err := LoadUserConfig()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	if !forceRefresh && !uc.RegistryStale(now, maxAge) {
		return uc, uc.RegisteredAgents(), nil
	}

	agents, refreshErr := uc.RefreshAgentRegistry(now)
	if refreshErr != nil {
		cached := uc.RegisteredAgents()
		if len(cached) > 0 {
			return uc, cached, nil
		}
		return nil, nil, refreshErr
	}
	if err := uc.Save(); err != nil {
		loglib.Warn("config: fresh agent registry save failed",
			loglib.FieldComponent, "config",
			"error", err,
		)
		return uc, agents, nil
	}
	return uc, agents, nil
}

func displayNameForProfile(agentKind, agentID string) string {
	agentKind = NormalizeAgentKind(agentKind)
	if agentKind == "" {
		agentKind = InferAgentKind(agentID, "")
	}
	if v, ok := knownAgentKindDisplayNames[agentKind]; ok {
		if strings.TrimSpace(agentID) == "" || agentID == agentKind {
			return v
		}
		return v + " (" + agentID + ")"
	}
	parts := strings.FieldsFunc(agentID, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	if len(parts) == 0 {
		return agentID
	}
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func copyEnvVars(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func storedWrapperAliases(entry RegisteredAgent, agentID string) []string {
	if len(entry.Aliases) == 0 {
		return nil
	}

	skip := make(map[string]struct{}, len(entry.ShimNames)+2)
	if agentID = strings.TrimSpace(agentID); agentID != "" {
		skip[agentID] = struct{}{}
	}
	for _, shimName := range entry.ShimNames {
		if shimName = strings.TrimSpace(shimName); shimName != "" {
			skip[shimName] = struct{}{}
		}
	}
	if base := strings.TrimSpace(filepath.Base(entry.RealBinary)); base != "" && base != "." {
		skip[base] = struct{}{}
	}

	aliases := make([]string, 0, len(entry.Aliases))
	for _, alias := range entry.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if _, ok := skip[alias]; ok {
			continue
		}
		aliases = append(aliases, alias)
	}
	return aliases
}

func uniqueAliases(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func choosePrimaryShimName(agentID string, shimNames []string) string {
	if len(shimNames) == 0 {
		return ""
	}
	for _, shimName := range shimNames {
		if shimName == agentID {
			return shimName
		}
	}
	return shimNames[0]
}

func choosePrimaryAlias(agentID string, shimNames, aliases []string, realBinary string) string {
	for _, shimName := range shimNames {
		if shimName == agentID {
			return shimName
		}
	}
	for _, alias := range aliases {
		if alias == agentID {
			return alias
		}
	}
	return firstNonEmpty(choosePrimaryShimName(agentID, shimNames), filepath.Base(realBinary), agentID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
