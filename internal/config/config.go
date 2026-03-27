// Package config loads and validates codero daemon configuration.
// The primary source of truth is a YAML file (Load). An env-only fallback
// (LoadEnv) is used when no config file path is provided.
package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Sentinel errors returned by Load.
var (
	// ErrConfigNotFound is returned when the config file does not exist.
	ErrConfigNotFound = errors.New("config file not found")

	// ErrConfigUnreadable is returned when the file exists but cannot be read.
	ErrConfigUnreadable = errors.New("config file unreadable")

	// ErrInvalidYAML is returned when the file contains malformed YAML or type mismatches.
	ErrInvalidYAML = errors.New("invalid YAML in config")

	// ErrUnknownFields is returned when the config file contains unrecognised keys.
	// Unknown fields are hard errors, not warnings.
	ErrUnknownFields = errors.New("unknown fields in config")

	// ErrMultipleDocuments is returned when the config file contains multiple YAML documents.
	ErrMultipleDocuments = errors.New("multiple YAML documents not allowed in config")

	// ErrMissingToken is returned when github_token is absent or empty.
	ErrMissingToken = errors.New("github_token is required")

	// ErrMissingRepos is returned when repos is absent or empty.
	ErrMissingRepos = errors.New("repos list is required and must be non-empty")

	// ErrMissingWebhookSecret is returned when webhook is enabled but no secret is set.
	ErrMissingWebhookSecret = errors.New("webhook.secret is required when webhook.enabled is true")

	// ErrInvalidObservabilityPort is returned when observability_port is outside 1..65535.
	ErrInvalidObservabilityPort = errors.New("observability_port must be between 1 and 65535")

	// ErrInvalidDashboardBasePath is returned when dashboard_base_path does not start with '/'.
	ErrInvalidDashboardBasePath = errors.New("dashboard_base_path must start with '/'")

	// ErrInvalidMergeMethod is returned when auto_merge.method is not a valid GitHub merge strategy.
	ErrInvalidMergeMethod = errors.New("auto_merge.method must be 'merge', 'squash', or 'rebase'")

	// ErrInvalidSweeperInterval is returned when sweeper.interval is not positive.
	ErrInvalidSweeperInterval = errors.New("sweeper.interval must be greater than 0")

	// ErrInvalidSessionTTL is returned when sweeper.session_ttl is not positive.
	ErrInvalidSessionTTL = errors.New("sweeper.session_ttl must be greater than 0")

	// ErrInvalidBranchHoldTTL is returned when sweeper.branch_hold_ttl is not positive.
	ErrInvalidBranchHoldTTL = errors.New("sweeper.branch_hold_ttl must be greater than 0")

	// ErrInvalidHandoffTTL is returned when sweeper.handoff_ttl is not positive.
	ErrInvalidHandoffTTL = errors.New("sweeper.handoff_ttl must be greater than 0")

	// ErrInvalidIssuePollInterval is returned when sweeper.issue_poll_interval is not positive.
	ErrInvalidIssuePollInterval = errors.New("sweeper.issue_poll_interval must be greater than 0")

	// ErrInvalidAPIServerAddr is returned when api_server.addr is empty or malformed.
	ErrInvalidAPIServerAddr = errors.New("api_server.addr must be a valid host:port")

	// ErrInvalidShutdownTimeout is returned when api_server.shutdown_timeout is not positive.
	ErrInvalidShutdownTimeout = errors.New("api_server.shutdown_timeout must be greater than 0")

	// ErrInvalidReadTimeout is returned when api_server.read_timeout is negative.
	ErrInvalidReadTimeout = errors.New("api_server.read_timeout must not be negative")

	// ErrInvalidWriteTimeout is returned when api_server.write_timeout is negative.
	ErrInvalidWriteTimeout = errors.New("api_server.write_timeout must not be negative")
)

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Addr           string `yaml:"addr"`
	Password       string `yaml:"password"`
	MaxRetries     int    `yaml:"max_retries"`
	RetryInterval  int    `yaml:"retry_interval"`  // seconds
	HealthInterval int    `yaml:"health_interval"` // seconds
}

