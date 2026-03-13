package config

import (
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("CODERO_REDIS_ADDR", "")
	t.Setenv("CODERO_REDIS_PASS", "")
	t.Setenv("CODERO_PID_FILE", "")
	t.Setenv("CODERO_LOG_LEVEL", "")

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
	t.Setenv("CODERO_REDIS_ADDR", "redis.example.com:6380")
	t.Setenv("CODERO_REDIS_PASS", "secret")
	t.Setenv("CODERO_PID_FILE", "/tmp/codero-test.pid")
	t.Setenv("CODERO_LOG_LEVEL", "debug")

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
