// services/openclaw-adapter/main.go
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type adapterConfig struct {
	Addr         string // listen address, default ":8112"
	StateURL     string // codero state endpoint
	LiteLLMURL   string // LiteLLM base URL
	LiteLLMModel string // model name
	LiteLLMKey   string // API key
	AuditLogPath string // JSONL audit log path
}

func loadConfig() adapterConfig {
	return adapterConfig{
		Addr:         envOr("OPENCLAW_ADDR", ":8112"),
		StateURL:     envOr("CODERO_STATE_URL", "http://localhost:8080/api/v1/openclaw/state"),
		LiteLLMURL:   envOr("LITELLM_URL", "http://localhost:4000"),
		LiteLLMModel: envOr("LITELLM_MODEL", "qwen3-coder-plus"),
		LiteLLMKey:   envOr("LITELLM_API_KEY", ""),
		AuditLogPath: envOr("OPENCLAW_AUDIT_LOG", "/data/logs/openclaw-audit.jsonl"),
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

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/query", h.handleQuery)

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
