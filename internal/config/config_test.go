package config

import (
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	c := Load()
	if c.RedisAddr != "localhost:6379" {
		t.Errorf("expected default RedisAddr, got %q", c.RedisAddr)
	}
	if c.LogLevel != "info" {
		t.Errorf("expected default LogLevel, got %q", c.LogLevel)
	}
	if c.LogPath != "" {
		t.Errorf("expected default LogPath to be empty, got %q", c.LogPath)
	}
	if c.DBPath != "/var/lib/codero/codero.db" {
		t.Errorf("expected default DBPath, got %q", c.DBPath)
	}
}

func TestLoad_Override(t *testing.T) {
	t.Setenv("CODERO_REDIS_ADDR", "127.0.0.1:6380")
	t.Setenv("CODERO_LOG_LEVEL", "debug")
	t.Setenv("CODERO_LOG_PATH", "/tmp/codero.log")
	t.Setenv("CODERO_DB_PATH", "/tmp/codero.db")

	c := Load()
	if c.RedisAddr != "127.0.0.1:6380" {
		t.Errorf("expected overridden RedisAddr, got %q", c.RedisAddr)
	}
	if c.LogLevel != "debug" {
		t.Errorf("expected overridden LogLevel, got %q", c.LogLevel)
	}
	if c.LogPath != "/tmp/codero.log" {
		t.Errorf("expected overridden LogPath, got %q", c.LogPath)
	}
	if c.DBPath != "/tmp/codero.db" {
		t.Errorf("expected overridden DBPath, got %q", c.DBPath)
	}
}
