package state

import (
	"context"
	"database/sql"
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

func TestConfirmAgentSession(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-confirm", "agent-1", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	if err := ConfirmAgentSession(ctx, db, "sess-confirm", "agent-1"); err != nil {
		t.Fatalf("ConfirmAgentSession: %v", err)
	}

	err := ConfirmAgentSession(ctx, db, "sess-confirm", "agent-2")
	if !errors.Is(err, ErrAgentSessionAgentMismatch) {
		t.Fatalf("ConfirmAgentSession mismatch: got %v", err)
	}

	if err := ExpireAgentSession(ctx, db, "sess-confirm", "expired"); err != nil {
		t.Fatalf("ExpireAgentSession: %v", err)
	}
	err = ConfirmAgentSession(ctx, db, "sess-confirm", "agent-1")
	if !errors.Is(err, ErrAgentSessionAlreadyEnded) {
		t.Fatalf("ConfirmAgentSession ended session: got %v", err)
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
	if active.Substatus != AssignmentSubstatusInProgress {
		t.Errorf("active substatus: got %q, want %q", active.Substatus, AssignmentSubstatusInProgress)
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
	if superseded.Substatus != AssignmentSubstatusTerminalWaitingNextTask {
		t.Errorf("superseded substatus: got %q, want %q", superseded.Substatus, AssignmentSubstatusTerminalWaitingNextTask)
	}
	if superseded.SupersededBy == nil || *superseded.SupersededBy != "assign-2" {
		t.Errorf("superseded_by: got %v, want %q", superseded.SupersededBy, "assign-2")
	}

	rows, err := db.sql.QueryContext(ctx, `
		SELECT rule_id, result
		FROM assignment_rule_checks
		WHERE assignment_id = ?
		ORDER BY rule_id ASC`, "assign-2")
	if err != nil {
		t.Fatalf("query assignment_rule_checks: %v", err)
	}
	defer rows.Close()

	results := map[string]string{}
	for rows.Next() {
		var ruleID, result string
		if err := rows.Scan(&ruleID, &result); err != nil {
			t.Fatalf("scan assignment_rule_checks: %v", err)
		}
		results[ruleID] = result
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("assignment_rule_checks rows: %v", err)
	}
	if results[RuleIDGateMustPassBeforeMerge] != "pending" {
		t.Fatalf("RULE-001 result = %q, want pending", results[RuleIDGateMustPassBeforeMerge])
	}
	if results[RuleIDNoSilentFailure] != "pass" {
		t.Fatalf("RULE-002 result = %q, want pass", results[RuleIDNoSilentFailure])
	}
}

func TestFinalizeAgentSession(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-final", "agent-1", "agent"); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO branch_states (
			id, repo, branch, state, approved, ci_green, pending_events, unresolved_threads,
			owner_agent, owner_session_id, owner_session_last_seen
		)
		VALUES (?, ?, ?, ?, 1, 1, 0, 0, ?, ?, datetime('now'))`,
		"branch-final", "acme/api", "feat/final", "merge_ready", "agent-1", "sess-final",
	)
	if err != nil {
		t.Fatalf("seed branch state: %v", err)
	}
	if err := AttachAgentAssignment(ctx, db, &AgentAssignment{
		ID:        "assign-final",
		SessionID: "sess-final",
		AgentID:   "agent-1",
		Repo:      "acme/api",
		Branch:    "feat/final",
		Worktree:  "/srv/storage/repo/codero/.worktrees/COD-066-cozy-tui-port/.tmp-tests/finalize-wt",
		TaskID:    "TASK-9",
	}); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}

	finishedAt := time.Date(2026, 3, 21, 20, 0, 0, 0, time.UTC)
	if err := FinalizeAgentSession(ctx, db, "sess-final", "agent-1", AgentSessionCompletion{
		Status:     "done",
		Substatus:  AssignmentSubstatusTerminalFinished,
		Summary:    "completed",
		Tests:      []string{"go test ./cmd/codero"},
		FinishedAt: finishedAt,
	}); err != nil {
		t.Fatalf("FinalizeAgentSession: %v", err)
	}

	sessionRow, err := GetAgentSession(ctx, db, "sess-final")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if sessionRow.EndedAt == nil || sessionRow.EndReason != "done" {
		t.Fatalf("session finalize state = %#v", sessionRow)
	}

	assignments, err := ListAgentAssignments(ctx, db, "sess-final")
	if err != nil {
		t.Fatalf("ListAgentAssignments: %v", err)
	}
	if len(assignments) != 1 || assignments[0].EndedAt == nil || assignments[0].EndReason != "done" {
		t.Fatalf("finalized assignments = %#v", assignments)
	}
	if assignments[0].Substatus != AssignmentSubstatusTerminalFinished {
		t.Fatalf("assignment substatus = %q, want %q", assignments[0].Substatus, AssignmentSubstatusTerminalFinished)
	}

	var ownerSessionID string
	if err := db.sql.QueryRowContext(ctx, `
		SELECT owner_session_id FROM branch_states WHERE repo = ? AND branch = ?`,
		"acme/api", "feat/final",
	).Scan(&ownerSessionID); err != nil {
		t.Fatalf("read branch state: %v", err)
	}
	if ownerSessionID != "" {
		t.Fatalf("owner_session_id = %q, want cleared", ownerSessionID)
	}

	events, err := ListAgentEvents(ctx, db, "sess-final", 0)
	if err != nil {
		t.Fatalf("ListAgentEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected agent events, got none")
	}
	if events[len(events)-1].EventType != "session_finalized" {
		t.Fatalf("latest event_type = %q, want session_finalized", events[len(events)-1].EventType)
	}

	rows, err := db.sql.QueryContext(ctx, `
		SELECT rule_id, result
		FROM assignment_rule_checks
		WHERE assignment_id = ?
		ORDER BY rule_id ASC`, "assign-final")
	if err != nil {
		t.Fatalf("query assignment_rule_checks: %v", err)
	}
	defer rows.Close()

	results := map[string]string{}
	for rows.Next() {
		var ruleID, result string
		if err := rows.Scan(&ruleID, &result); err != nil {
			t.Fatalf("scan assignment_rule_checks: %v", err)
		}
		results[ruleID] = result
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("assignment_rule_checks rows: %v", err)
	}
	if results[RuleIDGateMustPassBeforeMerge] != "pass" {
		t.Fatalf("RULE-001 result = %q, want pass", results[RuleIDGateMustPassBeforeMerge])
	}
	if results[RuleIDNoSilentFailure] != "pass" {
		t.Fatalf("RULE-002 result = %q, want pass", results[RuleIDNoSilentFailure])
	}
}

func TestFinalizeAgentSession_RejectsCompletionWhenMergeGateFails(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-gate", "agent-1", "agent"); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO branch_states (
			id, repo, branch, state, approved, ci_green, pending_events, unresolved_threads,
			owner_agent, owner_session_id, owner_session_last_seen
		)
		VALUES (?, ?, ?, ?, 0, 0, 1, 1, ?, ?, datetime('now'))`,
		"branch-gate", "acme/api", "feat/gate", "queued_cli", "agent-1", "sess-gate",
	)
	if err != nil {
		t.Fatalf("seed branch state: %v", err)
	}
	if err := AttachAgentAssignment(ctx, db, &AgentAssignment{
		ID:        "assign-gate",
		SessionID: "sess-gate",
		AgentID:   "agent-1",
		Repo:      "acme/api",
		Branch:    "feat/gate",
		Worktree:  "/srv/storage/repo/codero/.worktrees/COD-071/.tmp-tests/gate",
		TaskID:    "TASK-10",
	}); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}

	err = FinalizeAgentSession(ctx, db, "sess-gate", "agent-1", AgentSessionCompletion{
		Status:     "done",
		Substatus:  AssignmentSubstatusTerminalFinished,
		Summary:    "completed",
		FinishedAt: time.Now().UTC(),
	})
	if !errors.Is(err, ErrAssignmentGateNotPassed) {
		t.Fatalf("FinalizeAgentSession error = %v, want %v", err, ErrAssignmentGateNotPassed)
	}

	sessionRow, err := GetAgentSession(ctx, db, "sess-gate")
	if err != nil {
		t.Fatalf("GetAgentSession: %v", err)
	}
	if sessionRow.EndedAt != nil {
		t.Fatalf("session should remain live, got %#v", sessionRow)
	}

	active, err := GetActiveAgentAssignment(ctx, db, "sess-gate")
	if err != nil {
		t.Fatalf("GetActiveAgentAssignment: %v", err)
	}
	if active.EndedAt != nil {
		t.Fatalf("assignment should remain active, got %#v", active)
	}

	var rule001Result string
	if err := db.sql.QueryRowContext(ctx, `
		SELECT result
		FROM assignment_rule_checks
		WHERE assignment_id = ? AND rule_id = ?`,
		"assign-gate", RuleIDGateMustPassBeforeMerge,
	).Scan(&rule001Result); err != nil {
		t.Fatalf("read RULE-001 check: %v", err)
	}
	if rule001Result != "fail" {
		t.Fatalf("RULE-001 result = %q, want fail", rule001Result)
	}
}

