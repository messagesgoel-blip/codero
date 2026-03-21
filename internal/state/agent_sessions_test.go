package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRegisterAgentSession_UpsertAndHeartbeat(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-1", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	s, err := GetAgentSession(ctx, db, "sess-1")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if s.AgentID != "agent-1" {
		t.Errorf("agent_id: got %q, want %q", s.AgentID, "agent-1")
	}
	if s.Mode != "" {
		t.Errorf("mode: got %q, want empty", s.Mode)
	}

	_, err = db.sql.Exec(
		`UPDATE agent_sessions SET last_seen_at = datetime('now','-2 hours') WHERE session_id = ?`,
		"sess-1",
	)
	if err != nil {
		t.Fatalf("seed last_seen_at: %v", err)
	}
	before, err := GetAgentSession(ctx, db, "sess-1")
	if err != nil {
		t.Fatalf("GetAgentSession before heartbeat: %v", err)
	}
	if err := UpdateAgentSessionHeartbeat(ctx, db, "sess-1", false); err != nil {
		t.Fatalf("UpdateAgentSessionHeartbeat: %v", err)
	}
	after, err := GetAgentSession(ctx, db, "sess-1")
	if err != nil {
		t.Fatalf("GetAgentSession after heartbeat: %v", err)
	}
	if !after.LastSeenAt.After(before.LastSeenAt) {
		t.Errorf("last_seen_at not updated: before=%s after=%s", before.LastSeenAt, after.LastSeenAt)
	}
	if after.LastProgressAt != nil {
		t.Errorf("last_progress_at: got %v, want nil when progress not marked", after.LastProgressAt)
	}

	if err := RegisterAgentSession(ctx, db, "sess-1", "agent-2", "cli"); err != nil {
		t.Fatalf("RegisterAgentSession upsert: %v", err)
	}
	updated, err := GetAgentSession(ctx, db, "sess-1")
	if err != nil {
		t.Fatalf("GetAgentSession after upsert: %v", err)
	}
	if updated.AgentID != "agent-2" {
		t.Errorf("agent_id after upsert: got %q, want %q", updated.AgentID, "agent-2")
	}
	if updated.Mode != "cli" {
		t.Errorf("mode after upsert: got %q, want %q", updated.Mode, "cli")
	}
}

func TestRegisterAgentSession_RevivesEndedSession(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-revive", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	if err := ExpireAgentSession(ctx, db, "sess-revive", "expired"); err != nil {
		t.Fatalf("ExpireAgentSession: %v", err)
	}

	if err := RegisterAgentSession(ctx, db, "sess-revive", "agent-2", "cli"); err != nil {
		t.Fatalf("RegisterAgentSession revive: %v", err)
	}

	s, err := GetAgentSession(ctx, db, "sess-revive")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if s.EndedAt != nil {
		t.Fatalf("ended_at should be cleared on revive, got %v", s.EndedAt)
	}
	if s.EndReason != "" {
		t.Fatalf("end_reason should be cleared on revive, got %q", s.EndReason)
	}
	if s.AgentID != "agent-2" {
		t.Fatalf("agent_id after revive: got %q, want %q", s.AgentID, "agent-2")
	}
	if s.Mode != "cli" {
		t.Fatalf("mode after revive: got %q, want %q", s.Mode, "cli")
	}

	if err := UpdateAgentSessionHeartbeat(ctx, db, "sess-revive", false); err != nil {
		t.Fatalf("UpdateAgentSessionHeartbeat after revive: %v", err)
	}
}

func TestRegisterAgentSession_RejectsEndedUUIDSession(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	sessionID := "0e22cb0b-80b9-4af7-b824-a6164fefe3cd"
	if err := RegisterAgentSession(ctx, db, sessionID, "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	if err := ExpireAgentSession(ctx, db, sessionID, "expired"); err != nil {
		t.Fatalf("ExpireAgentSession: %v", err)
	}

	err := RegisterAgentSession(ctx, db, sessionID, "agent-2", "cli")
	if !errors.Is(err, ErrAgentSessionAlreadyEnded) {
		t.Fatalf("RegisterAgentSession should reject ended UUID session: got %v", err)
	}

	s, err := GetAgentSession(ctx, db, sessionID)
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if s.EndedAt == nil {
		t.Fatal("ended_at should remain set for rejected UUID session reuse")
	}
	if s.AgentID != "agent-1" {
		t.Fatalf("agent_id should remain unchanged after rejected reuse: got %q", s.AgentID)
	}
}

func TestUpdateAgentSessionHeartbeat_NotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	err := UpdateAgentSessionHeartbeat(ctx, db, "missing", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAgentSessionNotFound) {
		t.Fatalf("expected ErrAgentSessionNotFound, got %v", err)
	}
}

