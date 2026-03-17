package gatecheck

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// EngineConfig holds all gate engine configuration loaded from env vars.
type EngineConfig struct {
	Profile            Profile
	RepoPath           string
	StagedFiles        []string // nil = auto-detect from git
	MaxInfraBypass     int
	AllowRequiredSkip  bool
	GateTimeout        time.Duration
	MaxStagedFileBytes int64

	// Feature flags
	EnableFastTests         bool
	EnableNPMAudit          bool
	EnableDependencyDrift   bool
	EnforceForbiddenPaths   bool
	ForbiddenPathRegex      string
	EnforceLockfileSync     bool
	EnforceExecutablePolicy bool
	EnforceJSONDupKeys      bool
	EnforceSPDXForNewFiles  bool
	EnforceLicenseOnRelease bool

	// Required/optional check lists (comma-separated IDs; empty = use defaults)
	RequiredChecks []string
	OptionalChecks []string

	// Tool path overrides
	ShellcheckPath string
	SemgrepPath    string
	GitleaksPath   string
	RuffPath       string
	YamllintPath   string
}

// LoadEngineConfig reads all CODERO_* env vars and returns an EngineConfig.
func LoadEngineConfig() EngineConfig {
	profile := parseProfile(envString("CODERO_GATES_PROFILE", string(ProfilePortable)))

	cfg := EngineConfig{
		Profile:            profile,
		RepoPath:           envString("CODERO_REPO_PATH", ""),
		MaxInfraBypass:     envInt("CODERO_MAX_INFRA_BYPASS_GATES", 2, 0),
		AllowRequiredSkip:  envBool("CODERO_ALLOW_REQUIRED_SKIP"),
		GateTimeout:        time.Duration(envInt("CODERO_GATE_TIMEOUT", 120, 0)) * time.Second,
		MaxStagedFileBytes: int64(envInt("CODERO_MAX_STAGED_FILE_BYTES", 1048576, 0)), // 1 MiB default

		EnableFastTests:         envBool("CODERO_ENABLE_FAST_TESTS"),
		EnableNPMAudit:          envBool("CODERO_ENABLE_NPM_AUDIT"),
		EnableDependencyDrift:   envBool("CODERO_ENABLE_DEPENDENCY_DRIFT_REPORT"),
		EnforceForbiddenPaths:   envBool("CODERO_ENFORCE_FORBIDDEN_PATHS"),
		ForbiddenPathRegex:      envString("CODERO_FORBIDDEN_PATH_REGEX", ""),
		EnforceLockfileSync:     envBool("CODERO_ENFORCE_LOCKFILE_SYNC"),
		EnforceExecutablePolicy: envBool("CODERO_ENFORCE_EXECUTABLE_POLICY"),
		EnforceJSONDupKeys:      envBool("CODERO_ENFORCE_JSON_DUPLICATE_KEYS"),
		EnforceSPDXForNewFiles:  envBool("CODERO_ENFORCE_SPDX_FOR_NEW_FILES"),
		EnforceLicenseOnRelease: envBool("CODERO_ENFORCE_LICENSE_ON_RELEASE"),

		ShellcheckPath: envString("CODERO_TOOL_SHELLCHECK", "shellcheck"),
		SemgrepPath:    envString("CODERO_TOOL_SEMGREP", "semgrep"),
		GitleaksPath:   envString("CODERO_TOOL_GITLEAKS", "gitleaks"),
		RuffPath:       envString("CODERO_TOOL_RUFF", "ruff"),
		YamllintPath:   envString("CODERO_TOOL_YAMLLINT", "yamllint"),
	}

	if raw := os.Getenv("CODERO_REQUIRED_CHECKS"); raw != "" {
		cfg.RequiredChecks = splitCSV(raw)
	}
	if raw := os.Getenv("CODERO_OPTIONAL_CHECKS"); raw != "" {
		cfg.OptionalChecks = splitCSV(raw)
	}

	return cfg
}

func parseProfile(raw string) Profile {
	switch Profile(raw) {
	case ProfileStrict, ProfilePortable, ProfileOff:
		return Profile(raw)
	case Profile("fast"):
		return ProfilePortable
	default:
		return ProfilePortable
	}
}

func envString(key, def string) string {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	return v
}

func envInt(key string, def int, min int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return def
	}
	return n
}

func envBool(key string) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true
	case "0", "false":
		return false
	default:
		return false
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
