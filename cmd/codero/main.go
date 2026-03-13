package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/daemon"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=x.y.z".
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:          "codero",
		Short:        "codero — code review orchestration control plane",
		SilenceUsage: true,
	}

	root.AddCommand(daemonCmd(), statusCmd(), versionCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// daemonCmd starts the long-running daemon process.
func daemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Start the codero daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()

			// Redis must be reachable before doing anything else.
			if err := daemon.CheckRedis(cmd.Context(), cfg.RedisAddr, cfg.RedisPass); err != nil {
				fmt.Fprintf(os.Stderr, "codero: redis unavailable at %s: %v\n", cfg.RedisAddr, err)
				os.Exit(1)
			}

			if err := daemon.WritePID(cfg.PIDFile); err != nil {
				return fmt.Errorf("codero: %w", err)
			}
			defer daemon.RemovePID(cfg.PIDFile)

			log.Printf("codero: daemon started (pid %d)", os.Getpid())

			ctx, cancel := context.WithCancel(context.Background())
			var wg sync.WaitGroup

			// Initialize Redis client and load Lua scripts.
			// Startup fails fast if script loading fails.
			client := redislib.New(cfg.RedisAddr, cfg.RedisPass)
			if err := client.LoadScripts(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "codero: redis script load failed: %v\n", err)
				os.Exit(1)
			}

			// Monitor Redis connectivity after startup.
			wg.Add(1)
			go func() {
				defer wg.Done()
				daemon.WatchRedis(ctx, client)
			}()

			// HandleSignals blocks until SIGTERM/SIGINT, then cancels ctx,
			// waits for wg, and calls os.Exit.
			daemon.HandleSignals(cancel, &wg)
			return nil
		},
	}
}

// statusCmd reads the PID file and reports daemon state.
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()

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
			if err := daemon.CheckRedis(cmd.Context(), cfg.RedisAddr, cfg.RedisPass); err != nil {
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
