package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/daemon"
	"github.com/codero/codero/internal/delivery"
	"github.com/codero/codero/internal/gate"
	ghclient "github.com/codero/codero/internal/github"
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
		gateStatusCmd(),
		gateCheckCmd(),
		registerCmd(),
		queueCmd(&configPath),
		branchCmd(&configPath),
		eventsCmd(&configPath),
		scorecardCmd(&configPath),
		recordProvingEventCmd(&configPath),
		recordPrecommitCmd(&configPath),
		preflightCmd(),
		dailySnapshotCmd(&configPath),
		exitGateCmd(&configPath),
		tuiCmd(),
		dashboardCmd(&configPath),
		portsCmd(&configPath),
		pollCmd(&configPath),
		whyCmd(&configPath),
		proveCmd(&configPath),
	)

	if err := root.Execute(); err != nil {
		var usageErr *UsageError
		if errors.As(err, &usageErr) {
			os.Exit(2)
		}
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

			// CODERO_SKIP_GITHUB_SCOPE_CHECK=true bypasses the live GitHub API
			// call that validates token scopes. Intended for E2E tests and
			// isolated environments where a real token is unavailable. Never
			// set this in production.
			if os.Getenv("CODERO_SKIP_GITHUB_SCOPE_CHECK") == "true" {
				loglib.Info("codero: github scope check skipped (E2E/test mode)",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
				)
			} else if err := config.ValidateTokenScopes(cmd.Context(), cfg.GitHubToken, nil); err != nil {
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
			// Remove PID file on exit (clean shutdown or error return).
			// HandleSignals waits for all goroutines before returning, so the
			// PID file outlives every subsystem cleanup defer below.
			defer func() {
				if err := daemon.RemovePID(cfg.PIDFile); err != nil {
					loglib.Warn("codero: failed to remove PID file on exit",
						loglib.FieldComponent, "daemon",
						"pid_file", cfg.PIDFile,
						"error", err,
					)
				}
			}()

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

			// GitHub client: shared by the review runner provider and the reconciler.
			gh := ghclient.NewClient(cfg.GitHubToken)

			// Review runner: consumes queued_cli branches and dispatches reviews.
			// Uses the real GitHubProvider to fetch CodeRabbit review comments.
			reviewRunner := runner.New(db, queue, leaseMgr, stream,
				runner.NewGitHubProvider(gh),
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
				gh,
				cfg.Repos,
				cfg.Webhook.Enabled,
			)
			if cfg.AutoMerge.Enabled {
				reconciler.WithAutoMerge(gh, cfg.AutoMerge.Method)
				loglib.Info("codero: auto-merge enabled",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"merge_method", cfg.AutoMerge.Method,
				)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				reconciler.Run(ctx)
			}()

			// Observability server: exposes /health, /queue, /metrics, /ready.
			slotCounter := scheduler.NewSlotCounter(client)
			obs := daemon.NewObservabilityServer(client, queue, slotCounter, db.Unwrap(),
				cfg.ObservabilityHost, strconv.Itoa(cfg.ObservabilityPort),
				cfg.DashboardBasePath, version)
			obs.Start()
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-ctx.Done()
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer stopCancel()
				if err := obs.Stop(stopCtx); err != nil {
					loglib.Error("codero: observability server shutdown error",
						loglib.FieldComponent, "daemon",
						"error", err,
					)
				}
			}()

			// Webhook server: started only if explicitly enabled.
			// Polling-only mode remains fully functional without it.
			if cfg.Webhook.Enabled {
				dedup := webhook.NewDeduplicator(db, client)
				proc := webhook.NewEventProcessor(db, stream).WithGitHubClient(gh)
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

// commitGateCmd runs the pre-commit review gate via the shared gate-heartbeat contract.
// It polls the shared heartbeat binary until STATUS: PASS or STATUS: FAIL, rendering
// a live progress bar during the run. All timeouts are env-driven and independent.
//
// On terminal state, provider-level outcomes (copilot, litellm) are automatically
// persisted to the proving scorecard DB. Failures to write metrics are warnings only
// and do not affect gate exit behavior.
func commitGateCmd() *cobra.Command {
	var repoPath string

	cmd := &cobra.Command{
		Use:   "commit-gate",
		Short: "Run pre-commit review gate via shared heartbeat",
		Long: `Run the mandatory pre-commit review gate before committing.

Delegates to the shared gate-heartbeat contract:
  - First call starts the run and returns STATUS: PENDING
  - Subsequent polls return PENDING until the gate completes
  - Terminal states: STATUS: PASS (commit allowed) or STATUS: FAIL (commit blocked)

Gate sequence: Copilot first, LiteLLM second.
Semgrep deterministic checks run inside the shared gate pipeline as a
hard blocker before AI pass/fail resolution.
Each gate has its own independent timeout; one gate timeout does not
reduce the next gate's budget.

On completion, provider-level outcomes are automatically recorded in the
proving scorecard DB. Manual use of 'record-precommit' is no longer required.

Timeout and polling are configurable via environment variables:
  CODERO_COPILOT_TIMEOUT_SEC      Copilot gate timeout (default: 15)
  CODERO_LITELLM_TIMEOUT_SEC      LiteLLM gate timeout (default: 45)
  CODERO_GATE_TOTAL_TIMEOUT_SEC   Overall gate wall-clock budget (default: 180)
  CODERO_GATE_POLL_INTERVAL_SEC   Poll interval between heartbeat calls (default: 180)
  CODERO_GATE_HEARTBEAT_BIN       Path to gate-heartbeat binary`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if repoPath == "" {
				absPath, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getwd: %w", err)
				}
				repoPath = absPath
			}

			cfg := gate.LoadConfig()
			cfg.RepoPath = repoPath

			if _, err := os.Stat(cfg.HeartbeatBin); err != nil {
				return fmt.Errorf("gate-heartbeat not found at %s (set CODERO_GATE_HEARTBEAT_BIN to override)", cfg.HeartbeatBin)
			}

			fmt.Printf("Starting pre-commit review gate...\n")
			fmt.Printf("  copilot timeout: %ds  litellm timeout: %ds  total: %ds\n",
				cfg.CopilotTimeoutSec, cfg.LiteLLMTimeoutSec, cfg.GateTotalTimeoutSec)

			runner := &gate.Runner{Cfg: cfg}

			result, err := runner.Run(cmd.Context(), func(r gate.Result) {
				fmt.Printf("\r%s", gate.FormatProgressLine(r))
			})

			fmt.Println() // end the progress line
			fmt.Println()
			fmt.Print(gate.FormatSummary(result))

			if err != nil {
				return fmt.Errorf("commit gate error: %w", err)
			}

			// Auto-record provider outcomes to proving scorecard DB.
			// This is a best-effort write; failures are warnings, not fatal.
			autoRecordGateOutcomes(cmd.Context(), result, repoPath, *configPathForCmd(cmd))

			switch result.Status {
			case gate.StatusPass:
				fmt.Println("✅ Commit gate PASSED")
				return nil
			case gate.StatusFail:
				fmt.Println("⚠️  Commit gate FAILED")
				return fmt.Errorf("commit gate failed")
			default:
				return fmt.Errorf("commit gate: unexpected status %q", result.Status)
			}
		},
	}

	cmd.Flags().StringVarP(&repoPath, "repo-path", "r", "",
		"path to repository (default: current directory)")

	return cmd
}

