// Package event defines the structured cross-actor event envelope contract.
//
// BND-004: All cross-actor communication flows through this envelope.
// Codero emits structured payloads only. OpenClaw owns final PTY injection
// timing and transport behavior. reply_to is an OpenClaw endpoint, not a PTY path.
package event

import (
	"encoding/json"
	"fmt"
	"time"
)

// EventType identifies the kind of cross-actor event.
type EventType string

const (
	EventTypeTaskAssign     EventType = "task.assign"
	EventTypeTaskDeliver    EventType = "task.deliver"
	EventTypeFeedbackInject EventType = "feedback.inject"
	EventTypeSessionStatus  EventType = "session.status"
	EventTypeGateResult     EventType = "gate.result"
	EventTypeReviewFindings EventType = "review.findings"
)

// ReplyToEndpoint is an OpenClaw-owned durable endpoint for structured
// feedback delivery. It is NOT a PTY path — it is an addressable surface
// that OpenClaw controls for injection timing.
type ReplyToEndpoint struct {
	// Type identifies the endpoint kind. Valid: "openclaw_session".
	Type string `json:"type"`
	// SessionID is the managed session identifier.
	SessionID string `json:"session_id"`
	// TmuxName is the transport identity for tmux-backed sessions.
	TmuxName string `json:"tmux_name,omitempty"`
	// DaemonAddr is the gRPC address for daemon-routed sessions.
	DaemonAddr string `json:"daemon_addr,omitempty"`
}

// Validate checks that the endpoint has the required fields.
func (e ReplyToEndpoint) Validate() error {
	if e.Type == "" {
		return fmt.Errorf("event: reply_to.type is required")
	}
	if e.Type != "openclaw_session" {
		return fmt.Errorf("event: reply_to.type must be openclaw_session, got %q", e.Type)
	}
	if e.SessionID == "" {
		return fmt.Errorf("event: reply_to.session_id is required")
	}
	return nil
}

// Envelope is the structured cross-actor event envelope (BND-004).
// Codero emits these; OpenClaw consumes them and controls PTY injection.
type Envelope struct {
	// EventID is a unique identifier for this event.
	EventID string `json:"event_id"`
	// Type identifies the event kind.
	Type EventType `json:"type"`
	// Source is the emitting actor (always "codero" for outbound events).
	Source string `json:"source"`
	// ReplyTo is the OpenClaw endpoint for structured feedback delivery.
	ReplyTo ReplyToEndpoint `json:"reply_to"`
	// Timestamp is when the event was created.
	Timestamp time.Time `json:"timestamp"`
	// Payload is the event-specific structured data.
	Payload json.RawMessage `json:"payload"`
	// SchemaVersion pins the envelope format version.
	SchemaVersion string `json:"schema_version"`
}

// CurrentSchemaVersion is the current envelope schema version.
const CurrentSchemaVersion = "v1"

// Validate checks that the envelope meets the BND-004 contract.
func (e Envelope) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("event: event_id is required")
	}
	if e.Type == "" {
		return fmt.Errorf("event: type is required")
	}
	if e.Source == "" {
		return fmt.Errorf("event: source is required")
	}
	if err := e.ReplyTo.Validate(); err != nil {
		return fmt.Errorf("event: reply_to: %w", err)
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("event: timestamp is required")
	}
	if e.Payload == nil {
		return fmt.Errorf("event: payload is required")
	}
	if e.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("event: unsupported schema_version %q, want %q", e.SchemaVersion, CurrentSchemaVersion)
	}
	return nil
}

// MarshalJSON encodes the envelope as JSON.
func (e Envelope) MarshalJSON() ([]byte, error) {
	type alias Envelope
	return json.Marshal(alias(e))
}

// UnmarshalJSON decodes the envelope from JSON and validates it.
func (e *Envelope) UnmarshalJSON(data []byte) error {
	type alias Envelope
	if err := json.Unmarshal(data, (*alias)(e)); err != nil {
		return fmt.Errorf("event: decode: %w", err)
	}
	return e.Validate()
}

// TaskDeliverPayload is the structured payload for task.deliver events.
type TaskDeliverPayload struct {
	TaskID       string `json:"task_id"`
	AssignmentID string `json:"assignment_id"`
	SessionID    string `json:"session_id"`
	Worktree     string `json:"worktree"`
	Branch       string `json:"branch"`
	Repo         string `json:"repo"`
}

