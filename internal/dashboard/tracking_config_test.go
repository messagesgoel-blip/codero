package dashboard_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// TestTrackingConfig_EnvVars_EmptyKey verifies that an empty env_vars key is rejected.
func TestTrackingConfig_EnvVars_EmptyKey(t *testing.T) {
	t.Setenv("CODERO_USER_CONFIG_DIR", t.TempDir())
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(map[string]any{
		"agent_id": "test-agent",
		"env_vars": map[string]string{"": "val"},
	})
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/tracking-config", bytes.NewReader(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct{ Error string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error != "env_vars key cannot be empty" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}

// TestTrackingConfig_EnvVars_KeyContainsEquals verifies that a key with '=' is rejected.
func TestTrackingConfig_EnvVars_KeyContainsEquals(t *testing.T) {
	t.Setenv("CODERO_USER_CONFIG_DIR", t.TempDir())
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(map[string]any{
		"agent_id": "test-agent",
		"env_vars": map[string]string{"FOO=BAR": "val"},
	})
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/tracking-config", bytes.NewReader(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct{ Error string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error != "env_vars key cannot contain '='" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}

// TestTrackingConfig_EnvVars_KeyContainsNUL verifies that a key with NUL byte is rejected.
func TestTrackingConfig_EnvVars_KeyContainsNUL(t *testing.T) {
	t.Setenv("CODERO_USER_CONFIG_DIR", t.TempDir())
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(map[string]any{
		"agent_id": "test-agent",
		"env_vars": map[string]string{"FOO\x00BAR": "val"},
	})
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/tracking-config", bytes.NewReader(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct{ Error string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error != "env_vars cannot contain NUL bytes" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}

// TestTrackingConfig_EnvVars_ValueContainsNUL verifies that a value with NUL byte is rejected.
func TestTrackingConfig_EnvVars_ValueContainsNUL(t *testing.T) {
	t.Setenv("CODERO_USER_CONFIG_DIR", t.TempDir())
	h, _ := newTestHandler(t)

	body, _ := json.Marshal(map[string]any{
		"agent_id": "test-agent",
		"env_vars": map[string]string{"VALID_KEY": "val\x00ue"},
	})
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/tracking-config", bytes.NewReader(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct{ Error string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error != "env_vars cannot contain NUL bytes" {
		t.Fatalf("unexpected error: %q", resp.Error)
	}
}

// TestTrackingConfig_EnvVars_ValidKeyValue verifies that a valid key/value pair is accepted.
func TestTrackingConfig_EnvVars_ValidKeyValue(t *testing.T) {
	t.Setenv("CODERO_USER_CONFIG_DIR", t.TempDir())
	// Seed a minimal config so DiscoverAgents doesn't fail.
	cfgDir := os.Getenv("CODERO_USER_CONFIG_DIR")
	os.WriteFile(cfgDir+"/config.yaml", []byte("disabled_agents: []\n"), 0o644)

	h, _ := newTestHandler(t)

	body, _ := json.Marshal(map[string]any{
		"agent_id": "test-agent",
		"env_vars": map[string]string{"MY_TOKEN": "secret123"},
	})
	rec := doRequest(t, h, http.MethodPut, "/api/v1/dashboard/tracking-config", bytes.NewReader(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
