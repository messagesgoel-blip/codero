package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/daemon"
	"github.com/codero/codero/internal/delivery"
	loglib "github.com/codero/codero/internal/log"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/runner"
	"github.com/codero/codero/internal/scheduler"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/webhook"
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

	root.AddCommand(
		daemonCmd(&configPath),
		statusCmd(&configPath),
		versionCmd(),
		commitGateCmd(),
		registerCmd(),
		queueCmd(&configPath),
		branchCmd(&configPath),
		eventsCmd(&configPath),
	)

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

			// Sprint 5: initialize delivery stream, runner, expiry worker,
			// reconciler, and (optionally) webhook receiver.
			stream := delivery.NewStream(db, client)
			queue := scheduler.NewQueue(client)
			leaseMgr := scheduler.NewLeaseManager(client)

			// Review runner: consumes queued_cli branches and dispatches reviews.
			reviewRunner := runner.New(db, queue, leaseMgr, stream,
				runner.NewStubProvider(0),
				runner.Config{Repos: cfg.Repos},
			)
			wg.Add(1)
			go func() {
				defer wg.Done()
				reviewRunner.Run(ctx)
			}()

			// Expiry worker: session heartbeat TTL and lease audit.
			expiryWorker := scheduler.NewExpiryWorker(db, queue, stream)
			wg.Add(1)
			go func() {
				defer wg.Done()
				expiryWorker.Run(ctx)
			}()

			// Reconciler: polls GitHub for drift repair.
			// webhookEnabled=false → polling-only mode (60s interval).
			reconciler := webhook.NewReconciler(db,
				&webhook.StubGitHubClient{},
				cfg.Repos,
				cfg.Webhook.Enabled,
			)
			wg.Add(1)
			go func() {
				defer wg.Done()
				reconciler.Run(ctx)
			}()

			// Webhook server: started only if explicitly enabled.
			// Polling-only mode remains fully functional without it.
			if cfg.Webhook.Enabled {
				dedup := webhook.NewDeduplicator(db, client)
				proc := &webhook.NopProcessor{}
				handler := webhook.NewHandler(cfg.Webhook.Secret, dedup, proc)
				addr := fmt.Sprintf(":%d", cfg.Webhook.Port)
				srv := webhook.NewServer(addr, handler)

				loglib.Info("codero: webhook receiver starting",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "webhook",
					"addr", addr,
				)

				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := srv.Start(ctx); err != nil {
						loglib.Error("codero: webhook server error",
							loglib.FieldComponent, "webhook",
							"error", err,
							"ctx_err", ctx.Err(),
						)
					}
				}()
			} else {
				loglib.Info("codero: webhook receiver disabled (polling-only mode)",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
				)
			}
			loglib.Info("codero: daemon started",
				loglib.FieldEventType, loglib.EventStartup,
				loglib.FieldComponent, "daemon",
				"pid", os.Getpid(),
				"webhook_enabled", cfg.Webhook.Enabled,
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

// commitGateCmd runs the pre-commit review gate.
// It executes the repo's two-pass-review.sh script and returns non-zero if the gate fails.
func commitGateCmd() *cobra.Command {
	var (
		timeout   int
		repoPath  string
		scriptDir string
	)

	cmd := &cobra.Command{
		Use:   "commit-gate",
		Short: "Run pre-commit review gate",
		Long: `Run the mandatory two-pass review gate before commit.

This command delegates to two-pass-review.sh in the repo.
The script defines stage order and blocking behavior.
Exit code 0 allows commit, non-zero aborts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if repoPath == "" {
				absPath, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getwd: %w", err)
				}
				repoPath = absPath
			}

			if scriptDir == "" {
				possiblePaths := []string{
					filepath.Join(repoPath, ".codero", "scripts", "review"),
					filepath.Join(repoPath, "scripts", "review"),
				}
				for _, p := range possiblePaths {
					if _, err := os.Stat(p); err == nil {
						scriptDir = p
						break
					}
				}
				if scriptDir == "" {
					return fmt.Errorf("could not find review scripts directory; pass --script-dir")
				}
			}

			twoPassScript := filepath.Join(scriptDir, "two-pass-review.sh")
			if _, err := os.Stat(twoPassScript); err != nil {
				return fmt.Errorf("two-pass-review.sh not found in %s", scriptDir)
			}

			fmt.Println("Running pre-commit review gate...")
			env := os.Environ()
			env = append(env, fmt.Sprintf("CODERO_REPO_PATH=%s", repoPath))
			if timeout > 0 {
				env = append(env, fmt.Sprintf("CODERO_REVIEW_PASS_TIMEOUT_SEC=%d", timeout))
			}

			ctx := cmd.Context()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
				defer cancel()
			}

			// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
			execCmd := exec.CommandContext(ctx, twoPassScript)
			execCmd.Env = env
			execCmd.Dir = repoPath
			execCmd.Stdin = os.Stdin
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr

			err := execCmd.Run()
			if err != nil {
				if execErr, ok := err.(*exec.ExitError); ok {
					fmt.Println("\n⚠️  Commit gate FAILED")
					return fmt.Errorf("commit gate failed (exit code: %d)", execErr.ExitCode())
				}
				return fmt.Errorf("commit gate error: %w", err)
			}

			fmt.Println("\n✅ Commit gate PASSED")
			return nil
		},
	}

	cmd.Flags().IntVarP(&timeout, "timeout", "t", 300,
		"timeout for each review pass in seconds (default: 300)")
	cmd.Flags().StringVarP(&repoPath, "repo-path", "r", "",
		"path to repository (default: current directory)")
	cmd.Flags().StringVar(&scriptDir, "script-dir", "",
		"path to review scripts directory")

	return cmd
}

// registerCmd registers a branch for local review or queue submission.
func registerCmd() *cobra.Command {
	var (
		branch    string
		repo      string
		priority  int
		skipLocal bool
	)

	cmd := &cobra.Command{
		Use:   "register [branch]",
		Short: "Register a branch for review",
		Long: `Register a branch for codero review orchestration.

This command:
1. Records the branch in the local state store
2. Transitions to local_review (default) or queued_cli (with --skip-local)
3. Enables tracking and scoring for dispatch

If no branch is provided, uses the current git branch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPathForCmd(cmd))
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			if len(args) > 0 {
				branch = args[0]
			} else {
				branch, err = getCurrentBranch()
				if err != nil {
					return fmt.Errorf("get current branch: %w", err)
				}
			}

			if repo == "" {
				if len(cfg.Repos) == 0 {
					return fmt.Errorf("no repositories configured")
				}
				repo = cfg.Repos[0]
			}

			db, err := state.Open(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open state store: %w", err)
			}
			defer db.Close()

			targetState := state.StateLocalReview
			trigger := "codero-cli register"

			if skipLocal {
				targetState = state.StateQueuedCLI
				trigger = "codero-cli register --skip-local"
			}

			existing, err := state.GetBranch(db, repo, branch)
			if err != nil {
				if errors.Is(err, state.ErrBranchNotFound) {
					fmt.Printf("Branch %s/%s not found in state store.\n", repo, branch)
					fmt.Println("Branches are typically registered via webhook or daemon submit.")
					fmt.Println("For local_review, ensure the branch has been submitted first.")
					return fmt.Errorf("branch not registered")
				}
				return fmt.Errorf("check branch: %w", err)
			}

			if err := state.UpdateQueuePriority(db, existing.ID, priority); err != nil {
				return fmt.Errorf("update priority: %w", err)
			}

			if err := state.TransitionBranch(db, existing.ID, existing.State, targetState, trigger); err != nil {
				return fmt.Errorf("transition branch: %w", err)
			}
			fmt.Printf("Branch %s/%s: %s -> %s\n", repo, branch, existing.State, targetState)

			fmt.Printf("\nBranch registered successfully.\n")
			if skipLocal {
				fmt.Println("Branch is in queue for review dispatch.")
			} else {
				fmt.Println("Branch is in local_review - run commit-gate before committing.")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")
	cmd.Flags().IntVarP(&priority, "priority", "p", 10, "queue priority (0-20)")
	cmd.Flags().BoolVar(&skipLocal, "skip-local", false, "skip local_review and go directly to queue")

	return cmd
}

// configPathForCmd extracts the config path from cobra command flags.
func configPathForCmd(cmd *cobra.Command) *string {
	if cfgPath, err := cmd.Flags().GetString("config"); err == nil && cfgPath != "" {
		return &cfgPath
	}
	defaultPath := "codero.yaml"
	return &defaultPath
}

// getCurrentBranch returns the current git branch.
func getCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
