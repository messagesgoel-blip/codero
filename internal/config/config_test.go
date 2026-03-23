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
	t.Setenv("CODERO_READY_FILE", "")
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
	wantReadyFile := filepath.Join(filepath.Dir(c.PIDFile), "codero.ready")
	if c.ReadyFile != wantReadyFile {
		t.Errorf("ReadyFile: got %q, want %q", c.ReadyFile, wantReadyFile)
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
	readyOverride := filepath.Join(t.TempDir(), "codero.ready")
	t.Setenv("CODERO_REDIS_ADDR", "redis.example.com:6380")
	t.Setenv("CODERO_REDIS_PASS", "secret")
	t.Setenv("CODERO_PID_FILE", "/tmp/codero.pid")
	t.Setenv("CODERO_READY_FILE", readyOverride)
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
	if c.ReadyFile != readyOverride {
		t.Errorf("ReadyFile override: got %q, want %q", c.ReadyFile, readyOverride)
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
	c := &Config{
		GitHubToken:       " ",
		Repos:             []string{"org/repo"},
		ObservabilityPort: 8080,
		AutoMerge:         AutoMergeConfig{Method: "squash"},
	}
	if !errors.Is(c.Validate(), ErrMissingToken) {
		t.Fatalf("expected ErrMissingToken")
	}
	c = &Config{
		GitHubToken:       "ghp",
		Repos:             []string{"   "},
		ObservabilityPort: 8080,
		AutoMerge:         AutoMergeConfig{Method: "squash"},
	}
	if !errors.Is(c.Validate(), ErrMissingRepos) {
		t.Fatalf("expected ErrMissingRepos")
	}
}

func TestValidate_ObservabilityPort(t *testing.T) {
	valid := &Config{
		GitHubToken:       "ghp_test",
		Repos:             []string{"org/repo"},
		ObservabilityPort: 8080,
		AutoMerge:         AutoMergeConfig{Method: "squash"},
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid port to pass, got: %v", err)
	}

	for _, bad := range []int{0, -1, 65536, 99999} {
		c := &Config{
			GitHubToken:       "ghp_test",
			Repos:             []string{"org/repo"},
			ObservabilityPort: bad,
			AutoMerge:         AutoMergeConfig{Method: "squash"},
		}
		err := c.Validate()
		if !errors.Is(err, ErrInvalidObservabilityPort) {
			t.Errorf("port %d: expected ErrInvalidObservabilityPort, got: %v", bad, err)
		}
	}
}

// TestNewFields_COD026 verifies the new config fields added in COD-026.
func TestNewFields_COD026(t *testing.T) {
	c := defaults()
	if c.ObservabilityHost != "" {
		t.Errorf("default ObservabilityHost: want empty string, got %q", c.ObservabilityHost)
	}
	if c.DashboardBasePath != "/dashboard" {
		t.Errorf("default DashboardBasePath: want /dashboard, got %q", c.DashboardBasePath)
	}
	if c.DashboardPublicBaseURL != "" {
		t.Errorf("default DashboardPublicBaseURL: want empty, got %q", c.DashboardPublicBaseURL)
	}
}

func TestValidate_DashboardBasePath(t *testing.T) {
	good := &Config{
		GitHubToken:       "ghp_test",
		Repos:             []string{"org/repo"},
		ObservabilityPort: 8080,
		DashboardBasePath: "/my-dashboard",
		AutoMerge:         AutoMergeConfig{Method: "squash"},
	}
	if err := good.Validate(); err != nil {
		t.Errorf("valid base path should pass: %v", err)
	}

	emptyPath := &Config{
		GitHubToken:       "ghp_test",
		Repos:             []string{"org/repo"},
		ObservabilityPort: 8080,
		DashboardBasePath: "",
		AutoMerge:         AutoMergeConfig{Method: "squash"},
	}
	if err := emptyPath.Validate(); err != nil {
		t.Errorf("empty base path should pass: %v", err)
	}

	bad := &Config{
		GitHubToken:       "ghp_test",
		Repos:             []string{"org/repo"},
		ObservabilityPort: 8080,
		DashboardBasePath: "no-leading-slash",
		AutoMerge:         AutoMergeConfig{Method: "squash"},
	}
	if err := bad.Validate(); !errors.Is(err, ErrInvalidDashboardBasePath) {
		t.Errorf("no-leading-slash: expected ErrInvalidDashboardBasePath, got %v", err)
	}
}

func TestValidate_AutoMergeMethodAlwaysValidated(t *testing.T) {
	invalidWhenDisabled := &Config{
		GitHubToken:       "ghp_test",
		Repos:             []string{"org/repo"},
		ObservabilityPort: 8080,
		AutoMerge: AutoMergeConfig{
			Enabled: false,
			Method:  "invalid",
		},
	}
	if err := invalidWhenDisabled.Validate(); !errors.Is(err, ErrInvalidMergeMethod) {
		t.Fatalf("disabled with invalid method: expected ErrInvalidMergeMethod, got %v", err)
	}

	validWhenDisabled := &Config{
		GitHubToken:       "ghp_test",
		Repos:             []string{"org/repo"},
		ObservabilityPort: 8080,
		AutoMerge: AutoMergeConfig{
			Enabled: false,
			Method:  "squash",
		},
	}
	if err := validWhenDisabled.Validate(); err != nil {
		t.Fatalf("disabled with valid method should pass, got %v", err)
	}
}

func TestEnvOverrides_COD026(t *testing.T) {
	t.Setenv("CODERO_OBSERVABILITY_HOST", "127.0.0.1")
	t.Setenv("CODERO_DASHBOARD_BASE_PATH", "/codero/ui")
	t.Setenv("CODERO_DASHBOARD_PUBLIC_BASE_URL", "https://ops.example.com")

	c := defaults()
	applyEnvOverrides(c)

	if c.ObservabilityHost != "127.0.0.1" {
		t.Errorf("ObservabilityHost: got %q, want 127.0.0.1", c.ObservabilityHost)
	}
	if c.DashboardBasePath != "/codero/ui" {
		t.Errorf("DashboardBasePath: got %q, want /codero/ui", c.DashboardBasePath)
	}
	if c.DashboardPublicBaseURL != "https://ops.example.com" {
		t.Errorf("DashboardPublicBaseURL: got %q, want https://ops.example.com", c.DashboardPublicBaseURL)
	}
}

func TestEnvOverrides_PIDFileDerivesReadyFile(t *testing.T) {
	// When CODERO_PID_FILE is set but CODERO_READY_FILE is not,
	// ReadyFile should be derived from the PID file location.
	t.Setenv("CODERO_PID_FILE", "/custom/run/codero.pid")
	t.Setenv("CODERO_READY_FILE", "") // Explicitly unset

	c := defaults()
	applyEnvOverrides(c)

	if c.PIDFile != "/custom/run/codero.pid" {
		t.Errorf("PIDFile: got %q, want /custom/run/codero.pid", c.PIDFile)
	}
	wantReady := filepath.Join("/custom", "run", "codero.ready")
	if c.ReadyFile != wantReady {
		t.Errorf("ReadyFile: got %q, want %q (derived from PID dir)", c.ReadyFile, wantReady)
	}
}

func TestEnvOverrides_ReadyFileOverrideTakesPrecedence(t *testing.T) {
	// When both CODERO_PID_FILE and CODERO_READY_FILE are set,
	// the explicit ready file override should win.
	t.Setenv("CODERO_PID_FILE", "/custom/run/codero.pid")
	t.Setenv("CODERO_READY_FILE", "/other/path/codero.ready")

	c := defaults()
	applyEnvOverrides(c)

	if c.PIDFile != "/custom/run/codero.pid" {
		t.Errorf("PIDFile: got %q", c.PIDFile)
	}
	if c.ReadyFile != "/other/path/codero.ready" {
		t.Errorf("ReadyFile: got %q, want explicit override /other/path/codero.ready", c.ReadyFile)
	}
}

func TestEnvOverrides_NoPIDOverrideKeepsDefaultReady(t *testing.T) {
	// When CODERO_PID_FILE is not set, ReadyFile should remain at default.
	t.Setenv("CODERO_PID_FILE", "")
	t.Setenv("CODERO_READY_FILE", "")

	c := defaults()
	applyEnvOverrides(c)

	wantReady := filepath.Join(filepath.Dir(c.PIDFile), "codero.ready")
	if c.ReadyFile != wantReady {
		t.Errorf("ReadyFile: got %q, want default %q", c.ReadyFile, wantReady)
	}
}

func TestDefaults_RedisConfig(t *testing.T) {
	c := defaults()

	if c.Redis.MaxRetries != 3 {
		t.Errorf("Redis.MaxRetries: got %d, want 3", c.Redis.MaxRetries)
	}
	if c.Redis.RetryInterval != 1 {
		t.Errorf("Redis.RetryInterval: got %d, want 1", c.Redis.RetryInterval)
	}
	if c.Redis.HealthInterval != 30 {
		t.Errorf("Redis.HealthInterval: got %d, want 30", c.Redis.HealthInterval)
	}
}

func TestEnvOverrides_RedisConfig(t *testing.T) {
	t.Setenv("CODERO_REDIS_MAX_RETRIES", "5")
	t.Setenv("CODERO_REDIS_RETRY_INTERVAL", "2")
	t.Setenv("CODERO_REDIS_HEALTH_INTERVAL", "60")

	c := defaults()
	applyEnvOverrides(c)

	if c.Redis.MaxRetries != 5 {
		t.Errorf("Redis.MaxRetries: got %d, want 5", c.Redis.MaxRetries)
	}
	if c.Redis.RetryInterval != 2 {
		t.Errorf("Redis.RetryInterval: got %d, want 2", c.Redis.RetryInterval)
	}
	if c.Redis.HealthInterval != 60 {
		t.Errorf("Redis.HealthInterval: got %d, want 60", c.Redis.HealthInterval)
	}
}

func TestEnvOverrides_RedisConfigInvalidValuesIgnored(t *testing.T) {
	tests := []struct {
		name    string
		envKey  string
		envVal  string
		wantMax int
		wantInt int
		wantHLT int
	}{
		{
			name:    "max retries invalid string",
			envKey:  "CODERO_REDIS_MAX_RETRIES",
			envVal:  "invalid",
			wantMax: 3,
			wantInt: 1,
			wantHLT: 30,
		},
		{
			name:    "max retries negative",
			envKey:  "CODERO_REDIS_MAX_RETRIES",
			envVal:  "-1",
			wantMax: 3,
			wantInt: 1,
			wantHLT: 30,
		},
		{
			name:    "retry interval invalid string",
			envKey:  "CODERO_REDIS_RETRY_INTERVAL",
			envVal:  "invalid",
			wantMax: 3,
			wantInt: 1,
			wantHLT: 30,
		},
		{
			name:    "retry interval negative",
			envKey:  "CODERO_REDIS_RETRY_INTERVAL",
			envVal:  "-1",
			wantMax: 3,
			wantInt: 1,
			wantHLT: 30,
		},
		{
			name:    "health interval invalid string",
			envKey:  "CODERO_REDIS_HEALTH_INTERVAL",
			envVal:  "invalid",
			wantMax: 3,
			wantInt: 1,
			wantHLT: 30,
		},
		{
			name:    "health interval negative",
			envKey:  "CODERO_REDIS_HEALTH_INTERVAL",
			envVal:  "-1",
			wantMax: 3,
			wantInt: 1,
			wantHLT: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envVal)

			c := defaults()
			applyEnvOverrides(c)

			if c.Redis.MaxRetries != tt.wantMax {
				t.Errorf("Redis.MaxRetries: got %d, want %d", c.Redis.MaxRetries, tt.wantMax)
			}
			if c.Redis.RetryInterval != tt.wantInt {
				t.Errorf("Redis.RetryInterval: got %d, want %d", c.Redis.RetryInterval, tt.wantInt)
			}
			if c.Redis.HealthInterval != tt.wantHLT {
				t.Errorf("Redis.HealthInterval: got %d, want %d", c.Redis.HealthInterval, tt.wantHLT)
			}
		})
	}
}