func TestFinalizeAgentSession_RequiresSubstatus(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "sess-substatus", "agent-1", "agent"); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	if err := AttachAgentAssignment(ctx, db, &AgentAssignment{
		ID:        "assign-substatus",
		SessionID: "sess-substatus",
		AgentID:   "agent-1",
		Repo:      "acme/api",
		Branch:    "feat/substatus",
		Worktree:  "/srv/storage/repo/codero/.worktrees/COD-071/.tmp-tests/substatus",
		TaskID:    "TASK-11",
	}); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}

	err := FinalizeAgentSession(ctx, db, "sess-substatus", "agent-1", AgentSessionCompletion{
		Status:     "blocked",
		Summary:    "waiting on credentials",
		FinishedAt: time.Now().UTC(),
	})
	if !errors.Is(err, ErrAssignmentSubstatusRequired) {
		t.Fatalf("FinalizeAgentSession error = %v, want %v", err, ErrAssignmentSubstatusRequired)
	}

	var result string
	var detail sql.NullString
	if err := db.sql.QueryRowContext(ctx, `
		SELECT result, detail
		FROM assignment_rule_checks
		WHERE assignment_id = ? AND rule_id = ?`,
		"assign-substatus", RuleIDNoSilentFailure,
	).Scan(&result, &detail); err != nil {
		t.Fatalf("read RULE-002 check: %v", err)
	}
	if result != "fail" {
		t.Fatalf("RULE-002 result = %q, want fail", result)
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
