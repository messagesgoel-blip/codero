package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestIsAllowedHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"allowed", "api.example.com", true},
		{"allowed_status", "status.example.com", true},
		{"forbidden", "evil.example.com", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &url.URL{Host: tt.host}
			if got := isAllowedHost(u); got != tt.want {
				t.Errorf("isAllowedHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestFetchURL_ForbiddenHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?url=https://evil.com/secret", nil)
	w := httptest.NewRecorder()
	FetchURL(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestFetchURL_MissingURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	FetchURL(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestFetchURL_InvalidURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?url=%ZZ", nil)
	w := httptest.NewRecorder()
	FetchURL(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestFetchURL_AllowedHost(t *testing.T) {
	// Set up a test server and temporarily add its host to the allowlist.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello from upstream"))
	}))
	defer ts.Close()

	tsURL, _ := url.Parse(ts.URL)
	allowedHosts[tsURL.Hostname()] = true
	defer delete(allowedHosts, tsURL.Hostname())

	// Also point fetchClient at our test server's transport for localhost.
	origClient := fetchClient
	fetchClient = ts.Client()
	defer func() { fetchClient = origClient }()

	req := httptest.NewRequest(http.MethodGet, "/?url="+url.QueryEscape(ts.URL+"/data"), nil)
	w := httptest.NewRecorder()
	FetchURL(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != "hello from upstream" {
		t.Errorf("body = %q, want %q", got, "hello from upstream")
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/plain")
	}
}

func TestFetchURL_UpstreamError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal failure", http.StatusInternalServerError)
	}))
	defer ts.Close()

	tsURL, _ := url.Parse(ts.URL)
	allowedHosts[tsURL.Hostname()] = true
	defer delete(allowedHosts, tsURL.Hostname())

	origClient := fetchClient
	fetchClient = ts.Client()
	defer func() { fetchClient = origClient }()

	req := httptest.NewRequest(http.MethodGet, "/?url="+url.QueryEscape(ts.URL+"/fail"), nil)
	w := httptest.NewRecorder()
	FetchURL(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
