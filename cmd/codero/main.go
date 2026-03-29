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
	daemongrpc "github.com/codero/codero/internal/daemon/grpc"
	"github.com/codero/codero/internal/delivery"
	deliverypipeline "github.com/codero/codero/internal/delivery_pipeline"
	"github.com/codero/codero/internal/gate"
	ghclient "github.com/codero/codero/internal/github"
	loglib "github.com/codero/codero/internal/log"
	redislib "github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/runner"
	"github.com/codero/codero/internal/scheduler"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/sessmetrics"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tmux"
	"github.com/codero/codero/internal/webhook"
	"github.com/google/uuid"
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
		sessionCmd(&configPath),
		agentCmd(&configPath),
		queueCmd(&configPath),
		branchCmd(&configPath),
		eventsCmd(&configPath),
		scorecardCmd(&configPath),
		recordProvingEventCmd(&configPath),
		recordPrecommitCmd(&configPath),
		preflightCmd(),
		dailySnapshotCmd(&configPath),
		exitGateCmd(&configPath),
		dashboardCmd(&configPath),
		portsCmd(&configPath),
		pollCmd(&configPath),
		whyCmd(&configPath),
		proveCmd(&configPath),
		taskCmd(&configPath),
		contextCmd(),
		submitCmd(&configPath),
		setupCmd(),
		tailCmd(),
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
// Startup order follows codero_daemon_spec_v2 §3:
//
//  1. PID file
//  2. Config (flags > env > file > defaults)
//  3. SQLite + migrations
//  4. Redis (degrade on failure → polling-only mode)
//  5. Signal handlers
//  6. Components (delivery, scheduler, runner, expiry, reconciler)
//  7. Observability server
//  8. Ready sentinel + mark ready
//  9. Block on signals → phased shutdown
func daemonCmd(configPath *string) *cobra.Command {
	var (
		pidFile             string
		readyFile           string
		dbPath              string
		redisURL            string
		redisMaxRetries     int
		redisRetryInterval  int
		redisHealthInterval int
		// Sweeper config (§6.6)
		sweeperInterval   time.Duration
		sessionTTL        time.Duration
		branchHoldTTL     time.Duration
		handoffTTL        time.Duration
		issuePollInterval time.Duration
		// API server config (§6.3)
		apiAddr            string
		apiReadTimeout     time.Duration
		apiWriteTimeout    time.Duration
		apiShutdownTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:     "daemon",
		Aliases: []string{"serve"},
		Short:   "Start the codero daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// ─── Step 2: Load configuration ─────────────────────────
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}

			// CLI flag overrides (flags > env > config file > defaults).
			// Apply before Validate so derived paths are correct.
			pidFileOverride := pidFile != ""
			readyFileOverride := readyFile != ""
			if pidFile != "" {
				cfg.PIDFile = pidFile
			}
			if readyFile != "" {
				cfg.ReadyFile = readyFile
			}
			// If PID file was overridden but ready file was not,
			// derive ready sentinel from the new PID location.
			if pidFileOverride && !readyFileOverride {
				cfg.ReadyFile = filepath.Join(filepath.Dir(cfg.PIDFile), "codero.ready")
			}
			if dbPath != "" {
				cfg.DBPath = dbPath
			}
			if redisURL != "" {
				cfg.Redis.Addr = redisURL
			}
			if cmd.Flags().Changed("redis-max-retries") {
				cfg.Redis.MaxRetries = redisMaxRetries
			}
			if cmd.Flags().Changed("redis-retry-interval") {
				cfg.Redis.RetryInterval = redisRetryInterval
			}
			if cmd.Flags().Changed("redis-health-interval") {
				cfg.Redis.HealthInterval = redisHealthInterval
			}

			// Sweeper config overrides (§6.6).
			if cmd.Flags().Changed("sweeper-interval") {
				cfg.Sweeper.Interval = sweeperInterval
			}
			if cmd.Flags().Changed("session-ttl") {
				cfg.Sweeper.SessionTTL = sessionTTL
			}
			if cmd.Flags().Changed("branch-hold-ttl") {
				cfg.Sweeper.BranchHoldTTL = branchHoldTTL
			}
			if cmd.Flags().Changed("handoff-ttl") {
				cfg.Sweeper.HandoffTTL = handoffTTL
			}
			if cmd.Flags().Changed("issue-poll-interval") {
				cfg.Sweeper.IssuePollInterval = issuePollInterval
			}

			// API server config overrides (§6.3).
			if cmd.Flags().Changed("api-addr") {
				cfg.APIServer.Addr = apiAddr
			}
			if cmd.Flags().Changed("api-read-timeout") {
				cfg.APIServer.ReadTimeout = apiReadTimeout
			}
			if cmd.Flags().Changed("api-write-timeout") {
				cfg.APIServer.WriteTimeout = apiWriteTimeout
			}
			if cmd.Flags().Changed("api-shutdown-timeout") {
				cfg.APIServer.ShutdownTimeout = apiShutdownTimeout
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

			// ─── Step 1: PID file ───────────────────────────────────
			// Written first to prevent duplicate daemon starts.
			// Stale PID from unclean exit is detected and overwritten.
			if err := daemon.WritePID(cfg.PIDFile); err != nil {
				loglib.Error("codero: failed to write PID file",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
					"pid_file", cfg.PIDFile,
				)
				return fmt.Errorf("codero: %w", err)
			}
			loglib.Info("codero: PID file written",
				loglib.FieldEventType, loglib.EventStartup,
				loglib.FieldComponent, "daemon",
				"pid_file", cfg.PIDFile,
			)

			// Phased cleanup: PID file is removed on clean exit only.
			// On unclean exit (grace period exceeded / SIGKILL) the PID file
			// remains on disk so the next startup knows recovery is needed.
			// The ready sentinel is removed immediately on signal receipt,
			// not here in the defer.
			cleanExit := false
			defer func() {
				if !cleanExit {
					return
				}
				if err := daemon.RemovePID(cfg.PIDFile); err != nil {
					loglib.Warn("codero: failed to remove PID file on exit",
						loglib.FieldComponent, "daemon",
						"pid_file", cfg.PIDFile,
						"error", err,
					)
				}
			}()

			// ─── Step 3: SQLite + migrations ────────────────────────
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

			// ─── Step 4: Redis (degrade on failure) ─────────────────
			// Per spec §5.1: Redis failure enters polling-only mode.
			// The daemon continues with SQLite as source of truth.
			redisAvailable := true
			if err := daemon.CheckRedisWithRetry(cmd.Context(), cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.MaxRetries, cfg.Redis.RetryInterval); err != nil {
				loglib.Warn("codero: redis unavailable at startup — entering polling-only mode",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
					"addr", cfg.Redis.Addr,
					"retries", cfg.Redis.MaxRetries,
				)
				redisAvailable = false
				daemon.SetDegraded(true)
			}

			// ─── Step 5: Signal handlers + context ──────────────────
			// Must be registered before any goroutine spawns work.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var wg sync.WaitGroup

			// ─── Step 4b: Redis client and watch ────────────────────
			client := redislib.New(cfg.Redis.Addr, cfg.Redis.Password)
			defer client.Close()

			if redisAvailable {
				if err := client.LoadScripts(ctx); err != nil {
					loglib.Warn("codero: redis script load failed — continuing in polling-only mode",
						loglib.FieldEventType, loglib.EventStartup,
						loglib.FieldComponent, "daemon",
						"error", err,
					)
					redisAvailable = false
					daemon.SetDegraded(true)
				}
			}

			// Monitor Redis connectivity (reconnect on loss).
			wg.Add(1)
			go func() {
				defer wg.Done()
				daemon.WatchRedisWithInterval(ctx, client, cfg.Redis.HealthInterval)
			}()

			// ─── GitHub scope check (degrades, does not abort) ──────
			if os.Getenv("CODERO_SKIP_GITHUB_SCOPE_CHECK") == "true" {
				loglib.Info("codero: github scope check skipped (E2E/test mode)",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
				)
			} else if err := config.ValidateTokenScopes(cmd.Context(), cfg.GitHubToken, nil); err != nil {
				loglib.Warn("codero: github scope check failed — GitHub operations may be degraded",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
				)
				// Per spec §5.2: GitHub-unavailable mode.
				// Continue startup; tasks requiring GitHub are queued but not dispatched.
			}

			// ─── Step 6: Components ─────────────────────────────────
			stream := delivery.NewStream(db, client)
			queue := scheduler.NewQueue(client)
			leaseMgr := scheduler.NewLeaseManager(client)

			gh := ghclient.NewClient(cfg.GitHubToken)

			reviewRunner := runner.New(db, queue, leaseMgr, stream,
				runner.NewGitHubProvider(gh),
				runner.Config{Repos: cfg.Repos},
			)
			wg.Add(1)
			go func() {
				defer wg.Done()
				reviewRunner.Run(ctx)
			}()

			expiryWorker := scheduler.NewExpiryWorker(db, queue, stream)
			expiryWorker.TmuxChecker = tmux.RealExecutor{}
			reconciler := webhook.NewReconciler(db, gh, cfg.Repos, cfg.Webhook.Enabled)

			// ─── Session observability monitor ──────────────────────────
			interval := cfg.LiteLLMMetrics.Interval
			if interval <= 0 {
				interval = 30 * time.Second
			}
			metricsMonitor := sessmetrics.NewMonitor(cfg.LiteLLMMetrics.DSN, db, interval)
			wg.Add(1)
			go func() {
				defer wg.Done()
				metricsMonitor.Run(ctx)
			}()
			if cfg.AutoMerge.Enabled {
				reconciler.WithAutoMerge(gh, cfg.AutoMerge.Method)
				loglib.Info("codero: auto-merge enabled",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"merge_method", cfg.AutoMerge.Method,
				)
			}

			// ─── Step 7: API/observability server + gRPC ────────────
			slotCounter := scheduler.NewSlotCounter(client)
			sessStore := session.NewStore(db)

			loglib.Info("codero: running startup recovery sweep",
				loglib.FieldEventType, loglib.EventStartup,
				loglib.FieldComponent, "daemon",
			)
			expiryWorker.RunSessionExpiryCycle(ctx)
			expiryWorker.RunLeaseAuditCycle(ctx)
			reconciler.RunOnce(ctx)
			if err := ctx.Err(); err != nil {
				return err
			}

			pipeline := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
				StateDB: db,
				GitHub:  &githubPipelineAdapter{client: gh},
			})
			if err := pipeline.ClearStaleLocks(ctx); err != nil {
				loglib.Warn("codero: delivery pipeline stale lock sweep failed",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
				)
			}
			loglib.Info("codero: delivery pipeline initialized",
				loglib.FieldEventType, loglib.EventStartup,
				loglib.FieldComponent, "daemon",
			)

			// Create the gRPC daemon surface (Daemon Spec v2 §7).
			grpcSrv := daemongrpc.NewServer(daemongrpc.ServerConfig{
				DB:           db,
				RawDB:        db.Unwrap(),
				GitHubHealth: reconciler,
				SessionStore: sessStore,
				Version:      version,
			})

			// Serve gRPC + HTTP on the same port via h2c multiplexing.
			obs := daemon.NewObservabilityServerWithGRPC(client, queue, slotCounter, db.Unwrap(),
				cfg.APIServer.Addr, cfg.APIServer.ReadTimeout, cfg.APIServer.WriteTimeout,
				cfg.DashboardBasePath, version, grpcSrv.GRPCServer(), cfg)
			obs.SetPipeline(pipeline)
			obs.Start()
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-ctx.Done()
				stopCtx, stopCancel := context.WithTimeout(context.Background(), cfg.APIServer.ShutdownTimeout)
				defer stopCancel()
				if err := obs.Stop(stopCtx); err != nil {
					loglib.Error("codero: observability server shutdown error",
						loglib.FieldComponent, "daemon",
						"error", err,
					)
				}
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				expiryWorker.Run(ctx)
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				reconciler.Run(ctx)
			}()

			// Webhook server (optional).
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

			// ─── Step 8: Ready sentinel + mark ready ────────────────
			if err := daemon.WriteSentinel(cfg.ReadyFile); err != nil {
				loglib.Error("codero: failed to write ready sentinel",
					loglib.FieldEventType, loglib.EventStartup,
					loglib.FieldComponent, "daemon",
					"error", err,
					"ready_file", cfg.ReadyFile,
				)
				return fmt.Errorf("codero: ready sentinel: %w", err)
			}
			obs.MarkReady()
			grpcSrv.MarkReady()

			loglib.Info("codero: daemon started",
				loglib.FieldEventType, loglib.EventStartup,
				loglib.FieldComponent, "daemon",
				"pid", os.Getpid(),
				"webhook_enabled", cfg.Webhook.Enabled,
				"redis_available", redisAvailable,
				"grpc_enabled", true,
			)

			// ─── Step 9: Block on signals → phased shutdown ─────────
			// Shutdown order per spec §4.1:
			//   1. Mark not ready (stop serving new requests)
			//   2. Cancel context (drain in-flight work)
			//   3. Wait for goroutines (grace period)
			//   4. Close Redis, then DB (deferred)
			//   5. Remove PID sentinel (deferred, clean exit only)
			//
			// The ready sentinel is removed immediately on signal so that
			// /ready returns 503 during the grace window even if the process
			// is still running. This matches the spec requirement that readiness
			// flips before drain, not after.
			markNotReady := func() {
				obs.MarkNotReady()
				grpcSrv.MarkNotReady()
				if err := daemon.RemoveSentinel(cfg.ReadyFile); err != nil {
					loglib.Warn("codero: failed to remove ready sentinel during shutdown",
						loglib.FieldComponent, "daemon",
						"ready_file", cfg.ReadyFile,
						"error", err,
					)
				}
			}
			exitCode := daemon.HandleSignals(cancel, &wg, markNotReady)

			if exitCode != 0 {
				// Grace period exceeded — leave PID on disk for recovery.
				return fmt.Errorf("codero: grace period exceeded, shutdown incomplete")
			}

			cleanExit = true
			return nil
		},
	}

	// Daemon-specific CLI flags (highest precedence per spec §6).
	cmd.Flags().StringVar(&pidFile, "pid-file", "", "PID file path (overrides config/env)")
	cmd.Flags().StringVar(&readyFile, "ready-file", "", "ready sentinel path (overrides config/env)")
	cmd.Flags().StringVar(&dbPath, "db-path", "", "SQLite database path (overrides config/env)")
	cmd.Flags().StringVar(&redisURL, "redis-url", "", "Redis connection address (overrides config/env)")
	cmd.Flags().IntVar(&redisMaxRetries, "redis-max-retries", 3, "Redis startup retry attempts (overrides config/env, default 3)")
	cmd.Flags().IntVar(&redisRetryInterval, "redis-retry-interval", 1, "Redis retry backoff interval in seconds (overrides config/env, default 1)")
	cmd.Flags().IntVar(&redisHealthInterval, "redis-health-interval", 30, "Redis health check interval in seconds (overrides config/env, default 30)")

	// Sweeper config flags (§6.6).
	cmd.Flags().DurationVar(&sweeperInterval, "sweeper-interval", 60*time.Second, "Sweeper run interval (overrides config/env, default 60s)")
	cmd.Flags().DurationVar(&sessionTTL, "session-ttl", 90*time.Second, "Session heartbeat TTL (overrides config/env, default 90s)")
	cmd.Flags().DurationVar(&branchHoldTTL, "branch-hold-ttl", 72*time.Hour, "Branch hold TTL (overrides config/env, default 72h)")
	cmd.Flags().DurationVar(&handoffTTL, "handoff-ttl", 10*time.Minute, "Handoff acceptance TTL (overrides config/env, default 10m)")
	cmd.Flags().DurationVar(&issuePollInterval, "issue-poll-interval", 10*time.Minute, "Issue poll interval (overrides config/env, default 10m)")

	// API server config flags (§6.3).
	cmd.Flags().StringVar(&apiAddr, "api-addr", config.DefaultAPIServerAddr,
		fmt.Sprintf("API server bind address (overrides config/env, default %s)", config.DefaultAPIServerAddr))
	cmd.Flags().DurationVar(&apiReadTimeout, "api-read-timeout", config.DefaultAPIServerReadTimeout,
		fmt.Sprintf("API read timeout (overrides config/env, default %s)", config.DefaultAPIServerReadTimeout))
	cmd.Flags().DurationVar(&apiWriteTimeout, "api-write-timeout", config.DefaultAPIServerWriteTimeout,
		fmt.Sprintf("API write timeout (overrides config/env, default %s)", config.DefaultAPIServerWriteTimeout))
	cmd.Flags().DurationVar(&apiShutdownTimeout, "api-shutdown-timeout", config.DefaultAPIServerShutdownTimeout,
		fmt.Sprintf("API graceful shutdown timeout (overrides config/env, default %s)", config.DefaultAPIServerShutdownTimeout))

	return cmd
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

