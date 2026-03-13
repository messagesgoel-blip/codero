package config

import "os"

// Config holds runtime configuration for the codero daemon.
// Environment variables override all defaults. File-based config comes in P1-S1-02.
type Config struct {
	RedisAddr string // default: "localhost:6379"
	RedisPass string // default: ""
	PIDFile   string // default: "/var/run/codero/codero.pid"
	LogLevel  string // default: "info"
	DBPath    string // default: "/var/lib/codero/codero.db"
}

// Load reads config from environment variables only.
// CODERO_REDIS_ADDR, CODERO_REDIS_PASS, CODERO_PID_FILE, CODERO_LOG_LEVEL,
// CODERO_DB_PATH. Returns defaults for any unset variable.
func Load() *Config {
	c := &Config{
		RedisAddr: "localhost:6379",
		RedisPass: "",
		PIDFile:   "/var/run/codero/codero.pid",
		LogLevel:  "info",
		DBPath:    "/var/lib/codero/codero.db",
	}
	if v := os.Getenv("CODERO_REDIS_ADDR"); v != "" {
		c.RedisAddr = v
	}
	if v := os.Getenv("CODERO_REDIS_PASS"); v != "" {
		c.RedisPass = v
	}
	if v := os.Getenv("CODERO_PID_FILE"); v != "" {
		c.PIDFile = v
	}
	if v := os.Getenv("CODERO_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("CODERO_DB_PATH"); v != "" {
		c.DBPath = v
	}
	return c
}
