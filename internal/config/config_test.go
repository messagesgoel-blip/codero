package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig writes a temporary codero.yaml test fixture and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codero.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return path
}

// clearConfigEnvOverrides clears every env override that Load and its helpers read.
func clearConfigEnvOverrides(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "CODERO_") || key == "GITHUB_TOKEN" {
			t.Setenv(key, "")
		}
	}
}

// clearLoadOverrideEnv removes every env override that Load and its helpers read.
func clearLoadOverrideEnv(t *testing.T) {
	t.Helper()
	clearConfigEnvOverrides(t)
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
	clearLoadOverrideEnv(t)
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
	clearLoadOverrideEnv(t)
	_, err := Load("/nonexistent/path/codero.yaml")
	if !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("want ErrConfigNotFound, got %v", err)
	}
}

func TestLoad_UnknownFields(t *testing.T) {
	clearLoadOverrideEnv(t)
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
	clearLoadOverrideEnv(t)
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
		APIServer: APIServerConfig{
			Addr:            ":7700",
			ShutdownTimeout: time.Second,
		},
		Sweeper: SweeperConfig{
			Interval:          time.Second,
			SessionTTL:        time.Second,
			BranchHoldTTL:     time.Second,
			HandoffTTL:        time.Second,
			IssuePollInterval: time.Second,
		},
		AutoMerge: AutoMergeConfig{Method: "squash"},
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

func TestValidate_SweeperDurations(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want error
	}{
		{
			name: "interval zero",
			mut: func(c *Config) {
				c.Sweeper.Interval = 0
			},
			want: ErrInvalidSweeperInterval,
		},
		{
			name: "interval negative",
			mut: func(c *Config) {
				c.Sweeper.Interval = -1 * time.Second
			},
			want: ErrInvalidSweeperInterval,
		},
		{
			name: "session ttl zero",
			mut: func(c *Config) {
				c.Sweeper.SessionTTL = 0
			},
			want: ErrInvalidSessionTTL,
		},
		{
			name: "session ttl negative",
			mut: func(c *Config) {
				c.Sweeper.SessionTTL = -1 * time.Second
			},
			want: ErrInvalidSessionTTL,
		},
		{
			name: "branch hold ttl zero",
			mut: func(c *Config) {
				c.Sweeper.BranchHoldTTL = 0
			},
			want: ErrInvalidBranchHoldTTL,
		},
		{
			name: "branch hold ttl negative",
			mut: func(c *Config) {
				c.Sweeper.BranchHoldTTL = -1 * time.Second
			},
			want: ErrInvalidBranchHoldTTL,
		},
		{
			name: "handoff ttl zero",
			mut: func(c *Config) {
				c.Sweeper.HandoffTTL = 0
			},
			want: ErrInvalidHandoffTTL,
		},
		{
			name: "handoff ttl negative",
			mut: func(c *Config) {
				c.Sweeper.HandoffTTL = -1 * time.Second
			},
			want: ErrInvalidHandoffTTL,
		},
		{
			name: "issue poll interval zero",
			mut: func(c *Config) {
				c.Sweeper.IssuePollInterval = 0
			},
			want: ErrInvalidIssuePollInterval,
		},
		{
			name: "issue poll interval negative",
			mut: func(c *Config) {
				c.Sweeper.IssuePollInterval = -1 * time.Second
			},
			want: ErrInvalidIssuePollInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := defaults()
			c.GitHubToken = "ghp_test"
			c.Repos = []string{"org/repo"}
			tt.mut(c)

			if err := c.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestValidate_APIServerAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want error
	}{
		{name: "default addr", addr: ":7700"},
		{name: "loopback addr", addr: "127.0.0.1:7700"},
		{name: "ipv6 addr", addr: "[::1]:7700"},
		{name: "empty", addr: "", want: ErrInvalidAPIServerAddr},
		{name: "missing port", addr: "localhost", want: ErrInvalidAPIServerAddr},
		{name: "zero port", addr: ":0", want: ErrInvalidAPIServerAddr},
		{name: "non numeric port", addr: "localhost:http", want: ErrInvalidAPIServerAddr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := defaults()
			c.GitHubToken = "ghp_test"
			c.Repos = []string{"org/repo"}
			c.APIServer.Addr = tt.addr

			err := c.Validate()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Validate() unexpected error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
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
		APIServer: APIServerConfig{
			Addr:            ":7700",
			ShutdownTimeout: time.Second,
		},
		Sweeper: SweeperConfig{
			Interval:          time.Second,
			SessionTTL:        time.Second,
			BranchHoldTTL:     time.Second,
			HandoffTTL:        time.Second,
			IssuePollInterval: time.Second,
		},
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
		APIServer: APIServerConfig{
			Addr:            ":7700",
			ShutdownTimeout: time.Second,
		},
		Sweeper: SweeperConfig{
			Interval:          time.Second,
			SessionTTL:        time.Second,
			BranchHoldTTL:     time.Second,
			HandoffTTL:        time.Second,
			IssuePollInterval: time.Second,
		},
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
		APIServer: APIServerConfig{
			Addr:            ":7700",
			ShutdownTimeout: time.Second,
		},
		Sweeper: SweeperConfig{
			Interval:          time.Second,
			SessionTTL:        time.Second,
			BranchHoldTTL:     time.Second,
			HandoffTTL:        time.Second,
			IssuePollInterval: time.Second,
		},
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
		APIServer: APIServerConfig{
			Addr:            ":7700",
			ShutdownTimeout: time.Second,
		},
		Sweeper: SweeperConfig{
			Interval:          time.Second,
			SessionTTL:        time.Second,
			BranchHoldTTL:     time.Second,
			HandoffTTL:        time.Second,
			IssuePollInterval: time.Second,
		},
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

func TestDefaults_SweeperConfig(t *testing.T) {
	c := defaults()

	if c.Sweeper.Interval != 60*time.Second {
		t.Errorf("Sweeper.Interval: got %s, want %s", c.Sweeper.Interval, 60*time.Second)
	}
	if c.Sweeper.SessionTTL != 90*time.Second {
		t.Errorf("Sweeper.SessionTTL: got %s, want %s", c.Sweeper.SessionTTL, 90*time.Second)
	}
	if c.Sweeper.BranchHoldTTL != 72*time.Hour {
		t.Errorf("Sweeper.BranchHoldTTL: got %s, want %s", c.Sweeper.BranchHoldTTL, 72*time.Hour)
	}
	if c.Sweeper.HandoffTTL != 10*time.Minute {
		t.Errorf("Sweeper.HandoffTTL: got %s, want %s", c.Sweeper.HandoffTTL, 10*time.Minute)
	}
	if c.Sweeper.IssuePollInterval != 10*time.Minute {
		t.Errorf("Sweeper.IssuePollInterval: got %s, want %s", c.Sweeper.IssuePollInterval, 10*time.Minute)
	}
}

func TestDefaults_APIServerConfig(t *testing.T) {
	c := defaults()

	if c.APIServer.Addr != DefaultAPIServerAddr {
		t.Errorf("APIServer.Addr: got %q, want %s", c.APIServer.Addr, DefaultAPIServerAddr)
	}
	if c.APIServer.ReadTimeout != DefaultAPIServerReadTimeout {
		t.Errorf("APIServer.ReadTimeout: got %s, want %s", c.APIServer.ReadTimeout, DefaultAPIServerReadTimeout)
	}
	if c.APIServer.WriteTimeout != DefaultAPIServerWriteTimeout {
		t.Errorf("APIServer.WriteTimeout: got %s, want %s", c.APIServer.WriteTimeout, DefaultAPIServerWriteTimeout)
	}
	if c.APIServer.ShutdownTimeout != DefaultAPIServerShutdownTimeout {
		t.Errorf("APIServer.ShutdownTimeout: got %s, want %s", c.APIServer.ShutdownTimeout, DefaultAPIServerShutdownTimeout)
	}
}

func TestEnvOverrides_SweeperConfig(t *testing.T) {
	t.Setenv("CODERO_SWEEPER_INTERVAL", "2m")
	t.Setenv("CODERO_SWEEPER_SESSION_TTL", "3m")
	t.Setenv("CODERO_SWEEPER_BRANCH_HOLD_TTL", "48h")
	t.Setenv("CODERO_SWEEPER_HANDOFF_TTL", "15m")
	t.Setenv("CODERO_SWEEPER_ISSUE_POLL_INTERVAL", "5m")

	c := defaults()
	applyEnvOverrides(c)

	if c.Sweeper.Interval != 2*time.Minute {
		t.Errorf("Sweeper.Interval: got %s, want %s", c.Sweeper.Interval, 2*time.Minute)
	}
	if c.Sweeper.SessionTTL != 3*time.Minute {
		t.Errorf("Sweeper.SessionTTL: got %s, want %s", c.Sweeper.SessionTTL, 3*time.Minute)
	}
	if c.Sweeper.BranchHoldTTL != 48*time.Hour {
		t.Errorf("Sweeper.BranchHoldTTL: got %s, want %s", c.Sweeper.BranchHoldTTL, 48*time.Hour)
	}
	if c.Sweeper.HandoffTTL != 15*time.Minute {
		t.Errorf("Sweeper.HandoffTTL: got %s, want %s", c.Sweeper.HandoffTTL, 15*time.Minute)
	}
	if c.Sweeper.IssuePollInterval != 5*time.Minute {
		t.Errorf("Sweeper.IssuePollInterval: got %s, want %s", c.Sweeper.IssuePollInterval, 5*time.Minute)
	}
}

func TestEnvOverrides_SweeperConfigLegacyAliases(t *testing.T) {
	t.Setenv("CODERO_SESSION_TTL", "3m")
	t.Setenv("CODERO_BRANCH_HOLD_TTL", "48h")
	t.Setenv("CODERO_HANDOFF_TTL", "15m")
	t.Setenv("CODERO_ISSUE_POLL_INTERVAL", "5m")

	c := defaults()
	applyEnvOverrides(c)

	if c.Sweeper.SessionTTL != 3*time.Minute {
		t.Errorf("Sweeper.SessionTTL: got %s, want %s", c.Sweeper.SessionTTL, 3*time.Minute)
	}
	if c.Sweeper.BranchHoldTTL != 48*time.Hour {
		t.Errorf("Sweeper.BranchHoldTTL: got %s, want %s", c.Sweeper.BranchHoldTTL, 48*time.Hour)
	}
	if c.Sweeper.HandoffTTL != 15*time.Minute {
		t.Errorf("Sweeper.HandoffTTL: got %s, want %s", c.Sweeper.HandoffTTL, 15*time.Minute)
	}
	if c.Sweeper.IssuePollInterval != 5*time.Minute {
		t.Errorf("Sweeper.IssuePollInterval: got %s, want %s", c.Sweeper.IssuePollInterval, 5*time.Minute)
	}
}

func TestEnvOverrides_APIServerConfig(t *testing.T) {
	t.Setenv("CODERO_API_ADDR", ":8800")
	t.Setenv("CODERO_API_READ_TIMEOUT", "45s")
	t.Setenv("CODERO_API_WRITE_TIMEOUT", "90s")
	t.Setenv("CODERO_API_SHUTDOWN_TIMEOUT", "20s")

	c := defaults()
	applyEnvOverrides(c)

	if c.APIServer.Addr != ":8800" {
		t.Errorf("APIServer.Addr: got %q, want :8800", c.APIServer.Addr)
	}
	if c.APIServer.ReadTimeout != 45*time.Second {
		t.Errorf("APIServer.ReadTimeout: got %s, want %s", c.APIServer.ReadTimeout, 45*time.Second)
	}
	if c.APIServer.WriteTimeout != 90*time.Second {
		t.Errorf("APIServer.WriteTimeout: got %s, want %s", c.APIServer.WriteTimeout, 90*time.Second)
	}
	if c.APIServer.ShutdownTimeout != 20*time.Second {
		t.Errorf("APIServer.ShutdownTimeout: got %s, want %s", c.APIServer.ShutdownTimeout, 20*time.Second)
	}
}

func TestLoad_SweeperAndAPIServerDurations(t *testing.T) {
	clearConfigEnvOverrides(t)

	path := writeConfig(t, `
github_token: ghp_test
repos:
  - acme/api
sweeper:
  interval: 2m
  session_ttl: 3m
  branch_hold_ttl: 48h
  handoff_ttl: 15m
  issue_poll_interval: 5m
api_server:
  addr: ":8800"
  read_timeout: 45s
  write_timeout: 90s
  shutdown_timeout: 20s
`)

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if c.Sweeper.Interval != 2*time.Minute {
		t.Errorf("Sweeper.Interval: got %s, want %s", c.Sweeper.Interval, 2*time.Minute)
	}
	if c.Sweeper.SessionTTL != 3*time.Minute {
		t.Errorf("Sweeper.SessionTTL: got %s, want %s", c.Sweeper.SessionTTL, 3*time.Minute)
	}
	if c.Sweeper.BranchHoldTTL != 48*time.Hour {
		t.Errorf("Sweeper.BranchHoldTTL: got %s, want %s", c.Sweeper.BranchHoldTTL, 48*time.Hour)
	}
	if c.Sweeper.HandoffTTL != 15*time.Minute {
		t.Errorf("Sweeper.HandoffTTL: got %s, want %s", c.Sweeper.HandoffTTL, 15*time.Minute)
	}
	if c.Sweeper.IssuePollInterval != 5*time.Minute {
		t.Errorf("Sweeper.IssuePollInterval: got %s, want %s", c.Sweeper.IssuePollInterval, 5*time.Minute)
	}
	if c.APIServer.Addr != ":8800" {
		t.Errorf("APIServer.Addr: got %q, want :8800", c.APIServer.Addr)
	}
	if c.APIServer.ReadTimeout != 45*time.Second {
		t.Errorf("APIServer.ReadTimeout: got %s, want %s", c.APIServer.ReadTimeout, 45*time.Second)
	}
	if c.APIServer.WriteTimeout != 90*time.Second {
		t.Errorf("APIServer.WriteTimeout: got %s, want %s", c.APIServer.WriteTimeout, 90*time.Second)
	}
	if c.APIServer.ShutdownTimeout != 20*time.Second {
		t.Errorf("APIServer.ShutdownTimeout: got %s, want %s", c.APIServer.ShutdownTimeout, 20*time.Second)
	}
}

func TestValidate_APIServerTimeouts(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want error
	}{
		{
			name: "shutdown timeout zero",
			mut:  func(c *Config) { c.APIServer.ShutdownTimeout = 0 },
			want: ErrInvalidShutdownTimeout,
		},
		{
			name: "shutdown timeout negative",
			mut:  func(c *Config) { c.APIServer.ShutdownTimeout = -1 * time.Second },
			want: ErrInvalidShutdownTimeout,
		},
		{
			name: "read timeout negative",
			mut:  func(c *Config) { c.APIServer.ReadTimeout = -1 * time.Second },
			want: ErrInvalidReadTimeout,
		},
		{
			name: "write timeout negative",
			mut:  func(c *Config) { c.APIServer.WriteTimeout = -1 * time.Second },
			want: ErrInvalidWriteTimeout,
		},
		{
			name: "read timeout zero is valid",
			mut:  func(c *Config) { c.APIServer.ReadTimeout = 0 },
		},
		{
			name: "write timeout zero is valid",
			mut:  func(c *Config) { c.APIServer.WriteTimeout = 0 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := defaults()
			c.GitHubToken = "ghp_test"
			c.Repos = []string{"org/repo"}
			tt.mut(c)
			err := c.Validate()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Validate() unexpected error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestEnvOverrides_ZeroDurationIgnored(t *testing.T) {
	// "0s" should NOT override defaults for strictly-positive sweeper fields.
	t.Setenv("CODERO_SWEEPER_INTERVAL", "0s")
	t.Setenv("CODERO_SWEEPER_SESSION_TTL", "0s")
	t.Setenv("CODERO_SWEEPER_BRANCH_HOLD_TTL", "0s")
	t.Setenv("CODERO_SWEEPER_HANDOFF_TTL", "0s")
	t.Setenv("CODERO_SWEEPER_ISSUE_POLL_INTERVAL", "0s")
	t.Setenv("CODERO_API_SHUTDOWN_TIMEOUT", "0s")

	c := defaults()
	applyEnvOverrides(c)

	// All should retain positive defaults since "0s" was rejected.
	if c.Sweeper.Interval <= 0 {
		t.Errorf("Sweeper.Interval should retain default, got %s", c.Sweeper.Interval)
	}
	if c.Sweeper.SessionTTL <= 0 {
		t.Errorf("Sweeper.SessionTTL should retain default, got %s", c.Sweeper.SessionTTL)
	}
	if c.Sweeper.BranchHoldTTL <= 0 {
		t.Errorf("Sweeper.BranchHoldTTL should retain default, got %s", c.Sweeper.BranchHoldTTL)
	}
	if c.Sweeper.HandoffTTL <= 0 {
		t.Errorf("Sweeper.HandoffTTL should retain default, got %s", c.Sweeper.HandoffTTL)
	}
	if c.Sweeper.IssuePollInterval <= 0 {
		t.Errorf("Sweeper.IssuePollInterval should retain default, got %s", c.Sweeper.IssuePollInterval)
	}
	if c.APIServer.ShutdownTimeout <= 0 {
		t.Errorf("APIServer.ShutdownTimeout should retain default, got %s", c.APIServer.ShutdownTimeout)
	}
}

func TestEnvOverrides_ZeroDurationAcceptedForNonNegative(t *testing.T) {
	// "0s" IS valid for read/write timeout (non-negative, not strictly positive).
	t.Setenv("CODERO_API_READ_TIMEOUT", "0s")
	t.Setenv("CODERO_API_WRITE_TIMEOUT", "0s")

	c := defaults()
	applyEnvOverrides(c)

	if c.APIServer.ReadTimeout != 0 {
		t.Errorf("APIServer.ReadTimeout: got %s, want 0s", c.APIServer.ReadTimeout)
	}
	if c.APIServer.WriteTimeout != 0 {
		t.Errorf("APIServer.WriteTimeout: got %s, want 0s", c.APIServer.WriteTimeout)
	}
}

func TestNormalizeCompat_LegacyBackfill(t *testing.T) {
	tests := []struct {
		name     string
		apiAddr  string
		obsHost  string
		obsPort  int
		wantAddr string
	}{
		{
			name:     "default api_server.addr backfills from observability_port",
			apiAddr:  ":7700",
			obsHost:  "",
			obsPort:  8080,
			wantAddr: ":8080",
		},
		{
			name:     "default api_server.addr backfills from custom observability_host and port",
			apiAddr:  ":7700",
			obsHost:  "127.0.0.1",
			obsPort:  9090,
			wantAddr: "127.0.0.1:9090",
		},
		{
			name:     "explicit api_server.addr is not overridden",
			apiAddr:  ":8800",
			obsHost:  "127.0.0.1",
			obsPort:  9090,
			wantAddr: ":8800",
		},
		{
			name:     "invalid observability_port skips backfill",
			apiAddr:  ":7700",
			obsHost:  "",
			obsPort:  0,
			wantAddr: ":7700",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := defaults()
			c.APIServer.Addr = tt.apiAddr
			c.ObservabilityHost = tt.obsHost
			c.ObservabilityPort = tt.obsPort
			normalizeCompat(c)
			if c.APIServer.Addr != tt.wantAddr {
				t.Errorf("APIServer.Addr = %q, want %q", c.APIServer.Addr, tt.wantAddr)
			}
		})
	}
}

func TestNormalizeCompat_LoadEnvBackfill(t *testing.T) {
	// Verify the full LoadEnv path applies legacy compat.
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	t.Setenv("CODERO_REPOS", "org/repo")
	t.Setenv("CODERO_OBSERVABILITY_HOST", "10.0.0.1")
	t.Setenv("CODERO_OBSERVABILITY_PORT", "9090")

	c := LoadEnv()
	if c.APIServer.Addr != "10.0.0.1:9090" {
		t.Errorf("APIServer.Addr = %q, want 10.0.0.1:9090 (backfilled from legacy)", c.APIServer.Addr)
	}
}

func TestNormalizeCompat_LoadEnvExplicitAPIAddrWins(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test")
	t.Setenv("CODERO_REPOS", "org/repo")
	t.Setenv("CODERO_API_ADDR", ":8800")
	t.Setenv("CODERO_OBSERVABILITY_PORT", "9090")

	c := LoadEnv()
	if c.APIServer.Addr != ":8800" {
		t.Errorf("APIServer.Addr = %q, want :8800 (explicit api_server.addr should win)", c.APIServer.Addr)
	}
}
