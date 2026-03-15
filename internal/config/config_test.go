package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codero.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return path
}

func TestLoadEnv_Defaults(t *testing.T) {
	t.Setenv("CODERO_REDIS_ADDR", "")
	t.Setenv("CODERO_REDIS_PASS", "")
	t.Setenv("CODERO_PID_FILE", "")
	t.Setenv("CODERO_LOG_LEVEL", "")
	t.Setenv("CODERO_LOG_PATH", "")
	t.Setenv("CODERO_DB_PATH", "")
	t.Setenv("CODERO_OBSERVABILITY_PORT", "")

	c := LoadEnv()
	if c.Redis.Addr != "localhost:6379" {
		t.Errorf("Redis.Addr: got %q", c.Redis.Addr)
	}
	if c.PIDFile != "/var/run/codero/codero.pid" {
		t.Errorf("PIDFile: got %q", c.PIDFile)
	}
	if c.LogLevel != "info" {
		t.Errorf("LogLevel: got %q", c.LogLevel)
	}
	if c.LogPath != "" {
		t.Errorf("LogPath: got %q", c.LogPath)
	}
	if c.DBPath != "/var/lib/codero/codero.db" {
		t.Errorf("DBPath: got %q", c.DBPath)
	}
	if c.ObservabilityPort != 8080 {
		t.Errorf("ObservabilityPort: got %d, want 8080", c.ObservabilityPort)
	}
}

func TestLoadEnv_Overrides(t *testing.T) {
	t.Setenv("CODERO_REDIS_ADDR", "redis.example.com:6380")
	t.Setenv("CODERO_REDIS_PASS", "secret")
	t.Setenv("CODERO_PID_FILE", "/tmp/codero.pid")
	t.Setenv("CODERO_LOG_LEVEL", "debug")
	t.Setenv("CODERO_LOG_PATH", "/tmp/codero.log")
	t.Setenv("CODERO_DB_PATH", "/tmp/codero.db")
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	t.Setenv("CODERO_REPOS", " org/a , org/b ")
	t.Setenv("CODERO_OBSERVABILITY_PORT", "9091")

	c := LoadEnv()
	if c.Redis.Addr != "redis.example.com:6380" || c.Redis.Password != "secret" {
		t.Fatalf("redis overrides not applied: %#v", c.Redis)
	}
	if c.PIDFile != "/tmp/codero.pid" || c.LogLevel != "debug" || c.LogPath != "/tmp/codero.log" || c.DBPath != "/tmp/codero.db" {
		t.Fatalf("override mismatch: %#v", c)
	}
	if len(c.Repos) != 2 || c.Repos[0] != "org/a" || c.Repos[1] != "org/b" {
		t.Fatalf("repos parse mismatch: %v", c.Repos)
	}
	if c.ObservabilityPort != 9091 {
		t.Errorf("ObservabilityPort: got %d, want 9091", c.ObservabilityPort)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeConfig(t, `
github_token: ghp_test
repos:
  - acme/api
redis:
  addr: "localhost:6379"
pid_file: /tmp/test.pid
log_level: debug
log_path: /tmp/test.log
db_path: /tmp/test.db
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.GitHubToken != "ghp_test" {
		t.Errorf("GitHubToken: got %q", c.GitHubToken)
	}
	if c.LogPath != "/tmp/test.log" {
		t.Errorf("LogPath: got %q", c.LogPath)
	}
	if c.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath: got %q", c.DBPath)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/codero.yaml")
	if !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("want ErrConfigNotFound, got %v", err)
	}
}

func TestLoad_UnknownFields(t *testing.T) {
	path := writeConfig(t, `
github_token: ghp_test
repos:
  - org/repo
unexpected: true
`)
	_, err := Load(path)
	if !errors.Is(err, ErrUnknownFields) {
		t.Fatalf("want ErrUnknownFields, got %v", err)
	}
}

func TestLoad_MultipleDocuments(t *testing.T) {
	path := writeConfig(t, `
github_token: ghp_test
repos:
  - org/repo
---
foo: bar
`)
	_, err := Load(path)
	if !errors.Is(err, ErrMultipleDocuments) {
		t.Fatalf("want ErrMultipleDocuments, got %v", err)
	}
}

func TestValidate_MissingRequired(t *testing.T) {
	c := &Config{GitHubToken: " ", Repos: []string{"org/repo"}, ObservabilityPort: 8080}
	if !errors.Is(c.Validate(), ErrMissingToken) {
		t.Fatalf("expected ErrMissingToken")
	}
	c = &Config{GitHubToken: "ghp", Repos: []string{"   "}, ObservabilityPort: 8080}
	if !errors.Is(c.Validate(), ErrMissingRepos) {
		t.Fatalf("expected ErrMissingRepos")
	}
}

func TestValidate_ObservabilityPort(t *testing.T) {
	valid := &Config{GitHubToken: "ghp_test", Repos: []string{"org/repo"}, ObservabilityPort: 8080}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid port to pass, got: %v", err)
	}

	for _, bad := range []int{0, -1, 65536, 99999} {
		c := &Config{GitHubToken: "ghp_test", Repos: []string{"org/repo"}, ObservabilityPort: bad}
		if !errors.Is(c.Validate(), ErrInvalidObservabilityPort) {
			t.Errorf("port %d: expected ErrInvalidObservabilityPort, got: %v", bad, c.Validate())
		}
	}
}
