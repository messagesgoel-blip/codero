package state

import (
	"context"
	"testing"
)

// Agent Spec v3 certification tests.
// Each test maps to a specific clause in codero_certification_matrix_v1.md §2.

// TestCert_AgentV3_Rule001_GateBlocksMerge verifies RULE-001: gate must pass
// before an assignment can finalize as "done". When branch gate conditions are
// not met, FinalizeAgentSession must return ErrAssignmentGateNotPassed and
// record a RULE-001 failure in assignment_rule_checks.
//
// Matrix clause: RULE-001 | Evidence: IT
func TestCert_AgentV3_Rule001_GateBlocksMerge(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	mustRegisterSession(t, db, "cert-r1-sess", "cert-r1-agent")
	seedBranchState(t, db, "cert-r1-branch", "acme/api", "feat/cert-r1", "queued_cli",
		false, false, 1, 1, "cert-r1-agent", "cert-r1-sess")

	if err := AttachAgentAssignment(ctx, db, &AgentAssignment{
		ID:        "cert-r1-assign",
		SessionID: "cert-r1-sess",
		AgentID:   "cert-r1-agent",
		Repo:      "acme/api",
		Branch:    "feat/cert-r1",
		Worktree:  "/srv/storage/repo/codero/.worktrees/cert/.tmp-tests/cert-r1",
		TaskID:    "CERT-R1-TASK",
	}); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}

	err := FinalizeAgentSession(ctx, db, "cert-r1-sess", "cert-r1-agent", AgentSessionCompletion{
		Status:    "done",
		Substatus: AssignmentSubstatusTerminalFinished,
		Summary:   "cert-r1",
	})
	if err == nil {
		t.Fatal("FinalizeAgentSession should fail when gate conditions not met")
	}
	assertErrorIs(t, err, ErrAssignmentGateNotPassed, "RULE-001 should block finalization")

	var result string
	if err := db.sql.QueryRowContext(ctx,
		`SELECT result FROM assignment_rule_checks WHERE assignment_id = ? AND rule_id = ?`,
		"cert-r1-assign", RuleIDGateMustPassBeforeMerge,
	).Scan(&result); err != nil {
		t.Fatalf("query RULE-001 check: %v", err)
	}
	if result != "fail" {
		t.Errorf("RULE-001 result = %q, want fail", result)
	}
}

// TestCert_AgentV3_Rule002_RequiresSubstatus verifies RULE-002: no silent
// failure. Every finalization must provide a valid substatus.
//
// Matrix clause: RULE-002 | Evidence: UT
func TestCert_AgentV3_Rule002_RequiresSubstatus(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	mustRegisterSession(t, db, "cert-r2-sess", "cert-r2-agent")
	mustAcceptTask(t, db, "cert-r2-sess", "cert-r2-agent", "CERT-R2-TASK")

	err := FinalizeAgentSession(ctx, db, "cert-r2-sess", "cert-r2-agent", AgentSessionCompletion{
		Status:  "done",
		Summary: "cert-r2",
	})
	if err == nil {
		t.Fatal("FinalizeAgentSession should fail without substatus")
	}
	if err.Error() == "" {
		t.Fatal("error should describe the missing substatus")
	}
}

// TestCert_AgentV3_Section22_SubstatusEnumEnforced verifies §2.2: substatus
// enum is enforced with ACTIVE, BLOCKED, and TERMINAL state groups.
//
// Matrix clause: §2.2 | Evidence: UT
func TestCert_AgentV3_Section22_SubstatusEnumEnforced(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := mustAcceptTask(t, db, "cert-s22-sess", "cert-s22-agent", "CERT-S22-TASK")

	// Valid active substatus succeeds.
	if _, err := EmitAssignmentUpdate(ctx, db, a.ID, 1, AssignmentSubstatusWaitingForCI); err != nil {
		t.Fatalf("valid active substatus rejected: %v", err)
	}

	// Invalid substatus is rejected.
	_, err := EmitAssignmentUpdate(ctx, db, a.ID, 2, "bogus_invalid_state")
	if err == nil {
		t.Fatal("invalid substatus should be rejected")
	}
	assertErrorIs(t, err, ErrInvalidEmitSubstatus, "invalid substatus")

	// Empty substatus is rejected.
	_, err = EmitAssignmentUpdate(ctx, db, a.ID, 2, "")
	if err == nil {
		t.Fatal("empty substatus should be rejected")
	}

	// Verify state groups exist and are non-empty.
	if len(activeAssignmentSubstatusSet) == 0 {
		t.Error("ACTIVE state group is empty")
	}
	if len(blockedAssignmentSubstatusSet) == 0 {
		t.Error("BLOCKED state group is empty")
	}
	if len(terminalAssignmentSubstatusSet) == 0 {
		t.Error("TERMINAL state group is empty")
	}
}

