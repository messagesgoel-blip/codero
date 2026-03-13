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
	t.Setenv("CODERO_DB_PATH", "")

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
	if c.DBPath != "/var/lib/codero/codero.db" {
		t.Errorf("DBPath: got %q", c.DBPath)
	}
}

func TestLoadEnv_Overrides(t *testing.T) {
	t.Setenv("CODERO_REDIS_ADDR", "redis.example.com:6380")
	t.Setenv("CODERO_REDIS_PASS", "secret")
	t.Setenv("CODERO_PID_FILE", "/tmp/codero.pid")
	t.Setenv("CODERO_LOG_LEVEL", "debug")
	t.Setenv("CODERO_DB_PATH", "/tmp/codero.db")
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	t.Setenv("CODERO_REPOS", " org/a , org/b ")

	c := LoadEnv()
	if c.Redis.Addr != "redis.example.com:6380" || c.Redis.Password != "secret" {
		t.Fatalf("redis overrides not applied: %#v", c.Redis)
	}
	if c.PIDFile != "/tmp/codero.pid" || c.LogLevel != "debug" || c.DBPath != "/tmp/codero.db" {
		t.Fatalf("override mismatch: %#v", c)
	}
	if len(c.Repos) != 2 || c.Repos[0] != "org/a" || c.Repos[1] != "org/b" {
		t.Fatalf("repos parse mismatch: %v", c.Repos)
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
db_path: /tmp/test.db
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.GitHubToken != "ghp_test" {
		t.Errorf("GitHubToken: got %q", c.GitHubToken)
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
	c := &Config{GitHubToken: " ", Repos: []string{"org/repo"}}
	if !errors.Is(c.Validate(), ErrMissingToken) {
		t.Fatalf("expected ErrMissingToken")
	}
	c = &Config{GitHubToken: "ghp", Repos: []string{"   "}}
	if !errors.Is(c.Validate(), ErrMissingRepos) {
		t.Fatalf("expected ErrMissingRepos")
	}
}
