package event

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestReplyToDirectClient_Deliver_Bridge(t *testing.T) {
	oldExec := execPTYBridgeCommand
	defer func() { execPTYBridgeCommand = oldExec }()

	var gotArgs []string
	execPTYBridgeCommand = func(ctx context.Context, args ...string) ([]byte, error) {
		gotArgs = append([]string{}, args...)
		return []byte("ok"), nil
	}

	client := NewReplyToDirectClient()

	payload := FeedbackInjectPayload{
		AssignmentID: "assign-1",
		SessionID:    "sess-1",
		Findings: []FeedbackItem{
			{File: "main.go", Line: 10, Message: "style issue"},
		},
	}
	pBody, _ := json.Marshal(payload)

	env := Envelope{
		EventID: "evt-1",
		Type:    EventTypeFeedbackInject,
		Source:  "codero",
		ReplyTo: ReplyToEndpoint{
			Type:      "openclaw_session",
			SessionID: "sess-1",
			TmuxName:  "codero-agent-1-sess1",
			Profile:   "codex",
		},
		Timestamp:     time.Now().UTC(),
		Payload:       pBody,
		SchemaVersion: CurrentSchemaVersion,
	}

	if err := client.Deliver(context.Background(), env); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	// Verify the bridge was called with correct arguments.
	got := strings.Join(gotArgs, " ")
	wantArgs := []string{
		"deliver",
		"--session codero-agent-1-sess1",
		"--profile codex",
		"--message",
	}

	for _, want := range wantArgs {
		if !strings.Contains(got, want) {
			t.Errorf("bridge call missing %q, got: %q", want, got)
		}
	}

	if !strings.Contains(got, "style issue") {
		t.Errorf("bridge call missing finding message, got: %q", got)
	}
}

func TestReplyToDirectClient_Deliver_BridgeFailure(t *testing.T) {
	oldExec := execPTYBridgeCommand
	defer func() { execPTYBridgeCommand = oldExec }()

	execPTYBridgeCommand = func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte("injection failed: terminal busy"), errors.New("exit status 1")
	}

	client := NewReplyToDirectClient()
	env := Envelope{
		EventID:   "evt-fail",
		Type:      EventTypeFeedbackInject,
		Source:    "codero",
		Timestamp: time.Now().UTC(),
		ReplyTo: ReplyToEndpoint{
			Type:      "openclaw_session",
			SessionID: "sess-1",
			TmuxName:  "codero-agent-1-sess1",
			Profile:   "codex",
		},
		Payload:       []byte("{}"),
		SchemaVersion: CurrentSchemaVersion,
	}

	err := client.Deliver(context.Background(), env)
	if err == nil {
		t.Fatal("expected error from failing bridge, got nil")
	}
	if !strings.Contains(err.Error(), "terminal busy") {
		t.Errorf("error missing bridge output, got: %v", err)
	}
}

func TestReplyToDirectClient_Deliver_MissingRouting(t *testing.T) {
	client := NewReplyToDirectClient()
	env := Envelope{
		EventID:   "evt-missing",
		Type:      EventTypeFeedbackInject,
		Source:    "codero",
		Timestamp: time.Now().UTC(),
		ReplyTo: ReplyToEndpoint{
			Type:      "openclaw_session",
			SessionID: "sess-1",
			// Missing TmuxName/Profile
		},
		Payload:       []byte("{}"),
		SchemaVersion: CurrentSchemaVersion,
	}

	err := client.Deliver(context.Background(), env)
	if err == nil {
		t.Fatal("expected error for missing routing info, got nil")
	}
	if !strings.Contains(err.Error(), "missing routing info") {
		t.Errorf("expected missing routing info error, got: %v", err)
	}
}
