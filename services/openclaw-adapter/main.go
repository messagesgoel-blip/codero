// services/openclaw-adapter/main.go
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

type adapterConfig struct {
	Addr             string // listen address, default ":8112"
	StateURL         string // codero state endpoint
	BaseURL          string // codero base URL (for session lookup)
	BridgePath       string // path to agent-tmux-bridge binary
	LiteLLMURL       string // LiteLLM base URL
	LiteLLMModel     string // model name
	LiteLLMKey       string // API key
	AuditLogPath     string // JSONL audit log path
	CoderoPath       string // path to codero binary, empty = search PATH
	CoderoConfigPath string // path to codero config yaml
	ObserverPollSec  int    // poll interval in seconds, default 5
}

func loadConfig() adapterConfig {
	pollSec := 5
	if v := os.Getenv("OBSERVER_POLL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pollSec = n
		}
	}
	return adapterConfig{
		Addr:             envOr("OPENCLAW_ADDR", ":8112"),
		StateURL:         envOr("CODERO_STATE_URL", "http://localhost:8080/api/v1/openclaw/state"),
		BaseURL:          envOr("CODERO_BASE_URL", "http://localhost:8080"),
		BridgePath:       envOr("BRIDGE_PATH", "/srv/storage/shared/tools/bin/agent-tmux-bridge"),
		LiteLLMURL:       envOr("LITELLM_URL", "http://localhost:4000"),
		LiteLLMModel:     envOr("LITELLM_MODEL", "qwen3-coder-plus"),
		LiteLLMKey:       envOr("LITELLM_API_KEY", ""),
		AuditLogPath:     envOr("OPENCLAW_AUDIT_LOG", "/data/logs/openclaw-audit.jsonl"),
		CoderoPath:       envOr("CODERO_BIN", ""),
		CoderoConfigPath: envOr("CODERO_CONFIG", ""),
		ObserverPollSec:  pollSec,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	cfg := loadConfig()
	h := newHandler(cfg)

	// Start TASK_COMPLETE observer.
	obs := NewObserver(observerConfig{
		BaseURL:          cfg.BaseURL,
		BridgePath:       cfg.BridgePath,
		CoderoPath:       cfg.CoderoPath,
		CoderoConfigPath: cfg.CoderoConfigPath,
		PollInterval:     time.Duration(cfg.ObserverPollSec) * time.Second,
	}, h.auditFile, &h.auditMu)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/query", h.handleQuery)
	mux.HandleFunc("/deliver", h.handleDeliver)
	mux.HandleFunc("/audit", h.handleAudit)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go obs.Start(ctx)

	go func() {
		log.Printf("openclaw-adapter listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}