// autoRecordGateOutcomes persists per-provider pre-commit gate outcomes to the
// proving scorecard DB. Each provider (copilot, litellm) is recorded with an
// idempotent ID derived from run_id + provider, so repeated calls for the same
// gate run are safe (INSERT OR IGNORE).
//
// Failures to load config or open the DB are printed as warnings only; they do
// not affect the gate exit code or CLI behavior.
func autoRecordGateOutcomes(ctx context.Context, result gate.Result, repoPath, configPath string) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gate-metrics: config unavailable, skipping auto-record (%v)\n", err)
		return
	}

	db, err := state.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gate-metrics: DB unavailable, skipping auto-record (%v)\n", err)
		return
	}
	defer db.Close()

	branch, err := getCurrentBranchAt(repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gate-metrics: branch detection failed (%v)\n", err)
	}
	repo := ""
	if len(cfg.Repos) > 0 {
		repo = cfg.Repos[0]
	}

	for _, p := range []struct{ provider, gateState string }{
		{"copilot", result.CopilotStatus},
		{"litellm", result.LiteLLMStatus},
	} {
		if p.gateState == "" {
			continue
		}
		id := fmt.Sprintf("pc-%s-%s", result.RunID, p.provider)
		rev := &state.PrecommitReview{
			ID:       id,
			Repo:     repo,
			Branch:   branch,
			Provider: p.provider,
			Status:   GateStateToPrecommitStatus(p.gateState),
		}
		if err := state.CreatePrecommitReviewIdempotent(ctx, db, rev); err != nil {
			fmt.Fprintf(os.Stderr, "gate-metrics: record %s: %v\n", p.provider, err)
		}
	}
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
	return getCurrentBranchAt("")
}

// getCurrentBranchAt returns the current git branch at repoPath.
// If repoPath is empty, it uses the current working directory.
func getCurrentBranchAt(repoPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
