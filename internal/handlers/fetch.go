// Package handlers provides HTTP request handlers.
package handlers

import (
	"io"
	"net/http"
	"net/url"
)

// allowedHosts is the allowlist of hosts that FetchURL may proxy to.
var allowedHosts = map[string]bool{
	"api.example.com":    true,
	"status.example.com": true,
}

// FetchURL proxies a request to an allowlisted remote host.
func FetchURL(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	parsed, err := url.Parse(raw)
	if err != nil || !allowedHosts[parsed.Hostname()] {
		http.Error(w, "forbidden or invalid url", http.StatusForbidden)
		return
	}
	// nosemgrep: go.lang.security.injection.tainted-url-host.tainted-url-host
	resp, err := http.Get(parsed.String()) //nolint:noctx
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	io.Copy(w, resp.Body) //nolint:errcheck
}