func TestUpdateAgentSessionHeartbeat_MarkProgress(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-progress", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}

	if err := UpdateAgentSessionHeartbeat(ctx, db, "sess-progress", true); err != nil {
		t.Fatalf("UpdateAgentSessionHeartbeat: %v", err)
	}

	s, err := GetAgentSession(ctx, db, "sess-progress")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if s.LastProgressAt == nil {
		t.Fatal("last_progress_at should be set when progress is marked")
	}
}

func TestAttachAgentAssignment_Supersede(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-1", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}

	first := &AgentAssignment{
		ID:        "assign-1",
		SessionID: "sess-1",
		AgentID:   "agent-1",
		Repo:      "acme/api",
		Branch:    "main",
		Worktree:  "/worktrees/codero/wt1",
		TaskID:    "TASK-1",
	}
	if err := AttachAgentAssignment(ctx, db, first); err != nil {
		t.Fatalf("AttachAgentAssignment first: %v", err)
	}

	active, err := GetActiveAgentAssignment(ctx, db, "sess-1")
	if err != nil {
		t.Fatalf("GetActiveAgentAssignment: %v", err)
	}
	if active.ID != "assign-1" {
		t.Errorf("active assignment id: got %q, want %q", active.ID, "assign-1")
	}

	second := &AgentAssignment{
		ID:        "assign-2",
		SessionID: "sess-1",
		AgentID:   "agent-1",
		Repo:      "acme/api",
		Branch:    "feature/x",
		Worktree:  "/worktrees/codero/wt2",
		TaskID:    "TASK-2",
	}
	if err := AttachAgentAssignment(ctx, db, second); err != nil {
		t.Fatalf("AttachAgentAssignment second: %v", err)
	}

	active, err = GetActiveAgentAssignment(ctx, db, "sess-1")
	if err != nil {
		t.Fatalf("GetActiveAgentAssignment after second: %v", err)
	}
	if active.ID != "assign-2" {
		t.Errorf("active assignment id after second: got %q, want %q", active.ID, "assign-2")
	}

	assignments, err := ListAgentAssignments(ctx, db, "sess-1")
	if err != nil {
		t.Fatalf("ListAgentAssignments: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("assignments count: got %d, want 2", len(assignments))
	}

	var superseded *AgentAssignment
	for i := range assignments {
		if assignments[i].ID == "assign-1" {
			superseded = &assignments[i]
		}
	}
	if superseded == nil {
		t.Fatalf("missing superseded assignment")
	}
	if superseded.EndedAt == nil {
		t.Fatalf("superseded assignment ended_at should be set")
	}
	if superseded.EndReason != "superseded" {
		t.Errorf("end_reason: got %q, want %q", superseded.EndReason, "superseded")
	}
	if superseded.SupersededBy == nil || *superseded.SupersededBy != "assign-2" {
		t.Errorf("superseded_by: got %v, want %q", superseded.SupersededBy, "assign-2")
	}
}

func TestListExpiredAgentSessions(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-expired", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	_, err := db.sql.Exec(
		`UPDATE agent_sessions SET last_seen_at = datetime('now','-2 hours') WHERE session_id = ?`,
		"sess-expired",
	)
	if err != nil {
		t.Fatalf("seed last_seen_at: %v", err)
	}

	expired, err := ListExpiredAgentSessions(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("ListExpiredAgentSessions: %v", err)
	}
	if len(expired) != 1 {
		t.Fatalf("expired sessions: got %d, want 1", len(expired))
	}

	_, err = db.sql.Exec(
		`UPDATE agent_sessions SET ended_at = datetime('now') WHERE session_id = ?`,
		"sess-expired",
	)
	if err != nil {
		t.Fatalf("set ended_at: %v", err)
	}
	expired, err = ListExpiredAgentSessions(ctx, db, time.Hour)
	if err != nil {
		t.Fatalf("ListExpiredAgentSessions after end: %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("expired sessions after end: got %d, want 0", len(expired))
	}
}

func TestAgentEvents_AppendAndList(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-events", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}

	id, err := AppendAgentEvent(ctx, db, &AgentEvent{
		SessionID: "sess-events",
		AgentID:   "agent-1",
		EventType: "session_registered",
		Payload:   `{"ok":true}`,
	})
	if err != nil {
		t.Fatalf("AppendAgentEvent: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected event id, got 0")
	}

	events, err := ListAgentEvents(ctx, db, "sess-events", 0)
	if err != nil {
		t.Fatalf("ListAgentEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events count: got %d, want 2", len(events))
	}
	if events[0].EventType != "session_registered" {
		t.Errorf("event_type: got %q, want %q", events[0].EventType, "session_registered")
	}
	if events[1].EventType != "session_registered" {
		t.Errorf("appended event_type: got %q, want %q", events[1].EventType, "session_registered")
	}
}

func TestGetActiveAgentAssignment_NotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := GetActiveAgentAssignment(ctx, db, "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAgentAssignmentNotFound) {
		t.Fatalf("expected ErrAgentAssignmentNotFound, got %v", err)
	}
}