// FeedbackInjectPayload is the structured payload for feedback.inject events.
type FeedbackInjectPayload struct {
	AssignmentID string         `json:"assignment_id"`
	SessionID    string         `json:"session_id"`
	Findings     []FeedbackItem `json:"findings"`
	GateFindings []FeedbackItem `json:"gate_findings,omitempty"`
	ReviewNotes  []FeedbackItem `json:"review_notes,omitempty"`
	OperatorNote string         `json:"operator_note,omitempty"`
}

// FeedbackItem is a single actionable feedback item.
type FeedbackItem struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Type    string `json:"type,omitempty"`
	Message string `json:"message"`
}

// GateResultPayload is the structured payload for gate.result events.
type GateResultPayload struct {
	AssignmentID string         `json:"assignment_id"`
	SessionID    string         `json:"session_id"`
	Passed       bool           `json:"passed"`
	Attempt      int            `json:"attempt"`
	Findings     []FeedbackItem `json:"findings,omitempty"`
}

// ReviewFindingsPayload is the structured payload for review.findings events.
type ReviewFindingsPayload struct {
	AssignmentID string         `json:"assignment_id"`
	SessionID    string         `json:"session_id"`
	PRNumber     int            `json:"pr_number"`
	Findings     []FeedbackItem `json:"findings"`
	Resolved     []FeedbackItem `json:"resolved,omitempty"`
}

// SessionStatusPayload is the structured payload for session.status events.
type SessionStatusPayload struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	Status    string `json:"status"`
	Substatus string `json:"substatus,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// NewTaskDeliver creates a task.deliver envelope.
func NewTaskDeliver(eventID string, replyTo ReplyToEndpoint, p TaskDeliverPayload) (Envelope, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return Envelope{}, fmt.Errorf("event: marshal payload: %w", err)
	}
	return Envelope{
		EventID:       eventID,
		Type:          EventTypeTaskDeliver,
		Source:        "codero",
		ReplyTo:       replyTo,
		Timestamp:     time.Now().UTC(),
		Payload:       body,
		SchemaVersion: CurrentSchemaVersion,
	}, nil
}

// NewFeedbackInject creates a feedback.inject envelope.
func NewFeedbackInject(eventID string, replyTo ReplyToEndpoint, p FeedbackInjectPayload) (Envelope, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return Envelope{}, fmt.Errorf("event: marshal payload: %w", err)
	}
	return Envelope{
		EventID:       eventID,
		Type:          EventTypeFeedbackInject,
		Source:        "codero",
		ReplyTo:       replyTo,
		Timestamp:     time.Now().UTC(),
		Payload:       body,
		SchemaVersion: CurrentSchemaVersion,
	}, nil
}

// NewGateResult creates a gate.result envelope.
func NewGateResult(eventID string, replyTo ReplyToEndpoint, p GateResultPayload) (Envelope, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return Envelope{}, fmt.Errorf("event: marshal payload: %w", err)
	}
	return Envelope{
		EventID:       eventID,
		Type:          EventTypeGateResult,
		Source:        "codero",
		ReplyTo:       replyTo,
		Timestamp:     time.Now().UTC(),
		Payload:       body,
		SchemaVersion: CurrentSchemaVersion,
	}, nil
}

// NewReviewFindings creates a review.findings envelope.
func NewReviewFindings(eventID string, replyTo ReplyToEndpoint, p ReviewFindingsPayload) (Envelope, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return Envelope{}, fmt.Errorf("event: marshal payload: %w", err)
	}
	return Envelope{
		EventID:       eventID,
		Type:          EventTypeReviewFindings,
		Source:        "codero",
		ReplyTo:       replyTo,
		Timestamp:     time.Now().UTC(),
		Payload:       body,
		SchemaVersion: CurrentSchemaVersion,
	}, nil
}

// NewSessionStatus creates a session.status envelope.
func NewSessionStatus(eventID string, replyTo ReplyToEndpoint, p SessionStatusPayload) (Envelope, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return Envelope{}, fmt.Errorf("event: marshal payload: %w", err)
	}
	return Envelope{
		EventID:       eventID,
		Type:          EventTypeSessionStatus,
		Source:        "codero",
		ReplyTo:       replyTo,
		Timestamp:     time.Now().UTC(),
		Payload:       body,
		SchemaVersion: CurrentSchemaVersion,
	}, nil
}
