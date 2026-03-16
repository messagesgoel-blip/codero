// Package config loads and validates codero daemon configuration.
// The primary source of truth is a YAML file (Load). An env-only fallback
// (LoadEnv) is used when no config file path is provided.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

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
)

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
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

// Config holds runtime configuration for the codero daemon.
type Config struct {
	GitHubToken       string        `yaml:"github_token"`
	Repos             []string      `yaml:"repos"`
	Redis             RedisConfig   `yaml:"redis"`
	PIDFile           string        `yaml:"pid_file"`
	LogLevel          string        `yaml:"log_level"`
	LogPath           string        `yaml:"log_path"`
	DBPath            string        `yaml:"db_path"`
	Webhook           WebhookConfig `yaml:"webhook"`
	ObservabilityPort int           `yaml:"observability_port"`
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

// defaults returns a Config pre-populated with safe built-in values.
func defaults() *Config {
	return &Config{
		Redis: RedisConfig{
			Addr:     "localhost:6379",
			Password: "",
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
}

// applyEnvOverrides overwrites runtime fields from environment variables.
func applyEnvOverrides(c *Config) {
	if v := os.Getenv("CODERO_REDIS_ADDR"); v != "" {
		c.Redis.Addr = v
	}
	if v := os.Getenv("CODERO_REDIS_PASS"); v != "" {
		c.Redis.Password = v
	}
	if v := os.Getenv("CODERO_PID_FILE"); v != "" {
		c.PIDFile = v
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
	// CODERO_AUTO_MERGE_ENABLED=true activates automatic PR merging on merge_ready.
	if v := os.Getenv("CODERO_AUTO_MERGE_ENABLED"); v == "true" || v == "1" {
		c.AutoMerge.Enabled = true
	}
	// CODERO_AUTO_MERGE_METHOD overrides the GitHub merge strategy ("merge", "squash", "rebase").
	if v := os.Getenv("CODERO_AUTO_MERGE_METHOD"); v != "" {
		c.AutoMerge.Method = v
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
	if c.AutoMerge.Enabled {
		switch c.AutoMerge.Method {
		case "merge", "squash", "rebase":
			// valid
		default:
			return ErrInvalidMergeMethod
		}
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