// registerCmd registers a branch for pre-commit waiting or queue submission.
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
2. Transitions to waiting (default) or queued_cli (with --skip-local)
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

			targetState := state.StateWaiting
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
					fmt.Println("For waiting, ensure the branch has been submitted first.")
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
				fmt.Println("Branch is in waiting - run commit-gate before committing.")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")
	cmd.Flags().IntVarP(&priority, "priority", "p", 10, "queue priority (0-20)")
	cmd.Flags().BoolVar(&skipLocal, "skip-local", false, "skip waiting and go directly to queue")

	return cmd
}

// sessionCmd manages agent session registration, heartbeats, and assignment attachment.
func sessionCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage agent sessions",
	}

	cmd.PersistentFlags().String("daemon-addr", "",
		"daemon gRPC address (e.g. 127.0.0.1:8110); routes commands via gRPC instead of direct DB (env: CODERO_DAEMON_ADDR)")

	cmd.AddCommand(
		sessionBootstrapCmd(configPath),
		sessionRegisterCmd(configPath),
		sessionConfirmCmd(configPath),
		sessionHeartbeatCmd(configPath),
		sessionAttachCmd(configPath),
		sessionFinalizeCmd(configPath),
		sessionEndCmd(configPath),
		sessionMetricsCmd(configPath),
	)

	return cmd
}