// AutoMergeConfig controls automatic PR merging once merge_ready conditions are met.
type AutoMergeConfig struct {
	// Enabled activates automatic PR merging when a branch reaches merge_ready.
	// Default: false. Requires a GitHub token with write access to the repo.
	Enabled bool `yaml:"enabled"`
	// Method is the GitHub merge strategy: "merge", "squash", or "rebase".
	// Default: "squash".
	Method string `yaml:"method"`
}

// WebhookConfig holds webhook receiver settings.
// Webhooks are optional; polling-only mode is the default and fully functional
// even when webhook is disabled (WebhookEnabled = false).
type WebhookConfig struct {
	Enabled bool   `yaml:"enabled"` // default: false (polling-only)
	Port    int    `yaml:"port"`    // default: 9090
	Secret  string `yaml:"secret"`  // HMAC-SHA256 secret for signature verification
	Path    string `yaml:"path"`    // default: /webhook/github
}

// SweeperConfig holds sweeper settings for TTL expiry and cleanup.
type SweeperConfig struct {
	Interval          time.Duration `yaml:"interval"`            // default: 60s
	SessionTTL        time.Duration `yaml:"session_ttl"`         // default: 90s
	BranchHoldTTL     time.Duration `yaml:"branch_hold_ttl"`     // default: 72h
	HandoffTTL        time.Duration `yaml:"handoff_ttl"`         // default: 10m
	IssuePollInterval time.Duration `yaml:"issue_poll_interval"` // default: 10m
}

// APIServerConfig holds API server settings.
type APIServerConfig struct {
	Addr            string        `yaml:"addr"`             // default: :7700
	ReadTimeout     time.Duration `yaml:"read_timeout"`     // default: 30s
	WriteTimeout    time.Duration `yaml:"write_timeout"`    // default: 60s
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"` // default: 10s
}

const (
	// DefaultAPIServerAddr is the built-in default for api_server.addr.
	DefaultAPIServerAddr = ":7700"
	// DefaultAPIServerPort is the numeric default API bind port.
	DefaultAPIServerPort = 7700
	// DefaultAPIServerPortString is the string form of the default API bind port.
	DefaultAPIServerPortString = "7700"
	// DefaultAPIServerReadTimeout is the built-in default for api_server.read_timeout.
	DefaultAPIServerReadTimeout = 30 * time.Second
	// DefaultAPIServerWriteTimeout is the built-in default for api_server.write_timeout.
	DefaultAPIServerWriteTimeout = 60 * time.Second
	// DefaultAPIServerShutdownTimeout is the built-in default for api_server.shutdown_timeout.
	DefaultAPIServerShutdownTimeout = 10 * time.Second
)

// Config holds runtime configuration for the codero daemon.
type Config struct {
	GitHubToken       string          `yaml:"github_token"`
	Repos             []string        `yaml:"repos"`
	Redis             RedisConfig     `yaml:"redis"`
	Sweeper           SweeperConfig   `yaml:"sweeper"`
	APIServer         APIServerConfig `yaml:"api_server"`
	PIDFile           string          `yaml:"pid_file"`
	ReadyFile         string          `yaml:"ready_file"`
	LogLevel          string          `yaml:"log_level"`
	LogPath           string          `yaml:"log_path"`
	DBPath            string          `yaml:"db_path"`
	Webhook           WebhookConfig   `yaml:"webhook"`
	ObservabilityPort int             `yaml:"observability_port"`
	// ObservabilityHost controls the bind address for the HTTP server.
	// Default "" binds on all interfaces (0.0.0.0). Set to "127.0.0.1" to
	// restrict to loopback only in environments that require it.
	ObservabilityHost string `yaml:"observability_host"`
	// DashboardBasePath is the URL path prefix under which the dashboard SPA
	// and its API routes are served. Default: "/dashboard".
	// Useful for reverse-proxy deployments at e.g. "/codero/dashboard".
	DashboardBasePath string `yaml:"dashboard_base_path"`
	// DashboardPublicBaseURL overrides the base URL printed by "codero dashboard"
	// and returned by "codero ports". Useful when deployed behind a reverse proxy
	// where the external URL differs from the bind address.
	// Example: "https://ops.example.com/codero"
	DashboardPublicBaseURL string          `yaml:"dashboard_public_base_url"`
	AutoMerge              AutoMergeConfig `yaml:"auto_merge"`
}

