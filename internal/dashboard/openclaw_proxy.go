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
	h.proxyToAdapter(w, r, "openclaw proxy")
}

// handleChatProxy forwards legacy POST /api/v1/dashboard/chat to the OpenClaw adapter.
// The ChatRequest body shape (prompt, conversation_id) matches the adapter's /query shape,
// so the body is forwarded as-is.
func (h *Handler) handleChatProxy(w http.ResponseWriter, r *http.Request) {
	h.proxyToAdapter(w, r, "chat proxy")
}

// proxyToAdapter contains the shared reverse-proxy to the OpenClaw adapter sidecar.
func (h *Handler) proxyToAdapter(w http.ResponseWriter, r *http.Request, logPrefix string) {
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
		loglib.Error(logPrefix+": build request", "error", err)
		writeError(w, http.StatusBadGateway, "OpenClaw unavailable", "")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		loglib.Error(logPrefix+": request failed", "error", err)
		writeError(w, http.StatusBadGateway, "OpenClaw unavailable", "")
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		loglib.Error(logPrefix+": response copy failed", "error", err, "status", resp.StatusCode)
	}
}

// handleOpenClawAudit proxies GET /api/v1/openclaw/audit to the adapter's /audit endpoint.
func (h *Handler) handleOpenClawAudit(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	adapterURL := os.Getenv("OPENCLAW_ADAPTER_URL")
	if adapterURL == "" {
		adapterURL = "http://127.0.0.1:8112"
	}
	target := adapterURL + "/audit"
	if q := r.URL.RawQuery; q != "" {
		target += "?" + q
	}

	ctx := r.Context()
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		loglib.Error("audit proxy: build request", "error", err)
		writeError(w, http.StatusBadGateway, "OpenClaw unavailable", "")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		loglib.Error("audit proxy: request failed", "error", err)
		writeError(w, http.StatusBadGateway, "OpenClaw unavailable", "")
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		loglib.Error("audit proxy: response copy failed", "error", err, "status", resp.StatusCode)
	}
}
