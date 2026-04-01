package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelope_Validate_Valid(t *testing.T) {
	env := Envelope{
		EventID: "evt-001",
		Type:    EventTypeTaskDeliver,
		Source:  "codero",
		ReplyTo: ReplyToEndpoint{
			Type:      "openclaw_session",
			SessionID: "sess-001",
			TmuxName:  "codero-agent-001",
		},
		Timestamp:     time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC),
		Payload:       json.RawMessage(`{"task_id":"T-1"}`),
		SchemaVersion: CurrentSchemaVersion,
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("valid envelope failed: %v", err)
	}
}

func TestEnvelope_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		env  Envelope
	}{
		{"missing event_id", Envelope{
			Type: EventTypeTaskDeliver, Source: "codero",
			ReplyTo:   ReplyToEndpoint{Type: "openclaw_session", SessionID: "s1"},
			Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`),
			SchemaVersion: CurrentSchemaVersion,
		}},
		{"missing type", Envelope{
			EventID: "e1", Source: "codero",
			ReplyTo:   ReplyToEndpoint{Type: "openclaw_session", SessionID: "s1"},
			Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`),
			SchemaVersion: CurrentSchemaVersion,
		}},
		{"missing source", Envelope{
			EventID: "e1", Type: EventTypeTaskDeliver,
			ReplyTo:   ReplyToEndpoint{Type: "openclaw_session", SessionID: "s1"},
			Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`),
			SchemaVersion: CurrentSchemaVersion,
		}},
		{"missing timestamp", Envelope{
			EventID: "e1", Type: EventTypeTaskDeliver, Source: "codero",
			ReplyTo: ReplyToEndpoint{Type: "openclaw_session", SessionID: "s1"},
			Payload: json.RawMessage(`{}`), SchemaVersion: CurrentSchemaVersion,
		}},
		{"missing payload", Envelope{
			EventID: "e1", Type: EventTypeTaskDeliver, Source: "codero",
			ReplyTo:   ReplyToEndpoint{Type: "openclaw_session", SessionID: "s1"},
			Timestamp: time.Now().UTC(), SchemaVersion: CurrentSchemaVersion,
		}},
		{"wrong schema version", Envelope{
			EventID: "e1", Type: EventTypeTaskDeliver, Source: "codero",
			ReplyTo:   ReplyToEndpoint{Type: "openclaw_session", SessionID: "s1"},
			Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`),
			SchemaVersion: "v0",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.env.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestReplyToEndpoint_Validate(t *testing.T) {
	valid := ReplyToEndpoint{Type: "openclaw_session", SessionID: "s1"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid endpoint failed: %v", err)
	}

	missingType := ReplyToEndpoint{SessionID: "s1"}
	if err := missingType.Validate(); err == nil {
		t.Fatal("expected error for missing type")
	}

	wrongType := ReplyToEndpoint{Type: "pty_path", SessionID: "s1"}
	if err := wrongType.Validate(); err == nil {
		t.Fatal("expected error for wrong type")
	}

	missingSession := ReplyToEndpoint{Type: "openclaw_session"}
	if err := missingSession.Validate(); err == nil {
		t.Fatal("expected error for missing session_id")
	}
}

func TestEnvelope_MarshalUnmarshal(t *testing.T) {
	env := Envelope{
		EventID: "evt-001",
		Type:    EventTypeFeedbackInject,
		Source:  "codero",
		ReplyTo: ReplyToEndpoint{
			Type:      "openclaw_session",
			SessionID: "sess-001",
			TmuxName:  "codero-agent-001",
		},
		Timestamp:     time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC),
		Payload:       json.RawMessage(`{"assignment_id":"a1","session_id":"sess-001","findings":[{"message":"fix this"}]}`),
		SchemaVersion: CurrentSchemaVersion,
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.EventID != env.EventID {
		t.Errorf("event_id: got %q, want %q", decoded.EventID, env.EventID)
	}
	if decoded.Type != env.Type {
		t.Errorf("type: got %q, want %q", decoded.Type, env.Type)
	}
	if decoded.ReplyTo.SessionID != env.ReplyTo.SessionID {
		t.Errorf("reply_to.session_id: got %q, want %q", decoded.ReplyTo.SessionID, env.ReplyTo.SessionID)
	}
	if decoded.ReplyTo.TmuxName != env.ReplyTo.TmuxName {
		t.Errorf("reply_to.tmux_name: got %q, want %q", decoded.ReplyTo.TmuxName, env.ReplyTo.TmuxName)
	}
}

func TestNewTaskDeliver(t *testing.T) {
	replyTo := ReplyToEndpoint{Type: "openclaw_session", SessionID: "sess-001", TmuxName: "codero-agent-001"}
	payload := TaskDeliverPayload{
		TaskID: "T-001", AssignmentID: "a1", SessionID: "sess-001",
		Worktree: "/path/to/worktree", Branch: "feat/fix", Repo: "org/repo",
	}

	env, err := NewTaskDeliver("evt-001", replyTo, payload)
	if err != nil {
		t.Fatalf("NewTaskDeliver: %v", err)
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("envelope invalid: %v", err)
	}
	if env.Type != EventTypeTaskDeliver {
		t.Errorf("type: got %q, want %q", env.Type, EventTypeTaskDeliver)
	}
	if env.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("schema_version: got %q, want %q", env.SchemaVersion, CurrentSchemaVersion)
	}
}

