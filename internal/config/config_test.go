package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("CODERO_REDIS_ADDR")
	os.Unsetenv("CODERO_REDIS_PASS")
	os.Unsetenv("CODERO_PID_FILE")
	os.Unsetenv("CODERO_LOG_LEVEL")

	c := Load()
	if c.RedisAddr != "localhost:6379" {
		t.Errorf("RedisAddr: got %q, want %q", c.RedisAddr, "localhost:6379")
	}
	if c.RedisPass != "" {
		t.Errorf("RedisPass: got %q, want %q", c.RedisPass, "")
	}
	if c.PIDFile != "/var/run/codero/codero.pid" {
		t.Errorf("PIDFile: got %q, want %q", c.PIDFile, "/var/run/codero/codero.pid")
	}
	if c.LogLevel != "info" {
		t.Errorf("LogLevel: got %q, want %q", c.LogLevel, "info")
	}
}

func TestLoad_OverridesFromEnv(t *testing.T) {
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

	c := Load()
	if c.RedisAddr != "redis.example.com:6380" {
		t.Errorf("RedisAddr: got %q, want %q", c.RedisAddr, "redis.example.com:6380")
	}
	if c.RedisPass != "secret" {
		t.Errorf("RedisPass: got %q, want %q", c.RedisPass, "secret")
	}
	if c.PIDFile != "/tmp/codero-test.pid" {
		t.Errorf("PIDFile: got %q, want %q", c.PIDFile, "/tmp/codero-test.pid")
	}
	if c.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q, want %q", c.LogLevel, "debug")
	}
}
