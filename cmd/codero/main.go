package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/daemon"
	"github.com/redis/go-redis/v9"
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
// Any other error (wrong path, bad YAML, unknown fields) is returned as-is.
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

			// Validate GitHub token scopes before doing anything else.
			// Scope check is daemon-only; status/version remain lightweight.
			if cfg.GitHubToken != "" {
				if err := config.ValidateTokenScopes(cmd.Context(), cfg.GitHubToken, nil); err != nil {
					var missingErr *config.ErrMissingScopes
					if errors.As(err, &missingErr) {
						fmt.Fprintf(os.Stderr, "codero: github token missing scopes: %s\n",
							strings.Join(missingErr.Missing, ", "))
					} else {
						fmt.Fprintf(os.Stderr, "codero: github scope check failed: %v\n", err)
					}
					os.Exit(1)
				}
			}

			// Redis must be reachable before doing anything else.
			if err := daemon.CheckRedis(cfg.Redis.Addr); err != nil {
				fmt.Fprintf(os.Stderr, "codero: redis unavailable at %s: %v\n", cfg.Redis.Addr, err)
				os.Exit(1)
			}

			if err := daemon.WritePID(cfg.PIDFile); err != nil {
				return fmt.Errorf("codero: %w", err)
			}
			defer daemon.RemovePID(cfg.PIDFile)

			log.Printf("codero: daemon started (pid %d)", os.Getpid())

			ctx, cancel := context.WithCancel(context.Background())
			var wg sync.WaitGroup

			// Monitor Redis connectivity after startup.
			client := redis.NewClient(&redis.Options{
				Addr:     cfg.Redis.Addr,
				Password: cfg.Redis.Password,
			})
			wg.Add(1)
			go func() {
				defer wg.Done()
				daemon.WatchRedis(ctx, client)
			}()

			// HandleSignals blocks until SIGTERM/SIGINT, then cancels ctx,
			// waits for wg, and calls os.Exit. The defer above won't run on
			// os.Exit, so PID removal is registered before blocking.
			daemon.HandleSignals(cancel, &wg)
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
				fmt.Printf("codero: stale PID file (pid %d)\n", pid)
				os.Exit(1)
			}

			fmt.Printf("codero: running (pid %d)\n", pid)

			// Check Redis connectivity.
			redisState := "ok"
			if err := daemon.CheckRedis(cfg.Redis.Addr); err != nil {
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