// resolveDaemonAddr resolves the daemon gRPC address. Priority:
// 1. --daemon-addr flag  2. CODERO_DAEMON_ADDR env  3. ~/.codero/config.yaml
// 4. Auto-detect localhost:8110. Empty string means use direct DB access.
func resolveDaemonAddr(cmd *cobra.Command) string {
	if addr, _ := cmd.Flags().GetString("daemon-addr"); addr != "" {
		return addr
	}
	if addr := os.Getenv("CODERO_DAEMON_ADDR"); addr != "" {
		return addr
	}
	if uc, err := config.LoadUserConfig(); err == nil {
		if uc.DaemonAddr != "" {
			return uc.DaemonAddr
		}
	} else {
		loglib.Warn("failed to load user config",
			loglib.FieldComponent, "cli",
			"error", err,
		)
	}
	if daemonReachable(defaultDaemonAddr) {
		return defaultDaemonAddr
	}
	return ""
}

func sessionRegisterCmd(configPath *string) *cobra.Command {
	var (
		sessionID string
		agentID   string
		mode      string
		tmuxName  string
	)

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a new agent session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentID == "" {
				agentID = resolveAgentIDFromEnv()
			}
			if mode == "" {
				mode = resolveSessionModeFromEnv("agent")
			}

			if daemonAddr := resolveDaemonAddr(cmd); daemonAddr != "" {
				client, err := daemongrpc.NewSessionClient(daemonAddr)
				if err != nil {
					return fmt.Errorf("session register: %w", err)
				}
				defer client.Close()

				result, err := client.Register(cmd.Context(), agentID, mode)
				if err != nil {
					return fmt.Errorf("session register: %w", err)
				}
				fmt.Printf("session_id: %s\n", result.SessionID)
				fmt.Printf("heartbeat_secret: %s\n", result.HeartbeatSecret)
				return nil
			}

			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			if sessionID == "" {
				sessionID = resolveSessionIDFromEnv()
			}
			if sessionID == "" {
				sessionID = uuid.New().String()
			}
			if tmuxName == "" {
				tmuxName = os.Getenv("CODERO_TMUX_NAME")
			}

			var (
				secret      string
				registerErr error
			)
			if tmuxName != "" {
				secret, registerErr = store.RegisterWithTmux(cmd.Context(), sessionID, agentID, mode, tmuxName)
			} else {
				secret, registerErr = store.Register(cmd.Context(), sessionID, agentID, mode)
			}
			if registerErr != nil {
				return registerErr
			}
			fmt.Printf("session_id: %s\n", sessionID)
			fmt.Printf("heartbeat_secret: %s\n", secret)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier (defaults to CODERO_SESSION_ID or auto-generated)")
	cmd.Flags().StringVar(&agentID, "agent-id", "", "agent identifier (defaults to CODERO_AGENT_ID)")
	cmd.Flags().StringVar(&mode, "mode", "", "session mode label (default: agent)")
	cmd.Flags().StringVar(&tmuxName, "tmux-name", "", "tmux session name (optional)")

	return cmd
}