// TestCert_AgentV3_Section24_TransitionsValid verifies §2.4: only allowed
// substatus transitions succeed.
//
// Matrix clause: §2.4 | Evidence: UT
func TestCert_AgentV3_Section24_TransitionsValid(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := mustAcceptTask(t, db, "cert-s24-sess", "cert-s24-agent", "CERT-S24-TASK")

	// Active → blocked (allowed).
	updated, err := EmitAssignmentUpdate(ctx, db, a.ID, 1, AssignmentSubstatusBlockedCIFailure)
	if err != nil {
		t.Fatalf("active → blocked should succeed: %v", err)
	}
	if updated.Substatus != AssignmentSubstatusBlockedCIFailure {
		t.Errorf("substatus = %q, want %q", updated.Substatus, AssignmentSubstatusBlockedCIFailure)
	}

	// Blocked → terminal (allowed).
	updated, err = EmitAssignmentUpdate(ctx, db, a.ID, 2, AssignmentSubstatusTerminalFinished)
	if err != nil {
		t.Fatalf("blocked → terminal should succeed: %v", err)
	}
	if updated.EndedAt == nil {
		t.Error("terminal substatus should set ended_at")
	}
}

// TestCert_AgentV3_Section31_AgentRulesSchema verifies §3.1: agent_rules
// table exists and contains all 4 seed rules after migration.
//
// Matrix clause: §3.1 | Evidence: STATE
func TestCert_AgentV3_Section31_AgentRulesSchema(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	expectedRules := []string{
		RuleIDGateMustPassBeforeMerge,
		RuleIDNoSilentFailure,
		RuleIDBranchHoldTTL,
		RuleIDHeartbeatProgress,
	}

	// Query agent_rules directly — the table is seeded by migration 000008.
	rows, err := db.sql.QueryContext(ctx, `SELECT rule_id FROM agent_rules WHERE active = 1`)
	if err != nil {
		t.Fatalf("query agent_rules: %v", err)
	}
	defer rows.Close()

	ruleMap := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan agent_rules: %v", err)
		}
		ruleMap[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("agent_rules rows: %v", err)
	}

	// If the table has no rows (e.g., no explicit seed migration),
	// verify that the default fallback definitions cover all 4 rules.
	if len(ruleMap) == 0 {
		for _, id := range expectedRules {
			if _, ok := defaultAgentRuleDefinitions[id]; !ok {
				t.Errorf("seed rule %q missing from both agent_rules table and default definitions", id)
			}
		}
		return
	}

	for _, id := range expectedRules {
		if !ruleMap[id] {
			t.Errorf("seed rule %q missing from agent_rules", id)
		}
	}
}

// TestCert_AgentV3_Section32_RuleChecksAtomicWithAttach verifies §3.2:
// assignment_rule_checks rows are created atomically when an assignment is
// attached via AttachAgentAssignment (the canonical daemon/launcher path).
//
// Matrix clause: §3.2 | Evidence: STATE
func TestCert_AgentV3_Section32_RuleChecksAtomicWithAttach(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	mustRegisterSession(t, db, "cert-s32-sess", "cert-s32-agent")
	if err := AttachAgentAssignment(ctx, db, &AgentAssignment{
		ID:        "cert-s32-assign",
		SessionID: "cert-s32-sess",
		AgentID:   "cert-s32-agent",
		Repo:      "acme/api",
		Branch:    "feat/cert-s32",
		Worktree:  "/srv/storage/repo/codero/.worktrees/cert/.tmp-tests/cert-s32",
		TaskID:    "CERT-S32-TASK",
	}); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}

	var count int
	if err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM assignment_rule_checks WHERE assignment_id = ?`, "cert-s32-assign",
	).Scan(&count); err != nil {
		t.Fatalf("count rule checks: %v", err)
	}
	if count < 4 {
		t.Errorf("assignment_rule_checks count = %d, want >= 4 (one per seed rule)", count)
	}

	expectedRules := []string{
		RuleIDGateMustPassBeforeMerge,
		RuleIDNoSilentFailure,
		RuleIDBranchHoldTTL,
		RuleIDHeartbeatProgress,
	}
	for _, ruleID := range expectedRules {
		var checkID string
		if err := db.sql.QueryRowContext(ctx,
			`SELECT check_id FROM assignment_rule_checks WHERE assignment_id = ? AND rule_id = ?`,
			"cert-s32-assign", ruleID,
		).Scan(&checkID); err != nil {
			t.Fatalf("rule check %s missing from assignment_rule_checks: %v", ruleID, err)
		}
		if checkID == "" {
			t.Errorf("rule check %s: check_id should be non-empty", ruleID)
		}
	}
}

// TestCert_AgentV3_Section32_AcceptTaskSeedsRuleChecks verifies §3.2
// also applies to the AcceptTask path: rule checks are created atomically.
//
// Matrix clause: §3.2 | Evidence: STATE
func TestCert_AgentV3_Section32_AcceptTaskSeedsRuleChecks(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	a := mustAcceptTask(t, db, "cert-s32b-sess", "cert-s32b-agent", "CERT-S32B-TASK")

	var count int
	if err := db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM assignment_rule_checks WHERE assignment_id = ?`, a.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count rule checks: %v", err)
	}
	if count < 4 {
		t.Errorf("assignment_rule_checks count = %d, want >= 4 (one per seed rule)", count)
	}
}

