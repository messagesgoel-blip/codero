package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/redis"
	"github.com/codero/codero/internal/scheduler"
)

// ObservabilityServer provides HTTP endpoints for health, queue status, and metrics.
type ObservabilityServer struct {
	server      *http.Server
	redisClient *redis.Client
	queue       *scheduler.Queue
	slotCounter *scheduler.SlotCounter
	startTime   time.Time
	mu          sync.RWMutex
}

// NewObservabilityServer creates a new observability server.
func NewObservabilityServer(redisClient *redis.Client, queue *scheduler.Queue, slotCounter *scheduler.SlotCounter, port string) *ObservabilityServer {
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	obs := &ObservabilityServer{
		server:      server,
		redisClient: redisClient,
		queue:       queue,
		slotCounter: slotCounter,
		startTime:   time.Now(),
	}

	// Register routes
	mux.HandleFunc("/health", obs.handleHealth)
	mux.HandleFunc("/queue", obs.handleQueue)
	mux.HandleFunc("/metrics", obs.handleMetrics)
	mux.HandleFunc("/ready", obs.handleReady)

	return obs
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

// Stop gracefully shuts down the observability server.
func (o *ObservabilityServer) Stop(ctx context.Context) error {
	return o.server.Shutdown(ctx)
}

// handleHealth returns service health status.
func (o *ObservabilityServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := map[string]interface{}{
		"status":         "ok",
		"uptime_seconds": time.Since(o.startTime).Seconds(),
		"version":        "dev",
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
	fmt.Fprintf(w, "# HELP codero_uptime_seconds Seconds since service start\n")
	fmt.Fprintf(w, "# TYPE codero_uptime_seconds gauge\n")
	fmt.Fprintf(w, "codero_uptime_seconds %.2f\n", uptime)

	// Queue metrics
	if o.queue != nil {
		// Get queue length for a sample repo if available
		// In real implementation, we'd iterate over all repos
		fmt.Fprintf(w, "# HELP codero_queue_length Number of items in queue\n")
		fmt.Fprintf(w, "# TYPE codero_queue_length gauge\n")
	}

	// Slot counter metrics
	if o.slotCounter != nil {
		fmt.Fprintf(w, "# HELP codero_active_slots Current number of active dispatch slots\n")
		fmt.Fprintf(w, "# TYPE codero_active_slots gauge\n")
		// Would need to iterate repos for actual values
		fmt.Fprintf(w, "# HELP codero_slot_limit Maximum allowed concurrent dispatches\n")
		fmt.Fprintf(w, "# TYPE codero_slot_limit gauge\n")
	}

	// Redis metrics
	rc := o.redisClient.Unwrap()
	if err := rc.Ping(ctx).Err(); err == nil {
		fmt.Fprintf(w, "# HELP codero_redis_connected Redis connection status\n")
		fmt.Fprintf(w, "# TYPE codero_redis_connected gauge\n")
		fmt.Fprintf(w, "codero_redis_connected 1\n")
	} else {
		fmt.Fprintf(w, "codero_redis_connected 0\n")
	}
}

// handleReady returns readiness status (for Kubernetes readiness probe).
func (o *ObservabilityServer) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check Redis
	rc := o.redisClient.Unwrap()
	if err := rc.Ping(ctx).Err(); err != nil {
		http.Error(w, "Redis not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// DefaultObservabilityPort is the default port for observability endpoints.
const DefaultObservabilityPort = "8080"