func sessionConfirmCmd(configPath *string) *cobra.Command {
	var (
		sessionID string
		agentID   string
	)

	cmd := &cobra.Command{
		Use:   "confirm",
		Short: "Confirm that Codero has the injected live session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				sessionID = resolveSessionIDFromEnv()
			}
			if sessionID == "" {
				return fmt.Errorf("session-id is required")
			}
			if agentID == "" {
				agentID = resolveAgentIDFromEnv()
			}

			if daemonAddr := resolveDaemonAddr(cmd); daemonAddr != "" {
				client, err := daemongrpc.NewSessionClient(daemonAddr)
				if err != nil {
					return fmt.Errorf("session confirm: %w", err)
				}
				defer client.Close()
				if err := client.Confirm(cmd.Context(), sessionID, agentID); err != nil {
					return fmt.Errorf("session confirm: %w", err)
				}
				fmt.Printf("session_id: %s\n", sessionID)
				return nil
			}

			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			if err := store.Confirm(cmd.Context(), sessionID, agentID); err != nil {
				return err
			}
			fmt.Printf("session_id: %s\n", sessionID)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier (defaults to CODERO_SESSION_ID)")
	cmd.Flags().StringVar(&agentID, "agent-id", "", "agent identifier (defaults to CODERO_AGENT_ID)")

	return cmd
}

