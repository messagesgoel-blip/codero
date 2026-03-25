package tmux

import (
	"context"
	"testing"
)

func TestSessionName(t *testing.T) {
	tests := []struct {
		agentID   string
		sessionID string
		want      string
	}{
		{"claude", "a1b2c3d4-5678-9012-3456-789012345678", "codero-claude-a1b2c3d4"},
		{"codex", "12345678", "codero-codex-12345678"},
		{"x", "short", "codero-x-short"},
	}
	for _, tt := range tests {
		got := SessionName(tt.agentID, tt.sessionID)
		if got != tt.want {
			t.Errorf("SessionName(%q, %q) = %q, want %q", tt.agentID, tt.sessionID, got, tt.want)
		}
	}
}

func TestParseSessionName(t *testing.T) {
	tests := []struct {
		name      string
		wantAgent string
		wantUUID  string
		wantOK    bool
	}{
		{"codero-claude-a1b2c3d4", "claude", "a1b2c3d4", true},
		{"codero-test-agent-12345678", "test-agent", "12345678", true},
		{"random-session", "", "", false},
		{"codero-", "", "", false},
	}
	for _, tt := range tests {
		agent, uuid, ok := ParseSessionName(tt.name)
		if ok != tt.wantOK {
			t.Errorf("ParseSessionName(%q): ok = %v, want %v", tt.name, ok, tt.wantOK)
			continue
		}
		if agent != tt.wantAgent {
			t.Errorf("ParseSessionName(%q): agent = %q, want %q", tt.name, agent, tt.wantAgent)
		}
		if uuid != tt.wantUUID {
			t.Errorf("ParseSessionName(%q): uuid = %q, want %q", tt.name, uuid, tt.wantUUID)
		}
	}
}

func TestMockExecutor(t *testing.T) {
	ctx := context.Background()
	mock := NewMockExecutor()

	name := "codero-test-12345678"

	// Initially no sessions
	if mock.HasSession(ctx, name) {
		t.Error("should not have session initially")
	}

	// Create session
	if err := mock.NewSession(ctx, name, t.TempDir()); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if !mock.HasSession(ctx, name) {
		t.Error("should have session after creation")
	}

	// Send keys
	if err := mock.SendKeys(ctx, name, "echo hello"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	if len(mock.SentKeys) != 1 || mock.SentKeys[0].Command != "echo hello" {
		t.Errorf("SentKeys: %+v", mock.SentKeys)
	}

	// Capture pane
	mock.PaneContent[name] = "output"
	content, err := mock.CapturePane(ctx, name)
	if err != nil {
		t.Fatalf("CapturePane: %v", err)
	}
	if content != "output" {
		t.Errorf("CapturePane = %q, want output", content)
	}

	// List sessions
	sessions, err := mock.ListCoderoSessions(ctx)
	if err != nil {
		t.Fatalf("ListCoderoSessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0] != name {
		t.Errorf("ListCoderoSessions: %v", sessions)
	}

	// Kill session
	if err := mock.KillSession(ctx, name); err != nil {
		t.Fatalf("KillSession: %v", err)
	}
	if mock.HasSession(ctx, name) {
		t.Error("should not have session after kill")
	}
}