// Load reads config from a YAML file at path and applies env overrides.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrConfigNotFound, path)
		}
		return nil, fmt.Errorf("%w: %s: %w", ErrConfigUnreadable, path, err)
	}
	defer f.Close()

	c := defaults()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil {
		return nil, classifyYAMLError(err)
	}

	var dummy any
	if err := dec.Decode(&dummy); err == nil {
		return nil, ErrMultipleDocuments
	} else if !errors.Is(err, io.EOF) {
		return nil, classifyYAMLError(err)
	}

	applyEnvOverrides(c)
	normalizeCompat(c)

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// LoadEnv builds a Config from environment variables only, using built-in
// defaults for any unset variable. Unlike Load, this loader populates
// github_token from GITHUB_TOKEN and repos from CODERO_REPOS (comma-separated)
// so daemon fallback can succeed without a config file.
func LoadEnv() *Config {
	c := defaults()
	applyEnvOverrides(c)
	normalizeCompat(c)

	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		c.GitHubToken = v
	}
	if v := os.Getenv("CODERO_REPOS"); v != "" {
		for _, repo := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(repo); trimmed != "" {
				c.Repos = append(c.Repos, trimmed)
			}
		}
	}
	return c
}

// ParseAPIServerAddr validates and splits api_server.addr into host and port.
// An empty host is allowed and means bind on all interfaces.
func ParseAPIServerAddr(addr string) (string, int, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0, ErrInvalidAPIServerAddr
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, ErrInvalidAPIServerAddr
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, ErrInvalidAPIServerAddr
	}

	return host, port, nil
}

// defaults returns a Config pre-populated with safe built-in values.
func defaults() *Config {
	c := &Config{
		Redis: RedisConfig{
			Addr:           "localhost:6379",
			Password:       "",
			MaxRetries:     3,
			RetryInterval:  1,
			HealthInterval: 30,
		},
		Sweeper: SweeperConfig{
			Interval:          60 * time.Second,
			SessionTTL:        90 * time.Second,
			BranchHoldTTL:     72 * time.Hour,
			HandoffTTL:        10 * time.Minute,
			IssuePollInterval: 10 * time.Minute,
		},
		APIServer: APIServerConfig{
			Addr:            DefaultAPIServerAddr,
			ReadTimeout:     DefaultAPIServerReadTimeout,
			WriteTimeout:    DefaultAPIServerWriteTimeout,
			ShutdownTimeout: DefaultAPIServerShutdownTimeout,
		},
		PIDFile:  "/var/run/codero/codero.pid",
		LogLevel: "info",
		LogPath:  "",
		DBPath:   "/var/lib/codero/codero.db",
		Webhook: WebhookConfig{
			Enabled: false, // polling-only mode by default
			Port:    9090,
			Path:    "/webhook/github",
		},
		ObservabilityPort: 8080,
		ObservabilityHost: "",
		DashboardBasePath: "/dashboard",
		AutoMerge: AutoMergeConfig{
			Enabled: false,
			Method:  "squash",
		},
	}
	// Derive ReadyFile from PIDFile directory to keep sentinel paths colocated.
	c.ReadyFile = filepath.Join(filepath.Dir(c.PIDFile), "codero.ready")
	return c
}

