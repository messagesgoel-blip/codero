package dashboard_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/dashboard"
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

func TestTrackingConfig_Get_DegradesOnRegistryRefreshFailure(t *testing.T) {
	t.Setenv("CODERO_USER_CONFIG_DIR", t.TempDir())
	cfgDir := os.Getenv("CODERO_USER_CONFIG_DIR")

	uc := &config.UserConfig{
		Version:        1,
		DisabledAgents: []string{"ghost"},
		Registry: config.AgentRegistry{
			LastScan: time.Now().UTC(),
			Agents: map[string]config.RegisteredAgent{
				"ghost": {
					AgentID:  "ghost",
					Disabled: true,
				},
			},
		},
	}
	if err := uc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "bin"), []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write bin blocker: %v", err)
	}

	h, _ := newTestHandler(t)
	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/tracking-config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		DisabledAgents []string                 `json:"disabled_agents"`
		Agents         []config.RegisteredAgent `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.DisabledAgents) != 1 || resp.DisabledAgents[0] != "ghost" {
		t.Fatalf("disabled_agents=%v, want [ghost]", resp.DisabledAgents)
	}
	if len(resp.Agents) != 1 || resp.Agents[0].AgentID != "ghost" {
		t.Fatalf("agents=%v, want cached ghost agent", resp.Agents)
	}
}

func TestAgents_PreservesDisabledStateWhenRegistryRefreshFails(t *testing.T) {
	t.Setenv("CODERO_USER_CONFIG_DIR", t.TempDir())
	cfgDir := os.Getenv("CODERO_USER_CONFIG_DIR")

	uc := &config.UserConfig{
		Version:        1,
		DisabledAgents: []string{"ghost"},
		Registry: config.AgentRegistry{
			LastScan: time.Now().UTC(),
			Agents: map[string]config.RegisteredAgent{
				"ghost": {
					AgentID:  "ghost",
					Disabled: true,
				},
			},
		},
	}
	if err := uc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "bin"), []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write bin blocker: %v", err)
	}

	h, _ := newTestHandler(t)

	rec := doRequest(t, h, http.MethodGet, "/api/v1/dashboard/agents", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Agents []dashboard.AgentRosterRow `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Agents) != 1 {
		t.Fatalf("agents=%d, want 1", len(resp.Agents))
	}
	if resp.Agents[0].AgentID != "ghost" {
		t.Fatalf("agent_id=%q, want ghost", resp.Agents[0].AgentID)
	}
	if resp.Agents[0].Status != "disabled" {
		t.Fatalf("status=%q, want disabled", resp.Agents[0].Status)
	}
}
