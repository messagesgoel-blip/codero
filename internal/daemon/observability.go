package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	configpkg "github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/gate"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/scheduler"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	ggrpc "google.golang.org/grpc"
)

// ObservabilityServer provides HTTP endpoints for health, queue status, and metrics.
// It implements the observability skeleton contract from codero-roadmap-v5.md:
// - /health: Returns uptime, Redis status, and slot counter status
// - /queue: Returns queue snapshot with branch ordering/scores for a given repo
// - /metrics: Returns Prometheus-compatible text format metrics
// - /ready: Returns readiness status for Kubernetes probes
// - /api/v1/agent-metrics: Returns effectiveness metrics per agent and project
// - /gate: Returns the current pre-commit gate progress (dashboard UI parity)
type ObservabilityServer struct {
	server      *http.Server           // HTTP server for serving endpoints
	redisClient *redis.Client          // Redis client for health checks
	queue       *scheduler.Queue       // WFQ queue for queue introspection
	slotCounter *scheduler.SlotCounter // Slot counter for dispatch slot status
	db          *sql.DB                // SQLite state store for metrics queries
	pipeline    pipelineRunner         // Delivery pipeline for submit handling
	startTime   time.Time              // Process start time for uptime calculation
	mu          sync.RWMutex           // Mutex for thread-safe state access
	repoPath    string                 // Repo path for gate progress file lookup
	version     string                 // Binary version string set via ldflags
	ready       atomic.Bool            // Set true after full daemon bootstrap completes
	grpcServer  *ggrpc.Server          // Optional gRPC server for daemon contract surface
	cfg         *configpkg.Config      // Full daemon configuration
}

// NewObservabilityServer creates a new observability server.
// host is the bind address (empty string → all interfaces); port is the TCP port string.
// dashboardBasePath is the URL prefix for the dashboard SPA (default "/dashboard").
func NewObservabilityServer(redisClient *redis.Client, queue *scheduler.Queue, slotCounter *scheduler.SlotCounter, db *sql.DB, host, port, dashboardBasePath, version string, cfg *configpkg.Config) *ObservabilityServer {
	return NewObservabilityServerWithAddr(redisClient, queue, slotCounter, db, net.JoinHostPort(host, port), 0, 0, dashboardBasePath, version, cfg)
}

// NewObservabilityServerWithAddr creates a new observability server using a full
// bind address plus server-level read/write timeouts.
func NewObservabilityServerWithAddr(redisClient *redis.Client, queue *scheduler.Queue, slotCounter *scheduler.SlotCounter, db *sql.DB, addr string, readTimeout, writeTimeout time.Duration, dashboardBasePath, version string, cfg *configpkg.Config) *ObservabilityServer {
	return NewObservabilityServerWithGRPC(redisClient, queue, slotCounter, db, addr, readTimeout, writeTimeout, dashboardBasePath, version, nil, cfg)
}