// applyEnvOverrides overwrites runtime fields from environment variables.
// If CODERO_PID_FILE is set but CODERO_READY_FILE is not, ReadyFile is
// derived from the effective PID path to keep sentinel paths colocated.
func applyEnvOverrides(c *Config) {
	if v := os.Getenv("CODERO_REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := os.Getenv("CODERO_REDIS_PASS"); v != "" {
		c.Redis.Password = v
	}
	if v := os.Getenv("CODERO_REDIS_MAX_RETRIES"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 {
			c.Redis.MaxRetries = i
		}
	}
	if v := os.Getenv("CODERO_REDIS_RETRY_INTERVAL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 {
			c.Redis.RetryInterval = i
		}
	}
	if v := os.Getenv("CODERO_REDIS_HEALTH_INTERVAL"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 {
			c.Redis.HealthInterval = i
		}
	}
	pidFileEnvSet := false
	if v := os.Getenv("CODERO_PID_FILE"); v != "" {
		c.PIDFile = v
		pidFileEnvSet = true
	}
	readyFileEnvSet := false
	if v := os.Getenv("CODERO_READY_FILE"); v != "" {
		c.ReadyFile = v
		readyFileEnvSet = true
	}
	// If PID file was set via env but ready file was not,
	// derive ready sentinel from the new PID location.
	if pidFileEnvSet && !readyFileEnvSet {
		c.ReadyFile = filepath.Join(filepath.Dir(c.PIDFile), "codero.ready")
	}
	if v := os.Getenv("CODERO_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("CODERO_LOG_PATH"); v != "" {
		c.LogPath = v
	}
	if v := os.Getenv("CODERO_DB_PATH"); v != "" {
		c.DBPath = v
	}
	// Webhook env overrides.
	// CODERO_WEBHOOK_ENABLED=true enables the webhook receiver.
	if v := os.Getenv("CODERO_WEBHOOK_ENABLED"); v == "true" || v == "1" {
		c.Webhook.Enabled = true
	}
	if v := os.Getenv("CODERO_WEBHOOK_SECRET"); v != "" {
		c.Webhook.Secret = v
	}
	// CODERO_OBSERVABILITY_PORT overrides the HTTP port for the observability server.
	if v := os.Getenv("CODERO_OBSERVABILITY_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			// Force Validate() to fail fast instead of silently using default.
			c.ObservabilityPort = 0
		} else {
			c.ObservabilityPort = p
		}
	}
	// CODERO_OBSERVABILITY_HOST overrides the bind host for the observability server.
	if v := os.Getenv("CODERO_OBSERVABILITY_HOST"); v != "" {
		c.ObservabilityHost = v
	}
	// CODERO_DASHBOARD_BASE_PATH overrides the URL prefix for the dashboard SPA.
	if v := os.Getenv("CODERO_DASHBOARD_BASE_PATH"); v != "" {
		c.DashboardBasePath = v
	}
	// CODERO_DASHBOARD_PUBLIC_BASE_URL overrides the externally visible dashboard URL.
	if v := os.Getenv("CODERO_DASHBOARD_PUBLIC_BASE_URL"); v != "" {
		c.DashboardPublicBaseURL = v
	}
	// CODERO_AUTO_MERGE_ENABLED overrides auto-merge in either direction so
	// operators can disable it via env even when the YAML has enabled: true.
	if v := os.Getenv("CODERO_AUTO_MERGE_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.AutoMerge.Enabled = enabled
		}
	}
	// CODERO_AUTO_MERGE_METHOD overrides the GitHub merge strategy ("merge", "squash", "rebase").
	if v := os.Getenv("CODERO_AUTO_MERGE_METHOD"); v != "" {
		c.AutoMerge.Method = v
	}
	// Sweeper env overrides (§6.6). Use parsePositiveDuration to reject "0s"
	// which would override defaults and then fail in Validate().
	if v := os.Getenv("CODERO_SWEEPER_INTERVAL"); v != "" {
		if d, ok := parsePositiveDuration(v); ok {
			c.Sweeper.Interval = d
		}
	}
	if v := firstNonEmptyEnv("CODERO_SWEEPER_SESSION_TTL", "CODERO_SESSION_TTL"); v != "" {
		if d, ok := parsePositiveDuration(v); ok {
			c.Sweeper.SessionTTL = d
		}
	}
	if v := firstNonEmptyEnv("CODERO_SWEEPER_BRANCH_HOLD_TTL", "CODERO_BRANCH_HOLD_TTL"); v != "" {
		if d, ok := parsePositiveDuration(v); ok {
			c.Sweeper.BranchHoldTTL = d
		}
	}
	if v := firstNonEmptyEnv("CODERO_SWEEPER_HANDOFF_TTL", "CODERO_HANDOFF_TTL"); v != "" {
		if d, ok := parsePositiveDuration(v); ok {
			c.Sweeper.HandoffTTL = d
		}
	}
	if v := firstNonEmptyEnv("CODERO_SWEEPER_ISSUE_POLL_INTERVAL", "CODERO_ISSUE_POLL_INTERVAL"); v != "" {
		if d, ok := parsePositiveDuration(v); ok {
			c.Sweeper.IssuePollInterval = d
		}
	}
	// API server env overrides (§6.3).
	if v := os.Getenv("CODERO_API_ADDR"); v != "" {
		c.APIServer.Addr = v
	}
	if v := os.Getenv("CODERO_API_READ_TIMEOUT"); v != "" {
		if d, ok := parseNonNegativeDuration(v); ok {
			c.APIServer.ReadTimeout = d
		}
	}
	if v := os.Getenv("CODERO_API_WRITE_TIMEOUT"); v != "" {
		if d, ok := parseNonNegativeDuration(v); ok {
			c.APIServer.WriteTimeout = d
		}
	}
	if v := os.Getenv("CODERO_API_SHUTDOWN_TIMEOUT"); v != "" {
		if d, ok := parsePositiveDuration(v); ok {
			c.APIServer.ShutdownTimeout = d
		}
	}
}

