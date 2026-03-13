package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/daemon"
	loglib "github.com/codero/codero/internal/log"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/state"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=x.y.z".
var version = "dev"

func main() {
	var configPath string

	root := &cobra.Command{
		Use:          "codero",
		Short:        "codero — code review orchestration control plane",
		SilenceUsage: true,
	}

	// --config / -c is a global flag available to all subcommands.
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "codero.yaml",
		"path to codero YAML config file")

	root.AddCommand(daemonCmd(&configPath), statusCmd(&configPath), versionCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// loadConfig loads configuration from the YAML file at path.
// If path is the default "codero.yaml" and the file does not exist, it falls
// back to env-only loading so that existing env-based workflows keep working.
func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) && path == "codero.yaml" {
			return config.LoadEnv(), nil
		}
		return nil, err
	}
	return cfg, nil
}

// daemonCmd starts the long-running daemon process.
func daemonCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Start the codero daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			if err := loglib.Init(cfg.LogLevel, cfg.LogPath); err != nil {
				return fmt.Errorf("codero: log init: %w", err)
			}

			loglib.Info("codero: daemon starting",
				loglib.FieldEventType, loglib.EventStartup,
				loglib.FieldComponent, "daemon",
				"pid", os.Getpid(),
				"version", version,
			)

			if err := config.ValidateTokenScopes(cmd.Context(), cfg.GitHubToken, nil); err != nil {
				loglib.Error("codero: github scope check failed",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
				)
				var missingErr *config.ErrMissingScopes
				if errors.As(err, &missingErr) {
					return fmt.Errorf("codero: github token missing scopes: %s", strings.Join(missingErr.Missing, ", "))
				}
				return fmt.Errorf("codero: github scope check failed: %w", err)
			}

			// Redis must be reachable before doing anything else.
			if err := daemon.CheckRedis(cmd.Context(), cfg.Redis.Addr, cfg.Redis.Password); err != nil {
				loglib.Error("codero: redis unavailable",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
					"addr", cfg.Redis.Addr,
				)
				return fmt.Errorf("codero: redis unavailable at %s: %w", cfg.Redis.Addr, err)
			}

			// Acquire PID file early to prevent duplicate daemon starts.
			if err := daemon.WritePID(cfg.PIDFile); err != nil {
				loglib.Error("codero: failed to write PID file",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
					"pid_file", cfg.PIDFile,
				)
				return fmt.Errorf("codero: %w", err)
			}

			// Open SQLite state store and run pending migrations.
			db, err := state.Open(cfg.DBPath)
			if err != nil {
				loglib.Error("codero: state store open failed",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
					"db_path", cfg.DBPath,
				)
				return fmt.Errorf("codero: state store: %w", err)
			}
			defer db.Close()
			loglib.Info("codero: state store opened",
				loglib.FieldEventType, loglib.EventStartup,
				loglib.FieldComponent, "daemon",
				"db_path", cfg.DBPath,
			)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup

			// Initialize Redis client and load Lua scripts.
			// Startup fails fast if script loading fails.
			client := redislib.New(cfg.Redis.Addr, cfg.Redis.Password)
			defer client.Close()
			if err := client.LoadScripts(ctx); err != nil {
				loglib.Error("codero: redis script load failed",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
				)
				return fmt.Errorf("codero: redis script load failed: %w", err)
			}

			// Monitor Redis connectivity after startup.
			wg.Add(1)
			go func() {
				defer wg.Done()
				daemon.WatchRedis(ctx, client)
			}()

			loglib.Info("codero: daemon started",
				loglib.FieldEventType, loglib.EventStartup,
				loglib.FieldComponent, "daemon",
				"pid", os.Getpid(),
			)

			if exitCode := daemon.HandleSignals(cancel, &wg); exitCode != 0 {
				return fmt.Errorf("codero: grace period exceeded, shutdown incomplete")
			}
			return nil
		},
	}
}

// statusCmd reads the PID file and reports daemon state.
func statusCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			pid, err := daemon.ReadPID(cfg.PIDFile)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fmt.Println("codero: not running")
					return nil
				}
				return fmt.Errorf("codero: read pid file: %w", err)
			}

			if !daemon.ProcessRunning(pid) {
				return fmt.Errorf("codero: stale PID file (pid %d)", pid)
			}

			fmt.Printf("codero: running (pid %d)\n", pid)

			redisState := "ok"
			if err := daemon.CheckRedis(cmd.Context(), cfg.Redis.Addr, cfg.Redis.Password); err != nil {
				redisState = "unavailable"
			}
			fmt.Printf("redis: %s\n", redisState)
			fmt.Println("uptime: <not available until P1-S7>")

			return nil
		},
	}
}

// versionCmd prints the version string.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}
}