// NewObservabilityServerWithGRPC creates a new observability server with optional
// gRPC multiplexing. When grpcServer is non-nil, gRPC and HTTP share the same port
// via h2c (HTTP/2 cleartext) per Daemon Spec v2 §7: "gRPC + REST on CODERO_API_ADDR".
func NewObservabilityServerWithGRPC(redisClient *redis.Client, queue *scheduler.Queue, slotCounter *scheduler.SlotCounter, db *sql.DB, addr string, readTimeout, writeTimeout time.Duration, dashboardBasePath, version string, grpcServer *ggrpc.Server, cfg *configpkg.Config) *ObservabilityServer {
	if dashboardBasePath == "" {
		dashboardBasePath = "/dashboard"
	}
	// Normalise: must start with "/" and must not end with "/" (except bare "/").
	if !strings.HasPrefix(dashboardBasePath, "/") {
		dashboardBasePath = "/" + dashboardBasePath
	}
	dashboardBasePath = strings.TrimRight(dashboardBasePath, "/")
	if dashboardBasePath == "" {
		dashboardBasePath = "/dashboard"
	}

	mux := http.NewServeMux()

	// When a gRPC server is provided, multiplex gRPC and HTTP on the same
	// listener using h2c (HTTP/2 cleartext). gRPC requests are identified by
	// Content-Type "application/grpc" and routed to the gRPC server; all
	// other requests fall through to the HTTP mux.
	var handler http.Handler = mux
	if grpcServer != nil {
		handler = h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ct := r.Header.Get("Content-Type")
			if r.ProtoMajor == 2 && strings.HasPrefix(ct, "application/grpc") {
				grpcServer.ServeHTTP(w, r)
			} else {
				mux.ServeHTTP(w, r)
			}
		}), &http2.Server{})
	}

	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}
	if readTimeout > 0 {
		server.ReadHeaderTimeout = readTimeout
	}

	repoPath := os.Getenv("CODERO_REPO_PATH")
	if repoPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			repoPath = "/"
			loglib.Warn("observability: failed to resolve working directory; using fallback repo path",
				loglib.FieldEventType, "observability_repo_path_fallback",
				loglib.FieldComponent, "daemon",
				"error", err,
				"repo_path", repoPath,
			)
		} else {
			repoPath = wd
		}
	}

	obs := &ObservabilityServer{
		server:      server,
		redisClient: redisClient,
		queue:       queue,
		slotCounter: slotCounter,
		db:          db,
		startTime:   time.Now(),
		repoPath:    repoPath,
		version:     version,
		grpcServer:  grpcServer,
		cfg:         cfg,
	}

	// Register observability routes
	mux.HandleFunc("/health", obs.handleHealth)
	mux.HandleFunc("/queue", obs.handleQueue)
	mux.HandleFunc("/metrics", obs.handleMetrics)
	mux.HandleFunc("/ready", obs.handleReady)
	mux.HandleFunc("/api/v1/agent-metrics", obs.handleAgentMetrics)
	mux.HandleFunc("/gate", obs.handleGate)
	mux.HandleFunc("/api/v1/assignments/", obs.handleSubmit)

	// Register dashboard API routes and static file serving.
	settingsDir := filepath.Dir(os.Getenv("CODERO_DB_PATH"))
	if settingsDir == "." || settingsDir == "" {
		settingsDir = os.TempDir()
	}
	dashHandler := dashboard.NewHandler(db, dashboard.NewSettingsStore(settingsDir), cfg)
	dashHandler.RegisterRoutes(mux)

	// Serve dashboard static files under dashboardBasePath + "/".
	// Files are embedded from internal/dashboard/static/ at build time.
	staticFS, err := fs.Sub(dashboard.Static, "static")
	if err != nil {
		loglib.Error("dashboard: failed to create sub-FS for static assets",
			loglib.FieldComponent, "daemon", "error", err)
	} else {
		fileServer := http.FileServer(http.FS(staticFS))
		// During development, prevent caching of JS/CSS so changes are visible immediately.
		cachedFileServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			isHTML := strings.HasSuffix(p, "/") || strings.HasSuffix(p, ".html") || strings.HasSuffix(p, "index.html")
			if isHTML || strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".css") {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=60")
			}
			fileServer.ServeHTTP(w, r)
		})
		// Strip the base path before serving static files so that the embedded
		// index.html is served for any path under dashboardBasePath/.
		mux.Handle(dashboardBasePath+"/", http.StripPrefix(dashboardBasePath, cachedFileServer))
		// Redirect bare dashboardBasePath to dashboardBasePath/ so the SPA loads.
		mux.HandleFunc(dashboardBasePath, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, dashboardBasePath+"/", http.StatusMovedPermanently)
		})
	}

	// Redirect root to dashboard so the public URL works without /dashboard/ suffix.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, dashboardBasePath+"/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	return obs
}

// SetPipeline registers the delivery pipeline used by the submit endpoint.
func (o *ObservabilityServer) SetPipeline(p pipelineRunner) {
	o.pipeline = p
}

// Start launches the observability HTTP server in a background goroutine.
func (o *ObservabilityServer) Start() {
	go func() {
		loglib.Info("codero: observability server starting",
			loglib.FieldEventType, "observability_start",
			loglib.FieldComponent, "daemon",
			"addr", o.server.Addr,
		)
		if err := o.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			loglib.Error("codero: observability server error",
				loglib.FieldEventType, "observability_error",
				loglib.FieldComponent, "daemon",
				"error", err,
			)
		}
	}()
}

// Stop gracefully shuts down the observability server and any attached gRPC server.
func (o *ObservabilityServer) Stop(ctx context.Context) error {
	var (
		grpcErr error
		wg      sync.WaitGroup
	)
	if o.grpcServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.grpcServer.GracefulStop()
		}()
	}
	httpErr := o.server.Shutdown(ctx)

	if o.grpcServer != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			wg.Wait()
		}()

		select {
		case <-done:
		case <-ctx.Done():
			o.grpcServer.Stop()
			<-done
			if httpErr == nil {
				grpcErr = ctx.Err()
			}
		}
	}

	if httpErr != nil {
		return httpErr
	}
	return grpcErr
}

// Handler exposes the observability HTTP handler for integration tests.
func (o *ObservabilityServer) Handler() http.Handler {
	return o.server.Handler
}