func sessionHeartbeatCmd(configPath *string) *cobra.Command {
	var (
		sessionID       string
		heartbeatSecret string
		markProgress    bool
	)

	cmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Emit a session heartbeat",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				sessionID = resolveSessionIDFromEnv()
			}
			if sessionID == "" {
				return fmt.Errorf("session-id is required")
			}
			if heartbeatSecret == "" {
				heartbeatSecret = os.Getenv("CODERO_HEARTBEAT_SECRET")
			}

			if daemonAddr := resolveDaemonAddr(cmd); daemonAddr != "" {
				client, err := daemongrpc.NewSessionClient(daemonAddr)
				if err != nil {
					return fmt.Errorf("session heartbeat: %w", err)
				}
				defer client.Close()
				if err := client.Heartbeat(cmd.Context(), sessionID, heartbeatSecret, markProgress); err != nil {
					return fmt.Errorf("session heartbeat: %w", err)
				}
				return nil
			}

			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			return store.Heartbeat(cmd.Context(), sessionID, heartbeatSecret, markProgress)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier (defaults to CODERO_SESSION_ID)")
	cmd.Flags().StringVar(&heartbeatSecret, "heartbeat-secret", "", "heartbeat secret (defaults to CODERO_HEARTBEAT_SECRET)")
	cmd.Flags().BoolVar(&markProgress, "progress", false, "also refresh session progress_at for active work")

	return cmd
}

