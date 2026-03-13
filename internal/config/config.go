package config

import "os"

// Config holds runtime configuration for the codero daemon.
// Environment variables override all defaults. File-based config comes in P1-S1-02.
type Config struct {
	RedisAddr string // default: "localhost:6379"
	RedisPass string // default: ""
	PIDFile   string // default: "/var/run/codero/codero.pid"
	LogLevel  string // default: "info"
}

// Load reads config from environment variables only.
// CODERO_REDIS_ADDR, CODERO_REDIS_PASS, CODERO_PID_FILE, CODERO_LOG_LEVEL.
// Returns defaults for any unset variable. Uses os.LookupEnv to distinguish
// between unset and explicitly empty values.
func Load() *Config {
	c := &Config{
		RedisAddr: "localhost:6379",
		RedisPass: "",
		PIDFile:   "/var/run/codero/codero.pid",
		LogLevel:  "info",
	}
	if v, ok := os.LookupEnv("CODERO_REDIS_ADDR"); ok {
		c.RedisAddr = v
	}
	if v, ok := os.LookupEnv("CODERO_REDIS_PASS"); ok {
		c.RedisPass = v
	}
	if v, ok := os.LookupEnv("CODERO_PID_FILE"); ok {
		c.PIDFile = v
	}
	if v, ok := os.LookupEnv("CODERO_LOG_LEVEL"); ok {
		c.LogLevel = v
	}
	return c
}
