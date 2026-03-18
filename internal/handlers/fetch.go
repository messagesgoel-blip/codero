// Package handlers provides HTTP request handlers.
package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// allowedHosts is the allowlist of hosts that FetchURL may proxy to.
var allowedHosts = map[string]bool{
	"api.example.com":    true,
	"status.example.com": true,
}

func isAllowedHost(u *url.URL) bool {
	return allowedHosts[u.Hostname()]
}

var fetchClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !isAllowedHost(req.URL) {
			return fmt.Errorf("redirect to non-allowlisted host %q denied", req.URL.Hostname())
		}
		return nil
	},
}

// FetchURL proxies a request to an allowlisted remote host.
func FetchURL(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	parsed, err := url.Parse(raw)
	if err != nil || !isAllowedHost(parsed) {
		http.Error(w, "forbidden or invalid url", http.StatusForbidden)
		return
	}
	// nosemgrep: go.lang.security.injection.tainted-url-host.tainted-url-host
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, parsed.String(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := fetchClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		// Response already started; log only.
		_ = err
	}
}