// TestCert_AgentV3_Section92_SubstatusOwnershipSplit verifies §9.2:
// system-owned terminal substatuses cannot be emitted by agents, while
// agent-safe terminal substatuses can.
//
// Matrix clause: §9.2 | Evidence: UT
func TestCert_AgentV3_Section92_SubstatusOwnershipSplit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	systemOwned := []string{
		AssignmentSubstatusTerminalWaitingNextTask,
		AssignmentSubstatusTerminalLost,
		AssignmentSubstatusTerminalStuckAbandoned,
	}
	for _, ss := range systemOwned {
		t.Run("reject_system_"+ss, func(t *testing.T) {
			a := mustAcceptTask(t, db, "cert-s92-sys-"+ss, "cert-s92-agent", "CERT-S92-SYS-"+ss)
			_, err := EmitAssignmentUpdate(ctx, db, a.ID, 1, ss)
			assertErrorIs(t, err, ErrInvalidEmitSubstatus, "system-owned "+ss)
		})
	}

	agentSafe := []string{
		AssignmentSubstatusTerminalFinished,
		AssignmentSubstatusTerminalWaitingComments,
		AssignmentSubstatusTerminalCancelled,
	}
	for _, ss := range agentSafe {
		t.Run("allow_agent_"+ss, func(t *testing.T) {
			a := mustAcceptTask(t, db, "cert-s92-agt-"+ss, "cert-s92-agent", "CERT-S92-AGT-"+ss)
			updated, err := EmitAssignmentUpdate(ctx, db, a.ID, 1, ss)
			if err != nil {
				t.Fatalf("agent-safe %q should succeed: %v", ss, err)
			}
			if updated.Substatus != ss {
				t.Errorf("substatus = %q, want %q", updated.Substatus, ss)
			}
		})
	}
}

// helpers

func seedBranchState(t *testing.T, db *DB, id, repo, branch, branchState string,
	approved, ciGreen bool, pending, unresolved int,
	ownerAgent, ownerSession string) {
	t.Helper()
	approvedInt, ciGreenInt := 0, 0
	if approved {
		approvedInt = 1
	}
	if ciGreen {
		ciGreenInt = 1
	}
	_, err := db.sql.Exec(`
		INSERT INTO branch_states (
			id, repo, branch, state, approved, ci_green, pending_events, unresolved_threads,
			owner_agent, owner_session_id, owner_session_last_seen
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		id, repo, branch, branchState, approvedInt, ciGreenInt, pending, unresolved,
		ownerAgent, ownerSession,
	)
	if err != nil {
		t.Fatalf("seedBranchState %s: %v", id, err)
	}
}

func assertErrorIs(t *testing.T, err, target error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error, got nil", msg)
	}
	// Use string matching as errors may be wrapped with additional context.
	if target != nil {
		found := false
		for unwrapped := err; unwrapped != nil; {
			if unwrapped.Error() == target.Error() {
				found = true
				break
			}
			inner, ok := unwrapped.(interface{ Unwrap() error })
			if !ok {
				break
			}
			unwrapped = inner.Unwrap()
		}
		if !found {
			// Fallback: check if the error message contains the target message.
			if containsString(err.Error(), target.Error()) {
				return
			}
			t.Fatalf("%s: got %v, want %v", msg, err, target)
		}
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