func sessionAttachCmd(configPath *string) *cobra.Command {
	var (
		sessionID string
		agentID   string
		repo      string
		branch    string
		worktree  string
		mode      string
		taskID    string
		substatus string
	)

	cmd := &cobra.Command{
		Use:   "attach",
		Short: "Attach assignment context to a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				sessionID = resolveSessionIDFromEnv()
			}
			if sessionID == "" {
				return fmt.Errorf("session-id is required")
			}
			if agentID == "" {
				agentID = resolveAgentIDFromEnv()
			}
			if mode == "" {
				mode = resolveSessionModeFromEnv("agent")
			}
			if worktree == "" {
				worktree = resolveWorktreeFromEnv()
			}
			if worktree == "" {
				if cwd, err := os.Getwd(); err == nil {
					worktree = cwd
				}
			}
			if branch == "" {
				var err error
				branch, err = getCurrentBranch()
				if err != nil {
					return fmt.Errorf("get current branch: %w", err)
				}
			}

			if daemonAddr := resolveDaemonAddr(cmd); daemonAddr != "" {
				if repo == "" {
					return fmt.Errorf("--repo is required when using --daemon-addr")
				}
				client, err := daemongrpc.NewSessionClient(daemonAddr)
				if err != nil {
					return fmt.Errorf("session attach: %w", err)
				}
				defer client.Close()
				if err := client.AttachAssignment(cmd.Context(), sessionID, agentID, repo, branch, worktree, mode, taskID, substatus); err != nil {
					return fmt.Errorf("session attach: %w", err)
				}
				fmt.Printf("session %s attached to %s/%s\n", sessionID, repo, branch)
				return nil
			}

			cfg, err := loadConfig(*configPathForCmd(cmd))
			if err != nil {
				return fmt.Errorf("codero: config: %w", err)
			}
			if repo == "" {
				if len(cfg.Repos) == 0 {
					return fmt.Errorf("no repositories configured")
				}
				repo = cfg.Repos[0]
			}

			store, cleanup, err := openSessionStore(*configPathForCmd(cmd))
			if err != nil {
				return err
			}
			defer cleanup()

			if err := store.AttachAssignment(cmd.Context(), sessionID, agentID, repo, branch, worktree, mode, taskID, substatus); err != nil {
				return err
			}
			fmt.Printf("session %s attached to %s/%s\n", sessionID, repo, branch)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session-id", "", "session identifier (defaults to CODERO_SESSION_ID)")
	cmd.Flags().StringVar(&agentID, "agent-id", "", "agent identifier (defaults to CODERO_AGENT_ID)")
	cmd.Flags().StringVarP(&repo, "repo", "R", "", "repository (owner/repo)")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "branch name (default: current git branch)")
	cmd.Flags().StringVar(&worktree, "worktree", "", "worktree path (default: CODERO_WORKTREE or cwd)")
	cmd.Flags().StringVar(&mode, "mode", "", "session mode label (default: agent)")
	cmd.Flags().StringVar(&taskID, "task-id", "", "optional task identifier")
	cmd.Flags().StringVar(&substatus, "substatus", "in_progress", "assignment substatus")

	return cmd
}

func openSessionStore(configPath string) (*session.Store, func(), error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("codero: config: %w", err)
	}

	db, err := state.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open state store: %w", err)
	}
	cleanup := func() {
		_ = db.Close()
	}

	return session.NewStore(db), cleanup, nil
}

func resolveAgentIDFromEnv() string {
	if v := os.Getenv("CODERO_AGENT_ID"); v != "" {
		return v
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	if host, err := os.Hostname(); err == nil {
		return host
	}
	return ""
}

func resolveSessionIDFromEnv() string {
	if v := os.Getenv("CODERO_SESSION_ID"); v != "" {
		return v
	}
	if v := os.Getenv("CODERO_AGENT_SESSION_ID"); v != "" {
		return v
	}
	return ""
}

func resolveSessionModeFromEnv(fallback string) string {
	if v := os.Getenv("CODERO_SESSION_MODE"); v != "" {
		return v
	}
	return fallback
}

func resolveWorktreeFromEnv() string {
	if v := os.Getenv("CODERO_WORKTREE"); v != "" {
		return v
	}
	return ""
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
