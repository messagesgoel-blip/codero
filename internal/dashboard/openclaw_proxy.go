// internal/dashboard/openclaw_proxy.go
package dashboard

import (
	"io"
	"net/http"
	"os"
	"time"

	loglib "github.com/codero/codero/internal/log"
)

// handleOpenClawQuery proxies POST /api/v1/openclaw/query to the openclaw-adapter sidecar.
func (h *Handler) handleOpenClawQuery(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	adapterURL := os.Getenv("OPENCLAW_ADAPTER_URL")
	if adapterURL == "" {
		adapterURL = "http://127.0.0.1:8112"
	}
	target := adapterURL + "/query"

	ctx := r.Context()
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, r.Body)
	if err != nil {
		loglib.Error("openclaw proxy: build request", "error", err)
		writeError(w, http.StatusBadGateway, "OpenClaw unavailable", "")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		loglib.Error("openclaw proxy: request failed", "error", err)
		writeError(w, http.StatusBadGateway, "OpenClaw unavailable", "")
		return
	}
	defer resp.Body.Close()

	// Forward status and body from adapter.
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