// normalizeCompat backfills APIServer.Addr from legacy ObservabilityHost/ObservabilityPort
// when api_server.addr was not explicitly configured (still at built-in default).
// This preserves backward compatibility for operators using the older config fields.
// Explicit api_server.addr (any non-default value) always takes precedence.
func normalizeCompat(c *Config) {
	if c.APIServer.Addr != DefaultAPIServerAddr {
		return // api_server.addr was explicitly set, no backfill
	}
	if c.ObservabilityPort >= 1 && c.ObservabilityPort <= 65535 {
		c.APIServer.Addr = net.JoinHostPort(c.ObservabilityHost, strconv.Itoa(c.ObservabilityPort))
	}
}

// Validate checks that required fields are present and non-empty.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.GitHubToken) == "" {
		return ErrMissingToken
	}
	if len(c.Repos) == 0 {
		return ErrMissingRepos
	}
	for _, repo := range c.Repos {
		if strings.TrimSpace(repo) == "" {
			return ErrMissingRepos
		}
	}
	if c.Webhook.Enabled && strings.TrimSpace(c.Webhook.Secret) == "" {
		return ErrMissingWebhookSecret
	}
	if c.ObservabilityPort < 1 || c.ObservabilityPort > 65535 {
		return ErrInvalidObservabilityPort
	}
	if c.DashboardBasePath != "" && !strings.HasPrefix(c.DashboardBasePath, "/") {
		return ErrInvalidDashboardBasePath
	}
	if _, _, err := ParseAPIServerAddr(c.APIServer.Addr); err != nil {
		return err
	}
	if c.APIServer.ShutdownTimeout <= 0 {
		return ErrInvalidShutdownTimeout
	}
	if c.APIServer.ReadTimeout < 0 {
		return ErrInvalidReadTimeout
	}
	if c.APIServer.WriteTimeout < 0 {
		return ErrInvalidWriteTimeout
	}
	if c.Sweeper.Interval <= 0 {
		return ErrInvalidSweeperInterval
	}
	if c.Sweeper.SessionTTL <= 0 {
		return ErrInvalidSessionTTL
	}
	if c.Sweeper.BranchHoldTTL <= 0 {
		return ErrInvalidBranchHoldTTL
	}
	if c.Sweeper.HandoffTTL <= 0 {
		return ErrInvalidHandoffTTL
	}
	if c.Sweeper.IssuePollInterval <= 0 {
		return ErrInvalidIssuePollInterval
	}
	switch c.AutoMerge.Method {
	case "merge", "squash", "rebase":
		// valid
	default:
		return ErrInvalidMergeMethod
	}
	return nil
}

