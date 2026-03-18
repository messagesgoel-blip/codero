// Package handlers provides HTTP request handlers.
package handlers

import (
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var fileTmpl = template.Must(template.New("file").Parse("{{.}}"))

// ReadFile serves a file from the directory configured by HANDLERS_STATIC_ROOT.
// The path parameter is sanitised to prevent directory traversal.
func ReadFile(w http.ResponseWriter, r *http.Request) {
	staticRoot := os.Getenv("HANDLERS_STATIC_ROOT")
	if staticRoot == "" {
		http.Error(w, "static root not configured", http.StatusInternalServerError)
		return
	}
	rel := filepath.FromSlash(filepath.Clean("/" + strings.Trim(r.URL.Query().Get("path"), "/")))
	abs := filepath.Join(staticRoot, rel)
	if !strings.HasPrefix(abs, staticRoot+string(filepath.Separator)) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(abs) //nolint:gosec
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = fileTmpl.Execute(w, string(data))
}
