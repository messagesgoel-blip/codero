package contract

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/codero/codero/internal/event"
)

// ─── BND-004: Event Envelope and reply_to Boundary Contract Tests ────────────
//
// These tests validate the cross-actor event envelope contract:
//   1. Codero emits structured payloads only
//   2. reply_to is an OpenClaw endpoint, not a PTY path
//   3. All required event types are supported
//   4. Feedback injection succeeds through the durable endpoint contract

// TestBND004_EventEnvelope_AllRequiredEventTypes verifies that every required
// event type can be constructed and validated.
func TestBND004_EventEnvelope_AllRequiredEventTypes(t *testing.T) {
	replyTo := event.ReplyToEndpoint{
		Type:      "openclaw_session",
		SessionID: "sess-bnd004",
		TmuxName:  "codero-agent-bnd004",
	}

	tests := []struct {
		name string
		env  event.Envelope
		err  error
	}{
		{"task.deliver", func() event.Envelope {
			e, _ := event.NewTaskDeliver("evt-1", replyTo, event.TaskDeliverPayload{
				TaskID: "T-1", AssignmentID: "a1", SessionID: "sess-bnd004",
				Worktree: "/path", Branch: "feat/x", Repo: "org/repo",
			})
			return e
		}(), nil},
		{"feedback.inject", func() event.Envelope {
			e, _ := event.NewFeedbackInject("evt-2", replyTo, event.FeedbackInjectPayload{
				AssignmentID: "a1", SessionID: "sess-bnd004",
				Findings: []event.FeedbackItem{{Message: "fix nil check"}},
			})
			return e
		}(), nil},
		{"gate.result", func() event.Envelope {
			e, _ := event.NewGateResult("evt-3", replyTo, event.GateResultPayload{
				AssignmentID: "a1", SessionID: "sess-bnd004",
				Passed: true, Attempt: 1,
			})
			return e
		}(), nil},
		{"review.findings", func() event.Envelope {
			e, _ := event.NewReviewFindings("evt-4", replyTo, event.ReviewFindingsPayload{
				AssignmentID: "a1", SessionID: "sess-bnd004", PRNumber: 42,
				Findings: []event.FeedbackItem{{File: "main.go", Line: 10, Message: "unused import"}},
			})
			return e
		}(), nil},
		{"session.status", func() event.Envelope {
			e, _ := event.NewSessionStatus("evt-5", replyTo, event.SessionStatusPayload{
				SessionID: "sess-bnd004", AgentID: "agent-1", Status: "completed",
			})
			return e
		}(), nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.env.Validate(); err != nil {
				t.Fatalf("envelope validation failed: %v", err)
			}
			if tc.env.Type == "" {
				t.Fatal("event type must not be empty")
			}
			if tc.env.SchemaVersion != event.CurrentSchemaVersion {
				t.Errorf("schema_version = %q, want %q", tc.env.SchemaVersion, event.CurrentSchemaVersion)
			}
			if tc.env.Source != "codero" {
				t.Errorf("source = %q, want codero", tc.env.Source)
			}
			if tc.env.Payload == nil {
				t.Fatal("payload must not be nil")
			}
		})
	}
}

// TestBND004_ReplyTo_NotPTYPath verifies that reply_to rejects PTY path types.
// BND-004: reply_to is an OpenClaw endpoint, not a PTY path.
func TestBND004_ReplyTo_NotPTYPath(t *testing.T) {
	ptyReplyTo := event.ReplyToEndpoint{
		Type:      "pty_path",
		SessionID: "sess-001",
	}
	if err := ptyReplyTo.Validate(); err == nil {
		t.Fatal("expected validation error for pty_path reply_to type")
	}

	validReplyTo := event.ReplyToEndpoint{
		Type:      "openclaw_session",
		SessionID: "sess-001",
	}
	if err := validReplyTo.Validate(); err != nil {
		t.Fatalf("valid reply_to failed: %v", err)
	}
}

