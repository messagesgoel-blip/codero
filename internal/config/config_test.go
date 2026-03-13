package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ---- LoadEnv (env-only, backward-compat) ----

func TestLoadEnv_Defaults(t *testing.T) {
	os.Unsetenv("CODERO_REDIS_ADDR")
	os.Unsetenv("CODERO_REDIS_PASS")
	os.Unsetenv("CODERO_PID_FILE")
	os.Unsetenv("CODERO_LOG_LEVEL")

	c := LoadEnv()
	if c.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis.Addr: got %q, want %q", c.Redis.Addr, "localhost:6379")
	}
	if c.Redis.Password != "" {
		t.Errorf("Redis.Password: got %q, want %q", c.Redis.Password, "")
	}
	if c.PIDFile != "/var/run/codero/codero.pid" {
		t.Errorf("PIDFile: got %q, want %q", c.PIDFile, "/var/run/codero/codero.pid")
	}
	if c.LogLevel != "info" {
		t.Errorf("LogLevel: got %q, want %q", c.LogLevel, "info")
	}
}

func TestLoadEnv_OverridesFromEnv(t *testing.T) {
	os.Setenv("CODERO_REDIS_ADDR", "redis.example.com:6380")
	os.Setenv("CODERO_REDIS_PASS", "secret")
	os.Setenv("CODERO_PID_FILE", "/tmp/codero-test.pid")
	os.Setenv("CODERO_LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("CODERO_REDIS_ADDR")
		os.Unsetenv("CODERO_REDIS_PASS")
		os.Unsetenv("CODERO_PID_FILE")
		os.Unsetenv("CODERO_LOG_LEVEL")
	}()

	c := LoadEnv()
	if c.Redis.Addr != "redis.example.com:6380" {
		t.Errorf("Redis.Addr: got %q, want %q", c.Redis.Addr, "redis.example.com:6380")
	}
	if c.Redis.Password != "secret" {
		t.Errorf("Redis.Password: got %q, want %q", c.Redis.Password, "secret")
	}
	if c.PIDFile != "/tmp/codero-test.pid" {
		t.Errorf("PIDFile: got %q, want %q", c.PIDFile, "/tmp/codero-test.pid")
	}
	if c.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q, want %q", c.LogLevel, "debug")
	}
}

// ---- Load (file-based) ----

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codero.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return path
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeConfig(t, `
github_token: ghp_test123
repos:
  - acme/api
  - acme/frontend
redis:
  addr: "localhost:6379"
  password: ""
pid_file: /tmp/test.pid
log_level: debug
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if c.GitHubToken != "ghp_test123" {
		t.Errorf("GitHubToken: got %q, want %q", c.GitHubToken, "ghp_test123")
	}
	if len(c.Repos) != 2 || c.Repos[0] != "acme/api" {
		t.Errorf("Repos: got %v", c.Repos)
	}
	if c.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis.Addr: got %q", c.Redis.Addr)
	}
	if c.PIDFile != "/tmp/test.pid" {
		t.Errorf("PIDFile: got %q", c.PIDFile)
	}
	if c.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q", c.LogLevel)
	}
}

func TestLoad_MinimalValidConfig(t *testing.T) {
	// Only required fields; defaults should fill the rest.
	path := writeConfig(t, `
github_token: ghp_minimal
repos:
  - org/repo
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if c.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis.Addr default not applied: got %q", c.Redis.Addr)
	}
	if c.LogLevel != "info" {
		t.Errorf("LogLevel default not applied: got %q", c.LogLevel)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/codero.yaml")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrConfigNotFound) {
		t.Errorf("want ErrConfigNotFound, got: %v", err)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	path := writeConfig(t, `
github_token: [not
  a valid: yaml
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidYAML) {
		t.Errorf("want ErrInvalidYAML, got: %v", err)
	}
}

func TestLoad_UnknownFields(t *testing.T) {
	path := writeConfig(t, `
github_token: ghp_test
repos:
  - org/repo
unknown_field: oops
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnknownFields) {
		t.Errorf("want ErrUnknownFields, got: %v", err)
	}
}

func TestLoad_MissingToken(t *testing.T) {
	path := writeConfig(t, `
repos:
  - org/repo
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingToken) {
		t.Errorf("want ErrMissingToken, got: %v", err)
	}
}

func TestLoad_MissingRepos(t *testing.T) {
	path := writeConfig(t, `
github_token: ghp_test
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingRepos) {
		t.Errorf("want ErrMissingRepos, got: %v", err)
	}
}

func TestLoad_EmptyRepos(t *testing.T) {
	path := writeConfig(t, `
github_token: ghp_test
repos: []
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingRepos) {
		t.Errorf("want ErrMissingRepos, got: %v", err)
	}
}

func TestLoad_EnvOverridesRedis(t *testing.T) {
	os.Setenv("CODERO_REDIS_ADDR", "override-host:6399")
	defer os.Unsetenv("CODERO_REDIS_ADDR")

	path := writeConfig(t, `
github_token: ghp_test
repos:
  - org/repo
redis:
  addr: "file-host:6379"
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Env should override file value for redis.addr.
	if c.Redis.Addr != "override-host:6399" {
		t.Errorf("Redis.Addr: env override not applied, got %q", c.Redis.Addr)
	}
}