// handleHealth returns service health status.
func (o *ObservabilityServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := map[string]interface{}{
		"status":         "ok",
		"uptime_seconds": time.Since(o.startTime).Seconds(),
		"version":        o.version,
		"ready":          o.ready.Load(),
	}

	// Check Redis connectivity
	redisStatus := "ok"
	rc := o.redisClient.Unwrap()
	if err := rc.Ping(ctx).Err(); err != nil {
		redisStatus = "error: " + err.Error()
		status["status"] = "degraded"
	}
	status["redis"] = redisStatus

	// Check slot counter
	if o.slotCounter != nil {
		slots, err := o.slotCounter.GetSlotCount(ctx, "*")
		if err == nil {
			status["slots"] = slots
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if status["status"] == "ok" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(status)
}

// handleQueue returns queue snapshot for a repo.
func (o *ObservabilityServer) handleQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	repo := r.URL.Query().Get("repo")

	if repo == "" {
		http.Error(w, "repo parameter required", http.StatusBadRequest)
		return
	}

	if o.queue == nil {
		http.Error(w, "queue not configured", http.StatusServiceUnavailable)
		return
	}

	entries, err := o.queue.List(ctx, repo)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list queue: %v", err), http.StatusInternalServerError)
		return
	}

	length, err := o.queue.Len(ctx, repo)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get queue length: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"repo":   repo,
		"length": length,
		"items":  entries,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleMetrics returns Prometheus-compatible metrics.
func (o *ObservabilityServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Start with process metrics
	uptime := time.Since(o.startTime).Seconds()
	// nosemgrep: go.lang.security.audit.xss.no-fprintf-to-responsewriter.no-fprintf-to-responsewriter
	// This endpoint serves Prometheus text format metrics, not HTML. Prometheus is the only consumer.
	fmt.Fprintf(w, "# HELP codero_uptime_seconds Seconds since service start\n")
	fmt.Fprintf(w, "# TYPE codero_uptime_seconds gauge\n")
	fmt.Fprintf(w, "codero_uptime_seconds %.2f\n", uptime) //nolint:errcheck // nosemgrep: go.lang.security.audit.xss.no-fprintf-to-responsewriter.no-fprintf-to-responsewriter

	// Queue metrics
	if o.queue != nil {
		// Get queue length for a sample repo if available
		// In real implementation, we'd iterate over all repos
		// nosemgrep: go.lang.security.audit.xss.no-fprintf-to-responsewriter.no-fprintf-to-responsewriter
		fmt.Fprintf(w, "# HELP codero_queue_length Number of items in queue\n")
		fmt.Fprintf(w, "# TYPE codero_queue_length gauge\n")
	}

	// Slot counter metrics
	if o.slotCounter != nil {
		// nosemgrep: go.lang.security.audit.xss.no-fprintf-to-responsewriter.no-fprintf-to-responsewriter
		fmt.Fprintf(w, "# HELP codero_active_slots Current number of active dispatch slots\n")
		fmt.Fprintf(w, "# TYPE codero_active_slots gauge\n")
		// Would need to iterate repos for actual values
		// nosemgrep: go.lang.security.audit.xss.no-fprintf-to-responsewriter.no-fprintf-to-responsewriter
		fmt.Fprintf(w, "# HELP codero_slot_limit Maximum allowed concurrent dispatches\n")
		fmt.Fprintf(w, "# TYPE codero_slot_limit gauge\n")
	}

	// Redis metrics
	// nosemgrep: go.lang.security.audit.xss.no-fprintf-to-responsewriter.no-fprintf-to-responsewriter
	rc := o.redisClient.Unwrap()
	if err := rc.Ping(ctx).Err(); err == nil {
		fmt.Fprintf(w, "# HELP codero_redis_connected Redis connection status\n")
		fmt.Fprintf(w, "# TYPE codero_redis_connected gauge\n")
		fmt.Fprintf(w, "codero_redis_connected 1\n")
	} else {
		fmt.Fprintf(w, "codero_redis_connected 0\n")
	}
}

// handleAgentMetrics returns effectiveness metrics per agent and project.
// This implements the /api/v1/agent-metrics endpoint from the observability contract.
func (o *ObservabilityServer) handleAgentMetrics(w http.ResponseWriter, r *http.Request) {
	if o.db == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	repo := r.URL.Query().Get("repo")

	// Query branches for metrics
	var rows *sql.Rows
	var err error

	if repo != "" {
		rows, err = o.db.QueryContext(r.Context(), `
			SELECT repo, state, COUNT(*) as count,
			       SUM(CASE WHEN approved = 1 THEN 1 ELSE 0 END) as approved,
			       SUM(CASE WHEN ci_green = 1 THEN 1 ELSE 0 END) as ci_green,
			       SUM(retry_count) as total_retries
			FROM branch_states
			WHERE repo = ?
			GROUP BY repo, state`, repo)
	} else {
		rows, err = o.db.QueryContext(r.Context(), `
			SELECT repo, state, COUNT(*) as count,
			       SUM(CASE WHEN approved = 1 THEN 1 ELSE 0 END) as approved,
			       SUM(CASE WHEN ci_green = 1 THEN 1 ELSE 0 END) as ci_green,
			       SUM(retry_count) as total_retries
			FROM branch_states
			GROUP BY repo, state`)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("query failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Aggregate metrics by repo
	metrics := make(map[string]map[string]int)
	for rows.Next() {
		var r string
		var st string
		var count, approved, ciGreen, totalRetries int
		if err := rows.Scan(&r, &st, &count, &approved, &ciGreen, &totalRetries); err != nil {
			http.Error(w, fmt.Sprintf("scan failed: %v", err), http.StatusInternalServerError)
			return
		}
		if metrics[r] == nil {
			metrics[r] = make(map[string]int)
		}
		metrics[r][st] = count
		metrics[r]["total_approved"] += approved
		metrics[r]["total_ci_green"] += ciGreen
		metrics[r]["total_retries"] += totalRetries
	}
	if err := rows.Err(); err != nil {
		http.Error(w, fmt.Sprintf("row iteration failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Build response
	response := map[string]interface{}{
		"repos":        metrics,
		"generated_at": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleReady returns readiness status. The daemon is ready only after the
// full bootstrap sequence completes (PID, SQLite, signals, components, obs).
// Per the daemon v2 spec, this endpoint returns 503 until MarkReady is called.
func (o *ObservabilityServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if !o.ready.Load() {
		http.Error(w, "daemon not ready", http.StatusServiceUnavailable)
		return
	}

	// In degraded mode (Redis down), report ready but degraded.
	if IsDegraded() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("degraded"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// MarkReady signals that the daemon bootstrap is complete and the daemon
// is ready to serve. Called after all startup steps succeed.
func (o *ObservabilityServer) MarkReady() {
	o.ready.Store(true)
}

// MarkNotReady clears readiness, used during shutdown to stop serving
// new requests before draining existing ones.
func (o *ObservabilityServer) MarkNotReady() {
	o.ready.Store(false)
}

// DefaultObservabilityPort is the default port for observability endpoints.
const DefaultObservabilityPort = configpkg.DefaultAPIServerPortString

// handleGate returns the current pre-commit gate progress as JSON.
// Reads .codero/gate-heartbeat/progress.env written by two-pass-review.sh.
// This endpoint provides dashboard UI parity with the CLI progress bar.
//
// Response fields match the shared progress contract:
//
//	PROGRESS_BAR, CURRENT_GATE, COPILOT_STATUS, LITELLM_STATUS
func (o *ObservabilityServer) handleGate(w http.ResponseWriter, r *http.Request) {
	progressFile := filepath.Join(o.repoPath, ".codero", "gate-heartbeat", "progress.env")
	data, err := os.ReadFile(progressFile)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":         "no_run",
				"progress_bar":   gate.RenderBar("pending", "pending", "none"),
				"current_gate":   "none",
				"copilot_status": "pending",
				"litellm_status": "pending",
				"generated_at":   time.Now().Format(time.RFC3339),
			})
			return
		}
		http.Error(w, fmt.Sprintf("read progress file: %v", err), http.StatusInternalServerError)
		return
	}

	raw := string(data)
	result := gate.ParseProgressEnv(raw)
	bar := result.ProgressBar
	if bar == "" {
		bar = gate.RenderBar(result.CopilotStatus, result.LiteLLMStatus, result.CurrentGate)
	}
	if result.Comments == nil {
		result.Comments = []string{}
	}

	// Extract UPDATED_AT directly instead of parsing all fields a second time.
	var updatedAt string
	for _, line := range strings.Split(raw, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "UPDATED_AT" {
			updatedAt = strings.TrimSpace(v)
			break
		}
	}

	resp := map[string]interface{}{
		"run_id":         result.RunID,
		"status":         string(result.Status),
		"progress_bar":   bar,
		"current_gate":   result.CurrentGate,
		"copilot_status": result.CopilotStatus,
		"litellm_status": result.LiteLLMStatus,
		"comments":       result.Comments,
		"elapsed_sec":    result.ElapsedSec,
		"updated_at":     updatedAt,
		"generated_at":   time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