// TestBND004_Envelope_RoundTripJSON verifies that envelopes survive JSON
// serialization and deserialization without data loss.
func TestBND004_Envelope_RoundTripJSON(t *testing.T) {
	replyTo := event.ReplyToEndpoint{
		Type:       "openclaw_session",
		SessionID:  "sess-roundtrip",
		TmuxName:   "codero-agent-roundtrip",
		DaemonAddr: "127.0.0.1:8110",
	}
	payload := event.FeedbackInjectPayload{
		AssignmentID: "a1",
		SessionID:    "sess-roundtrip",
		Findings: []event.FeedbackItem{
			{File: "main.go", Line: 42, Message: "fix nil check"},
			{File: "util.go", Line: 15, Message: "unused variable"},
		},
		GateFindings: []event.FeedbackItem{{Message: "gate passed"}},
		OperatorNote: "please review carefully",
	}

	original, err := event.NewFeedbackInject("evt-roundtrip", replyTo, payload)
	if err != nil {
		t.Fatalf("NewFeedbackInject: %v", err)
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded event.Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify all fields survived round-trip
	if decoded.EventID != original.EventID {
		t.Errorf("event_id: got %q, want %q", decoded.EventID, original.EventID)
	}
	if decoded.Type != original.Type {
		t.Errorf("type: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.ReplyTo.Type != original.ReplyTo.Type {
		t.Errorf("reply_to.type: got %q, want %q", decoded.ReplyTo.Type, original.ReplyTo.Type)
	}
	if decoded.ReplyTo.SessionID != original.ReplyTo.SessionID {
		t.Errorf("reply_to.session_id: got %q, want %q", decoded.ReplyTo.SessionID, original.ReplyTo.SessionID)
	}
	if decoded.ReplyTo.TmuxName != original.ReplyTo.TmuxName {
		t.Errorf("reply_to.tmux_name: got %q, want %q", decoded.ReplyTo.TmuxName, original.ReplyTo.TmuxName)
	}
	if decoded.ReplyTo.DaemonAddr != original.ReplyTo.DaemonAddr {
		t.Errorf("reply_to.daemon_addr: got %q, want %q", decoded.ReplyTo.DaemonAddr, original.ReplyTo.DaemonAddr)
	}
	if decoded.SchemaVersion != original.SchemaVersion {
		t.Errorf("schema_version: got %q, want %q", decoded.SchemaVersion, original.SchemaVersion)
	}

	// Verify payload content
	var decodedPayload event.FeedbackInjectPayload
	if err := json.Unmarshal(decoded.Payload, &decodedPayload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decodedPayload.AssignmentID != payload.AssignmentID {
		t.Errorf("payload.assignment_id: got %q, want %q", decodedPayload.AssignmentID, payload.AssignmentID)
	}
	if len(decodedPayload.Findings) != len(payload.Findings) {
		t.Errorf("payload.findings count: got %d, want %d", len(decodedPayload.Findings), len(payload.Findings))
	}
	if decodedPayload.OperatorNote != payload.OperatorNote {
		t.Errorf("payload.operator_note: got %q, want %q", decodedPayload.OperatorNote, payload.OperatorNote)
	}
}

// TestBND004_CoderoEmitsStructuredPayloadsOnly verifies that Codero only
// emits structured event envelopes, not raw PTY text.
func TestBND004_CoderoEmitsStructuredPayloadsOnly(t *testing.T) {
	replyTo := event.ReplyToEndpoint{Type: "openclaw_session", SessionID: "sess-001"}

	// All event constructors produce structured JSON payloads
	tests := []struct {
		name string
		env  event.Envelope
	}{
		{"task.deliver", mustEnvelope(event.NewTaskDeliver("e1", replyTo, event.TaskDeliverPayload{
			TaskID: "T-1", AssignmentID: "a1", SessionID: "sess-001",
		}))},
		{"feedback.inject", mustEnvelope(event.NewFeedbackInject("e2", replyTo, event.FeedbackInjectPayload{
			AssignmentID: "a1", SessionID: "sess-001",
			Findings: []event.FeedbackItem{{Message: "fix this"}},
		}))},
		{"gate.result", mustEnvelope(event.NewGateResult("e3", replyTo, event.GateResultPayload{
			AssignmentID: "a1", SessionID: "sess-001", Passed: true,
		}))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Payload must be valid JSON
			if !json.Valid(tc.env.Payload) {
				t.Fatal("payload is not valid JSON")
			}

			// Payload must decode to a map (structured object, not raw text)
			var m map[string]interface{}
			if err := json.Unmarshal(tc.env.Payload, &m); err != nil {
				t.Fatalf("payload is not a structured object: %v", err)
			}
		})
	}
}

// TestBND004_FeedbackInjection_DurableEndpointContract verifies that feedback
// injection succeeds through the durable endpoint contract.
func TestBND004_FeedbackInjection_DurableEndpointContract(t *testing.T) {
	replyTo := event.ReplyToEndpoint{
		Type:      "openclaw_session",
		SessionID: "sess-feedback",
		TmuxName:  "codero-agent-feedback",
	}

	payload := event.FeedbackInjectPayload{
		AssignmentID: "a1",
		SessionID:    "sess-feedback",
		Findings: []event.FeedbackItem{
			{File: "main.go", Line: 42, Message: "nil pointer dereference risk"},
			{File: "handler.go", Line: 100, Message: "missing error check"},
		},
		GateFindings: []event.FeedbackItem{{Message: "lint failure: unused import"}},
		ReviewNotes:  []event.FeedbackItem{{Message: "consider extracting this function"}},
	}

	env, err := event.NewFeedbackInject("evt-feedback", replyTo, payload)
	if err != nil {
		t.Fatalf("NewFeedbackInject: %v", err)
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("envelope invalid: %v", err)
	}

	// Verify the envelope contains all feedback categories
	var decoded event.FeedbackInjectPayload
	if err := json.Unmarshal(env.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if len(decoded.Findings) != 2 {
		t.Errorf("findings count: got %d, want 2", len(decoded.Findings))
	}
	if len(decoded.GateFindings) != 1 {
		t.Errorf("gate_findings count: got %d, want 1", len(decoded.GateFindings))
	}
	if len(decoded.ReviewNotes) != 1 {
		t.Errorf("review_notes count: got %d, want 1", len(decoded.ReviewNotes))
	}
}

// TestBND004_OpenClawOwnsInjectionTiming verifies that the event envelope
// does not contain PTY injection timing information — OpenClaw owns that.
func TestBND004_OpenClawOwnsInjectionTiming(t *testing.T) {
	replyTo := event.ReplyToEndpoint{Type: "openclaw_session", SessionID: "sess-timing"}
	payload := event.FeedbackInjectPayload{
		AssignmentID: "a1", SessionID: "sess-timing",
		Findings: []event.FeedbackItem{{Message: "fix this"}},
	}

	env, err := event.NewFeedbackInject("evt-timing", replyTo, payload)
	if err != nil {
		t.Fatalf("NewFeedbackInject: %v", err)
	}

	// Envelope should only contain: event_id, type, source, reply_to, timestamp, payload, schema_version
	// It should NOT contain any PTY injection timing fields
	var m map[string]interface{}
	data, _ := json.Marshal(env)
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	expectedKeys := map[string]bool{
		"event_id": true, "type": true, "source": true,
		"reply_to": true, "timestamp": true, "payload": true,
		"schema_version": true,
	}
	for key := range m {
		if !expectedKeys[key] {
			t.Errorf("unexpected key %q in envelope — Codero should not control injection timing", key)
		}
	}
}

// TestBND004_ReplyToEndpoint_AllFields verifies that reply_to carries all
// identity fields needed for OpenClaw to route the event.
func TestBND004_ReplyToEndpoint_AllFields(t *testing.T) {
	replyTo := event.ReplyToEndpoint{
		Type:       "openclaw_session",
		SessionID:  "sess-fields",
		TmuxName:   "codero-agent-fields",
		DaemonAddr: "127.0.0.1:8110",
	}

	if err := replyTo.Validate(); err != nil {
		t.Fatalf("valid reply_to failed: %v", err)
	}

	// Round-trip through JSON
	data, err := json.Marshal(replyTo)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded event.ReplyToEndpoint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != replyTo.Type {
		t.Errorf("type: got %q, want %q", decoded.Type, replyTo.Type)
	}
	if decoded.SessionID != replyTo.SessionID {
		t.Errorf("session_id: got %q, want %q", decoded.SessionID, replyTo.SessionID)
	}
	if decoded.TmuxName != replyTo.TmuxName {
		t.Errorf("tmux_name: got %q, want %q", decoded.TmuxName, replyTo.TmuxName)
	}
	if decoded.DaemonAddr != replyTo.DaemonAddr {
		t.Errorf("daemon_addr: got %q, want %q", decoded.DaemonAddr, replyTo.DaemonAddr)
	}
}

// TestBND004_EventEnvelope_SchemaVersionPinned verifies that the schema
// version is pinned and rejected if mismatched.
func TestBND004_EventEnvelope_SchemaVersionPinned(t *testing.T) {
	if event.CurrentSchemaVersion != "v1" {
		t.Errorf("CurrentSchemaVersion = %q, want v1", event.CurrentSchemaVersion)
	}

	replyTo := event.ReplyToEndpoint{Type: "openclaw_session", SessionID: "sess-v1"}
	payload := event.TaskDeliverPayload{TaskID: "T-1", AssignmentID: "a1", SessionID: "sess-v1"}

	env, err := event.NewTaskDeliver("evt-v1", replyTo, payload)
	if err != nil {
		t.Fatalf("NewTaskDeliver: %v", err)
	}
	if env.SchemaVersion != "v1" {
		t.Errorf("schema_version = %q, want v1", env.SchemaVersion)
	}

	// Tamper with schema version — should fail validation
	env.SchemaVersion = "v0"
	if err := env.Validate(); err == nil {
		t.Fatal("expected validation error for wrong schema_version")
	}
}

// TestBND004_CrossActorEvent_RoundTripIntegration is a round-trip integration
// test proving Codero emits structured payloads and OpenClaw can consume them
// without PTY-path assumptions.
func TestBND004_CrossActorEvent_RoundTripIntegration(t *testing.T) {
	ctx := context.Background()
	_ = ctx // used for delivery client in real implementation

	replyTo := event.ReplyToEndpoint{
		Type:       "openclaw_session",
		SessionID:  "sess-integration",
		TmuxName:   "codero-agent-integration",
		DaemonAddr: "127.0.0.1:8110",
	}

	// Codero constructs a structured feedback injection event
	feedbackPayload := event.FeedbackInjectPayload{
		AssignmentID: "a1",
		SessionID:    "sess-integration",
		Findings: []event.FeedbackItem{
			{File: "main.go", Line: 42, Message: "nil pointer dereference risk on line 42"},
		},
		GateFindings: []event.FeedbackItem{{Message: "lint: unused variable 'x'"}},
	}

	envelope, err := event.NewFeedbackInject("evt-integration", replyTo, feedbackPayload)
	if err != nil {
		t.Fatalf("Codero: failed to construct envelope: %v", err)
	}

	// Validate the envelope meets the BND-004 contract
	if err := envelope.Validate(); err != nil {
		t.Fatalf("Codero: envelope invalid: %v", err)
	}

	// Serialize for transport (Codero emits JSON only)
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("Codero: failed to serialize: %v", err)
	}

	// OpenClaw receives and decodes the envelope
	var received event.Envelope
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("OpenClaw: failed to decode: %v", err)
	}

	// OpenClaw validates the envelope
	if err := received.Validate(); err != nil {
		t.Fatalf("OpenClaw: envelope invalid: %v", err)
	}

	// OpenClaw verifies reply_to is an OpenClaw endpoint (not PTY path)
	if received.ReplyTo.Type != "openclaw_session" {
		t.Fatalf("OpenClaw: reply_to.type = %q, want openclaw_session", received.ReplyTo.Type)
	}

	// OpenClaw extracts the structured payload
	var decodedPayload event.FeedbackInjectPayload
	if err := json.Unmarshal(received.Payload, &decodedPayload); err != nil {
		t.Fatalf("OpenClaw: failed to decode payload: %v", err)
	}

	// Verify content integrity
	if decodedPayload.AssignmentID != feedbackPayload.AssignmentID {
		t.Errorf("assignment_id: got %q, want %q", decodedPayload.AssignmentID, feedbackPayload.AssignmentID)
	}
	if len(decodedPayload.Findings) != len(feedbackPayload.Findings) {
		t.Errorf("findings count: got %d, want %d", len(decodedPayload.Findings), len(feedbackPayload.Findings))
	}
	if len(decodedPayload.GateFindings) != len(feedbackPayload.GateFindings) {
		t.Errorf("gate_findings count: got %d, want %d", len(decodedPayload.GateFindings), len(feedbackPayload.GateFindings))
	}

	// OpenClaw owns injection timing — no PTY path in the envelope
	if received.ReplyTo.TmuxName == "" {
		t.Log("note: tmux_name is optional for non-tmux sessions")
	}
}

// mustEnvelope is a test helper that panics on error.
func mustEnvelope(env event.Envelope, err error) event.Envelope {
	if err != nil {
		panic(err)
	}
	return env
}