func TestNewFeedbackInject(t *testing.T) {
	replyTo := ReplyToEndpoint{Type: "openclaw_session", SessionID: "sess-001"}
	payload := FeedbackInjectPayload{
		AssignmentID: "a1", SessionID: "sess-001",
		Findings: []FeedbackItem{{File: "main.go", Line: 42, Message: "fix nil check"}},
	}

	env, err := NewFeedbackInject("evt-002", replyTo, payload)
	if err != nil {
		t.Fatalf("NewFeedbackInject: %v", err)
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("envelope invalid: %v", err)
	}
	if env.Type != EventTypeFeedbackInject {
		t.Errorf("type: got %q, want %q", env.Type, EventTypeFeedbackInject)
	}
}

func TestNewGateResult(t *testing.T) {
	replyTo := ReplyToEndpoint{Type: "openclaw_session", SessionID: "sess-001"}
	payload := GateResultPayload{
		AssignmentID: "a1", SessionID: "sess-001",
		Passed: false, Attempt: 1,
		Findings: []FeedbackItem{{Message: "gate failed"}},
	}

	env, err := NewGateResult("evt-003", replyTo, payload)
	if err != nil {
		t.Fatalf("NewGateResult: %v", err)
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("envelope invalid: %v", err)
	}
	if env.Type != EventTypeGateResult {
		t.Errorf("type: got %q, want %q", env.Type, EventTypeGateResult)
	}
}

func TestNewReviewFindings(t *testing.T) {
	replyTo := ReplyToEndpoint{Type: "openclaw_session", SessionID: "sess-001"}
	payload := ReviewFindingsPayload{
		AssignmentID: "a1", SessionID: "sess-001", PRNumber: 42,
		Findings: []FeedbackItem{{File: "main.go", Line: 10, Message: "unused import"}},
	}

	env, err := NewReviewFindings("evt-004", replyTo, payload)
	if err != nil {
		t.Fatalf("NewReviewFindings: %v", err)
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("envelope invalid: %v", err)
	}
	if env.Type != EventTypeReviewFindings {
		t.Errorf("type: got %q, want %q", env.Type, EventTypeReviewFindings)
	}
}

func TestNewSessionStatus(t *testing.T) {
	replyTo := ReplyToEndpoint{Type: "openclaw_session", SessionID: "sess-001"}
	payload := SessionStatusPayload{
		SessionID: "sess-001", AgentID: "agent-001",
		Status: "completed", Substatus: "terminal_finished",
	}

	env, err := NewSessionStatus("evt-005", replyTo, payload)
	if err != nil {
		t.Fatalf("NewSessionStatus: %v", err)
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("envelope invalid: %v", err)
	}
	if env.Type != EventTypeSessionStatus {
		t.Errorf("type: got %q, want %q", env.Type, EventTypeSessionStatus)
	}
}

func TestEnvelope_RejectPTYPathReplyTo(t *testing.T) {
	// BND-004: reply_to must be an OpenClaw endpoint, not a raw PTY path.
	replyTo := ReplyToEndpoint{Type: "pty_path", SessionID: "sess-001"}
	payload := TaskDeliverPayload{TaskID: "T-1", AssignmentID: "a1", SessionID: "sess-001"}

	_, err := NewTaskDeliver("evt-001", replyTo, payload)
	if err != nil {
		t.Fatalf("NewTaskDeliver: %v", err)
	}

	// The envelope should fail validation because type is not "openclaw_session"
	env, _ := NewTaskDeliver("evt-001", replyTo, payload)
	if err := env.Validate(); err == nil {
		t.Fatal("expected validation error for pty_path reply_to type")
	}
}