// classifyYAMLError maps a yaml decoder error to the appropriate sentinel.
// It wraps the original error to preserve the error chain for callers.
func classifyYAMLError(err error) error {
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		for _, msg := range typeErr.Errors {
			if strings.Contains(msg, "not found in type") {
				return fmt.Errorf("%w: %w", ErrUnknownFields, typeErr)
			}
		}
		return fmt.Errorf("%w: %w", ErrInvalidYAML, typeErr)
	}
	return fmt.Errorf("%w: %w", ErrInvalidYAML, err)
}

func parseNonNegativeDuration(v string) (time.Duration, bool) {
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, false
	}
	return d, true
}

// parsePositiveDuration parses a duration that must be strictly > 0.
// Used for sweeper intervals and shutdown timeout where zero is invalid.
func parsePositiveDuration(v string) (time.Duration, bool) {
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

// LoadEnvFile loads key=value pairs from $HOME/.codero/config.env and sets them
// as environment variables (without overwriting existing values). This provides
// a machine-global config source per Gate Config v1.
func LoadEnvFile() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // No home dir, skip silently
	}
	path := filepath.Join(home, ".codero", "config.env")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, that's fine
		}
		return fmt.Errorf("read config.env: %w", err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("config.env:%d: malformed line (missing '='): %q", i+1, line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("config.env:%d: empty key", i+1)
		}
		value = strings.TrimSpace(value)
		// Don't overwrite existing env — shell env takes precedence
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	return nil
}

// DriftEntry records a config key where the env value differs from the file value.
// EnvValue is redacted for sensitive keys to avoid leaking secrets in logs or serialization.
type DriftEntry struct {
	Key      string
	EnvValue string
	YAMLKey  string
}

// sensitiveKeys are env keys whose values must be redacted in drift reports.
var sensitiveKeys = map[string]bool{
	"GITHUB_TOKEN":          true,
	"CODERO_REDIS_PASS":     true,
	"CODERO_WEBHOOK_SECRET": true,
}

// DetectDrift compares YAML-only config values against current environment variables
// and returns any keys where the env would override the file value.
// The caller should pass a Config loaded from YAML *before* applyEnvOverrides runs,
// or use DetectDriftFromPath which handles this automatically.
func DetectDrift(c *Config) []DriftEntry {
	var drifts []DriftEntry
	check := func(envKey, yamlKey, fileVal string) {
		envVal := os.Getenv(envKey)
		if envVal != "" && envVal != fileVal && fileVal != "" {
			displayVal := envVal
			if sensitiveKeys[envKey] {
				displayVal = "(redacted)"
			}
			drifts = append(drifts, DriftEntry{Key: envKey, EnvValue: displayVal, YAMLKey: yamlKey})
		}
	}
	check("GITHUB_TOKEN", "github_token", c.GitHubToken)
	check("CODERO_REDIS_ADDR", "redis.addr", c.Redis.Addr)
	check("CODERO_REDIS_PASS", "redis.password", c.Redis.Password)
	check("CODERO_OBSERVABILITY_PORT", "observability_port", strconv.Itoa(c.ObservabilityPort))
	check("CODERO_DASHBOARD_BASE_PATH", "dashboard_base_path", c.DashboardBasePath)
	if c.APIServer.Addr != "" {
		check("CODERO_API_ADDR", "api_server.addr", c.APIServer.Addr)
	}
	return drifts
}

// DetectDriftFromPath loads YAML without env overrides and compares against current env.
func DetectDriftFromPath(path string) ([]DriftEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	c := defaults()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil {
		return nil, fmt.Errorf("decode YAML for drift detection: %w", err)
	}
	// Compare raw YAML values (no env overrides applied) against current env.
	return DetectDrift(c), nil
}
