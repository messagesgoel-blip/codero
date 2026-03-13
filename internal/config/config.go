// Package config loads and validates codero daemon configuration.
// The primary source of truth is a YAML file (Load). An env-only fallback
// (LoadEnv) is used when no config file path is provided.
package config

import (
	"errors"
	"fmt"
	"os"
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

	// ErrMissingToken is returned when github_token is absent or empty.
	ErrMissingToken = errors.New("github_token is required")

	// ErrMissingRepos is returned when repos is absent or empty.
	ErrMissingRepos = errors.New("repos list is required and must be non-empty")
)

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
}

// Config holds runtime configuration for the codero daemon.
type Config struct {
	GitHubToken string      `yaml:"github_token"`
	Repos       []string    `yaml:"repos"`
	Redis       RedisConfig `yaml:"redis"`
	PIDFile     string      `yaml:"pid_file"`
	LogLevel    string      `yaml:"log_level"`
}

// Load reads config from a YAML file at path and applies env overrides.
//
// Errors:
//   - ErrConfigNotFound     if the file does not exist
//   - ErrConfigUnreadable   if the file cannot be opened or read
//   - ErrUnknownFields      if the file contains keys not in Config
//   - ErrInvalidYAML        if the file is malformed or has type errors
//   - ErrMissingToken       if github_token is absent or empty
//   - ErrMissingRepos       if repos is absent or empty
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrConfigNotFound, path)
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrConfigUnreadable, path, err)
	}
	defer f.Close()

	c := defaults()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil {
		return nil, classifyYAMLError(err)
	}

	applyEnvOverrides(c)

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// LoadEnv builds a Config from environment variables only, using built-in
// defaults for any unset variable. github_token and repos are not required
// (they remain empty) — this loader is for non-daemon commands and tests.
func LoadEnv() *Config {
	c := defaults()
	applyEnvOverrides(c)
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
	}
}

// applyEnvOverrides overwrites Redis/PID/log fields from environment variables.
// github_token and repos are never overridden via env — they are file-only.
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
}

// validate checks that required fields are present.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.GitHubToken) == "" {
		return ErrMissingToken
	}
	if len(c.Repos) == 0 {
		return ErrMissingRepos
	}
	return nil
}

// classifyYAMLError maps a yaml decoder error to the appropriate sentinel.
func classifyYAMLError(err error) error {
	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		for _, msg := range typeErr.Errors {
			if strings.Contains(msg, "not found in type") {
				return fmt.Errorf("%w: %s", ErrUnknownFields, typeErr.Error())
			}
		}
		return fmt.Errorf("%w: %s", ErrInvalidYAML, typeErr.Error())
	}
	return fmt.Errorf("%w: %s", ErrInvalidYAML, err.Error())
}
