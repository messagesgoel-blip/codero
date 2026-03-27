package tui

import "testing"

func TestIsSensitiveVar(t *testing.T) {
	tests := []struct {
		name     string
		varName  string
		expected bool
	}{
		{"explicit key", "CODERO_LITELLM_API_KEY", true},
		{"explicit secret", "CODERO_HEARTBEAT_SECRET", true},
		{"explicit pass", "CODERO_REDIS_PASS", true},
		{"suffix KEY", "CODERO_SOME_KEY", true},
		{"suffix SECRET", "CODERO_WEBHOOK_SECRET", true},
		{"suffix TOKEN", "CODERO_GITHUB_TOKEN", true},
		{"suffix PASSWORD", "CODERO_DB_PASSWORD", true},
		{"suffix PASS", "CODERO_SOME_PASS", true},
		{"numeric key exemption", "CODERO_API_KEY_2", false},
		{"numeric key exemption 10", "CODERO_API_KEY_10", false},
		{"normal var", "CODERO_TUI_POLL_INTERVAL", false},
		{"repo path", "CODERO_REPO_PATH", false},
		{"empty", "", false},
		{"KEY in middle", "CODERO_KEY_RING", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSensitiveVar(tt.varName); got != tt.expected {
				t.Errorf("isSensitiveVar(%q) = %v, want %v", tt.varName, got, tt.expected)
			}
		})
	}
}

func TestGroupConfigVars(t *testing.T) {
	vars := map[string]string{
		"CODERO_REPO_PATH":         "/some/path",
		"CODERO_BRANCH":            "main",
		"CODERO_TUI_POLL_INTERVAL": "500ms",
		"CODERO_LITELLM_URL":       "http://localhost:4000",
		"CODERO_UNKNOWN_THING":     "value",
	}
	sections := groupConfigVars(vars)
	if len(sections) == 0 {
		t.Fatal("expected at least one section")
	}
	found := false
	for _, s := range sections {
		if s.Title == "SESSION CONTEXT" {
			for _, kv := range s.Vars {
				if kv.Key == "CODERO_REPO_PATH" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("CODERO_REPO_PATH not found in SESSION CONTEXT section")
	}
	// Verify TUI section
	tuiFound := false
	for _, s := range sections {
		if s.Title == "TUI SETTINGS" {
			for _, kv := range s.Vars {
				if kv.Key == "CODERO_TUI_POLL_INTERVAL" {
					tuiFound = true
				}
			}
		}
	}
	if !tuiFound {
		t.Error("CODERO_TUI_POLL_INTERVAL not found in TUI SETTINGS section")
	}
}
