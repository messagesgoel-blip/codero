package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HANDLERS_STATIC_ROOT", dir)

	req := httptest.NewRequest(http.MethodGet, "/?path=hello.txt", nil)
	w := httptest.NewRecorder()
	ReadFile(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != "world" {
		t.Errorf("body = %q, want %q", got, "world")
	}
}

func TestReadFile_NoStaticRoot(t *testing.T) {
	t.Setenv("HANDLERS_STATIC_ROOT", "")

	req := httptest.NewRequest(http.MethodGet, "/?path=anything", nil)
	w := httptest.NewRecorder()
	ReadFile(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestReadFile_DirectoryTraversal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HANDLERS_STATIC_ROOT", dir)

	paths := []string{
		"../../breakout",
		"../../../breakout",
		"..%2f..%2fbreakout",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?path="+p, nil)
			w := httptest.NewRecorder()
			ReadFile(w, req)

			// Should be 400 (traversal blocked) or 500 (file not found after sanitisation)
			if w.Code == http.StatusOK {
				t.Errorf("traversal path %q returned 200; expected rejection", p)
			}
		})
	}
}

func TestReadFile_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HANDLERS_STATIC_ROOT", dir)

	req := httptest.NewRequest(http.MethodGet, "/?path=nonexistent.txt", nil)
	w := httptest.NewRecorder()
	ReadFile(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestReadFile_ContentTypeDetection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HANDLERS_STATIC_ROOT", dir)

	// Write an HTML file — DetectContentType uses content sniffing, not extension.
	htmlContent := []byte("<html><body>hello</body></html>")
	if err := os.WriteFile(filepath.Join(dir, "page.html"), htmlContent, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/?path=page.html", nil)
	w := httptest.NewRecorder()
	ReadFile(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html; charset=utf-8")
	}
}

func TestReadFile_Subdirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "data.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HANDLERS_STATIC_ROOT", dir)

	req := httptest.NewRequest(http.MethodGet, "/?path=sub/data.json", nil)
	w := httptest.NewRecorder()
	ReadFile(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != `{"ok":true}` {
		t.Errorf("body = %q, want %q", got, `{"ok":true}`)
	}
}
