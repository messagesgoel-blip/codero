package dashboard_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/gate"
)

// TestGetGateConfig_DefaultsWhenNoFile verifies the gate-config endpoint
// returns all spec keys with default values when no config file exists.
func TestGetGateConfig_DefaultsWhenNoFile(t *testing.T) {
	h, _ := newTestHandler(t)

	// Clear all gate env vars so defaults are used.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/settings/gate-config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp dashboard.GateConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Checks) == 0 {
		t.Fatal("expected non-empty checks array")
	}
	if len(resp.AISettings) == 0 {
		t.Fatal("expected non-empty ai_settings")
	}
	if len(resp.AlwaysOn) != 3 {
		t.Errorf("expected 3 always-on checks, got %d", len(resp.AlwaysOn))
	}

	// Verify all checks have source=default.
	for _, c := range resp.Checks {
		if c.Source != "default" {
			t.Errorf("check %s: expected source=default, got %s", c.EnvVar, c.Source)
		}
	}
}

// TestGetGateConfig_ReflectsFileValues verifies the dashboard reads config from file.
func TestGetGateConfig_ReflectsFileValues(t *testing.T) {
	// Create a config file in a temp HOME.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".codero")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.env")
	if err := os.WriteFile(cfgPath, []byte("CODERO_COPILOT_ENABLED=true\nCODERO_AI_QUORUM=3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clear env vars so file is the source.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/settings/gate-config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d; body: %s", w.Code, w.Body.String())
	}

	var resp dashboard.GateConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// Find COPILOT in checks.
	found := false
	for _, c := range resp.Checks {
		if c.EnvVar == "CODERO_COPILOT_ENABLED" {
			found = true
			if c.CurrentValue != "true" {
				t.Errorf("COPILOT value: got %q, want true", c.CurrentValue)
			}
			if c.Source != "config_file" {
				t.Errorf("COPILOT source: got %s, want config_file", c.Source)
			}
		}
	}
	if !found {
		t.Error("CODERO_COPILOT_ENABLED not found in checks")
	}

	// AI_QUORUM should be in ai_settings.
	if resp.AISettings["CODERO_AI_QUORUM"] != "3" {
		t.Errorf("AI_QUORUM: got %q, want 3", resp.AISettings["CODERO_AI_QUORUM"])
	}
}

// TestPutGateConfigVar_UpdatesFile verifies writing a single config var.
func TestPutGateConfigVar_UpdatesFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}

	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"value":"true"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/dashboard/settings/gate-config/CODERO_COPILOT_ENABLED",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d; body: %s", w.Code, w.Body.String())
	}

	// Verify file was written.
	cfgPath := filepath.Join(tmpHome, ".codero", "config.env")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	if !bytes.Contains(data, []byte("CODERO_COPILOT_ENABLED=true")) {
		t.Errorf("file missing updated value: %s", string(data))
	}

	// Read-after-write: GET should reflect the change.
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/settings/gate-config", nil)
	wGet := httptest.NewRecorder()
	mux.ServeHTTP(wGet, reqGet)

	var resp dashboard.GateConfigResponse
	if err := json.Unmarshal(wGet.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, c := range resp.Checks {
		if c.EnvVar == "CODERO_COPILOT_ENABLED" && c.CurrentValue != "true" {
			t.Errorf("read-after-write: COPILOT value=%q, want true", c.CurrentValue)
		}
	}
}

// TestPutGateConfigVar_UnknownVar returns 404.
func TestPutGateConfigVar_UnknownVar(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"value":"true"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/dashboard/settings/gate-config/NOT_REAL",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

// TestPutGateConfigVar_InvalidValue returns 422.
func TestPutGateConfigVar_InvalidValue(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"value":"maybe"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/dashboard/settings/gate-config/CODERO_COPILOT_ENABLED",
		bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422; body: %s", w.Code, w.Body.String())
	}
}

// TestGetGateConfig_DriftDetection verifies config drift is reported.
func TestGetGateConfig_DriftDetection(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cfgDir := filepath.Join(tmpHome, ".codero")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.env"),
		[]byte("CODERO_COPILOT_ENABLED=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clear all then set one to create drift.
	for _, entry := range gate.Registry {
		t.Setenv(entry.EnvVar, "")
	}
	t.Setenv("CODERO_COPILOT_ENABLED", "true")

	h, _ := newTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/settings/gate-config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp dashboard.GateConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.ConfigDrifts) != 1 {
		t.Fatalf("expected 1 drift, got %d", len(resp.ConfigDrifts))
	}
	if resp.ConfigDrifts[0].EnvVar != "CODERO_COPILOT_ENABLED" {
		t.Errorf("drift var: %s", resp.ConfigDrifts[0].EnvVar)
	}
}
