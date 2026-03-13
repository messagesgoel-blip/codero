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
	"github.com/redis/go-redis/v9"
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
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.Load()

			ctx := context.Background()
			redisOpts := &redis.Options{
				Addr:     cfg.RedisAddr,
				Password: cfg.RedisPass,
			}

			// Redis must be reachable before doing anything else.
			if err := daemon.CheckRedis(ctx, redisOpts); err != nil {
				fmt.Fprintf(os.Stderr, "codero: redis unavailable at %s: %v\n", cfg.RedisAddr, err)
				os.Exit(1)
			}

			if err := daemon.WritePID(cfg.PIDFile); err != nil {
				fmt.Fprintf(os.Stderr, "codero: %v\n", err)
				os.Exit(1)
			}
			defer daemon.RemovePID(cfg.PIDFile)

			log.Printf("codero: daemon started (pid %d)", os.Getpid())

			appCtx, cancel := context.WithCancel(context.Background())
			var wg sync.WaitGroup

			// Monitor Redis connectivity after startup.
			// All Redis clients are created through internal/redis — never via go-redis directly.
			client := redislib.New(cfg.RedisAddr, cfg.RedisPass)
			wg.Add(1)
			go func() {
				defer wg.Done()
				daemon.WatchRedis(appCtx, client.Unwrap())
			}()

			// HandleSignals blocks until SIGTERM/SIGINT, cancels ctx,
			// waits for wg, and returns an exit code.
			exitCode := daemon.HandleSignals(cancel, &wg)
			// Explicit cleanup since os.Exit skips defers.
			daemon.RemovePID(cfg.PIDFile)
			os.Exit(exitCode)
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
				return fmt.Errorf("codero: stale PID file (pid %d)", pid)
			}

			fmt.Printf("codero: running (pid %d)\n", pid)

			// Check Redis connectivity.
			redisState := "ok"
			redisOpts := &redis.Options{
				Addr:     cfg.RedisAddr,
				Password: cfg.RedisPass,
			}
			if err := daemon.CheckRedis(context.Background(), redisOpts); err != nil {
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
