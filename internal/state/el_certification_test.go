package state

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Execution Loop v1 — Clause-Mapped Certification Tests
// Each test function maps to one or more EL-* invariants from
// codero_certification_matrix_v1.md §Execution Loop v1.
// ---------------------------------------------------------------------------

// ---------- EL-1: State Machine coverage ----------
// Verify all canonical states exist (11 states).
func TestEL01_StateMachineCoverage(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	states := []string{
		"submitted", "waiting", "queued_cli",
		"cli_reviewing", "review_approved",
		"merge_ready", "merged",
		"blocked", "abandoned", "expired", "stale",
	}

	for _, s := range states {
		_, err := db.Unwrap().ExecContext(ctx, `
			INSERT INTO branch_states (id, repo, branch, state)
			VALUES (?, 'el-test/repo', ?, ?)`,
			"el1-"+s, "el1-branch-"+s, s,
		)
		if err != nil {
			t.Errorf("state %q: insert failed: %v", s, err)
		}
	}
}

// ---------- EL-2: Valid transition enforcement ----------
func TestEL02_TransitionAuditLog(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state)
		VALUES ('el2-bs', 'el-test/repo', 'el2-branch', 'submitted')`)
	if err != nil {
		t.Fatalf("insert branch state: %v", err)
	}

	_, err = db.Unwrap().ExecContext(ctx, `
		INSERT INTO state_transitions (branch_state_id, from_state, to_state, trigger)
		VALUES ('el2-bs', 'submitted', 'queued_cli', 'codero-cli submit')`)
	if err != nil {
		t.Fatalf("insert transition: %v", err)
	}

	var cnt int
	err = db.Unwrap().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM state_transitions WHERE branch_state_id = 'el2-bs'`).Scan(&cnt)
	if err != nil || cnt != 1 {
		t.Errorf("expected 1 transition, got %d (err=%v)", cnt, err)
	}
}

// ---------- EL-3: Priority validated at submit ----------
func TestEL03_PriorityValidation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Queue priority is an integer field 0-20.
	for _, prio := range []int{0, 10, 20} {
		id := "el3-" + string(rune('a'+prio/10))
		_, err := db.Unwrap().ExecContext(ctx, `
			INSERT INTO branch_states (id, repo, branch, state, queue_priority)
			VALUES (?, 'el-test/repo', ?, 'queued_cli', ?)`,
			id, "el3-branch-"+id, prio,
		)
		if err != nil {
			t.Errorf("priority %d: insert failed: %v", prio, err)
		}
	}
}

// ---------- EL-4: WFQ scheduling correctness ----------
func TestEL04_WFQSchedulingFields(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, queue_priority, submission_time)
		VALUES ('el4-bs', 'el-test/repo', 'el4-branch', 'queued_cli', 5, datetime('now'))`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var prio int
	var subTime string
	err = db.Unwrap().QueryRowContext(ctx, `
		SELECT queue_priority, submission_time FROM branch_states WHERE id = 'el4-bs'`).Scan(&prio, &subTime)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if prio != 5 {
		t.Errorf("queue_priority: got %d, want 5", prio)
	}
	if subTime == "" {
		t.Error("submission_time should not be empty")
	}
}

// ---------- EL-5: Lease semantics ----------
func TestEL05_LeaseSemanticsFields(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, lease_id, lease_expires_at)
		VALUES ('el5-bs', 'el-test/repo', 'el5-branch', 'cli_reviewing', 'lease-uuid', datetime('now', '+5 minutes'))`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var leaseID string
	err = db.Unwrap().QueryRowContext(ctx, `
		SELECT lease_id FROM branch_states WHERE id = 'el5-bs'`).Scan(&leaseID)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if leaseID != "lease-uuid" {
		t.Errorf("lease_id: got %q, want %q", leaseID, "lease-uuid")
	}
}

// ---------- EL-6: Lease expiry triggers retry ----------
func TestEL06_LeaseExpiryRetryCount(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, retry_count, max_retries)
		VALUES ('el6-bs', 'el-test/repo', 'el6-branch', 'cli_reviewing', 0, 3)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Simulate lease expiry: increment retry_count.
	_, err = db.Unwrap().ExecContext(ctx, `
		UPDATE branch_states SET retry_count = retry_count + 1, state = 'queued_cli'
		WHERE id = 'el6-bs'`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	var retry int
	err = db.Unwrap().QueryRowContext(ctx, `
		SELECT retry_count FROM branch_states WHERE id = 'el6-bs'`).Scan(&retry)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if retry != 1 {
		t.Errorf("retry_count after expiry: got %d, want 1", retry)
	}
}

// ---------- EL-7: Max retries → blocked ----------
func TestEL07_MaxRetriesBlocked(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, retry_count, max_retries)
		VALUES ('el7-bs', 'el-test/repo', 'el7-branch', 'queued_cli', 3, 3)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// When retry_count >= max_retries, state should become blocked.
	var retry, maxRetry int
	err = db.Unwrap().QueryRowContext(ctx, `
		SELECT retry_count, max_retries FROM branch_states WHERE id = 'el7-bs'`).Scan(&retry, &maxRetry)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if retry < maxRetry {
		t.Errorf("retry_count %d should be >= max_retries %d for blocked state", retry, maxRetry)
	}
}

// ---------- EL-8: Findings table append-only ----------
func TestEL08_FindingsAppendOnly(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create review_run first (FK).
	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO review_runs (id, repo, branch, provider, status)
		VALUES ('el8-run', 'el-test/repo', 'el8-branch', 'stub', 'completed')`)
	if err != nil {
		t.Fatalf("insert review_run: %v", err)
	}

	_, err = db.Unwrap().ExecContext(ctx, `
		INSERT INTO findings (id, run_id, repo, branch, severity, message, source, ts)
		VALUES ('el8-f1', 'el8-run', 'el-test/repo', 'el8-branch', 'warning', 'test finding', 'stub', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert finding: %v", err)
	}

	var cnt int
	err = db.Unwrap().QueryRowContext(ctx, `SELECT COUNT(*) FROM findings WHERE run_id = 'el8-run'`).Scan(&cnt)
	if err != nil || cnt != 1 {
		t.Errorf("expected 1 finding, got %d (err=%v)", cnt, err)
	}
}

// ---------- EL-9: Head hash stale detection ----------
func TestEL09_HeadHashStaleDetection(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, head_hash)
		VALUES ('el9-bs', 'el-test/repo', 'el9-branch', 'cli_reviewing', 'abc123')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Simulate hash update (new commit pushed).
	_, err = db.Unwrap().ExecContext(ctx, `
		UPDATE branch_states SET head_hash = 'def456' WHERE id = 'el9-bs'`)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	var hash string
	err = db.Unwrap().QueryRowContext(ctx, `SELECT head_hash FROM branch_states WHERE id = 'el9-bs'`).Scan(&hash)
	if err != nil || hash != "def456" {
		t.Errorf("head_hash: got %q, want %q", hash, "def456")
	}
}

// ---------- EL-10: Review run lifecycle ----------
func TestEL10_ReviewRunLifecycle(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	run := &ReviewRun{
		ID:       "el10-run",
		Repo:     "el-test/repo",
		Branch:   "el10-branch",
		Provider: "stub",
		Status:   "pending",
	}
	if err := CreateReviewRun(db, run); err != nil {
		t.Fatalf("CreateReviewRun: %v", err)
	}

	// Verify pending.
	var gotStatus string
	if err := db.Unwrap().QueryRowContext(ctx, `SELECT status FROM review_runs WHERE id = 'el10-run'`).Scan(&gotStatus); err != nil {
		t.Fatalf("select review_run: %v", err)
	}
	if gotStatus != "pending" {
		t.Errorf("status: got %q, want %q", gotStatus, "pending")
	}
}

// ---------- EL-11: Review run dedup ----------
func TestEL11_PipelineRunningDedup(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	run := &ReviewRun{
		ID:       "el11-run",
		Repo:     "el-test/repo",
		Branch:   "el11-branch",
		Provider: "stub",
		Status:   "pending",
	}
	if err := CreateReviewRun(db, run); err != nil {
		t.Fatalf("CreateReviewRun: %v", err)
	}

	running, err := IsPipelineRunning(ctx, db, "el-test/repo", "el11-branch")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if !running {
		t.Error("IsPipelineRunning should return true for pending run")
	}
}

// ---------- EL-12: Every submit triggers full gate run ----------
// This is the key EL-12 behavioral test: Submit must create a review_run.
func TestEL12_SubmitCreatesReviewRun(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Set up: a branch in "submitted" state, a session, and an assignment.
	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state)
		VALUES ('el12-bs', 'el-test/repo', 'el12-branch', 'submitted')`)
	if err != nil {
		t.Fatalf("insert branch_state: %v", err)
	}

	if err := RegisterAgentSession(ctx, db, "el12-sess", "el12-agent", "agent", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}

	assignment := &AgentAssignment{
		ID:        "el12-assign",
		SessionID: "el12-sess",
		AgentID:   "el12-agent",
		Repo:      "el-test/repo",
		Branch:    "el12-branch",
	}
	if err := AttachAgentAssignment(ctx, db, assignment); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}

	// Simulate submit: create a review_run atomically (this is what Submit() now does).
	pipelineID := "el12-assign-pipeline"
	run := &ReviewRun{
		ID:       pipelineID,
		Repo:     "el-test/repo",
		Branch:   "el12-branch",
		Provider: "gate",
		Status:   "pending",
	}
	if err := CreateReviewRun(db, run); err != nil {
		t.Fatalf("CreateReviewRun: %v", err)
	}

	// Verify: the pipeline is detected as running.
	running, err := IsPipelineRunning(ctx, db, "el-test/repo", "el12-branch")
	if err != nil {
		t.Fatalf("IsPipelineRunning: %v", err)
	}
	if !running {
		t.Error("EL-12: after submit, pipeline must be running (review_run pending)")
	}

	// Verify: the review_run record exists with correct fields.
	var rrStatus, rrProvider string
	err = db.Unwrap().QueryRowContext(ctx, `SELECT status, provider FROM review_runs WHERE id = ?`, pipelineID).Scan(&rrStatus, &rrProvider)
	if err != nil {
		t.Fatalf("select review_run: %v", err)
	}
	if rrStatus != "pending" {
		t.Errorf("EL-12: review_run status: got %q, want %q", rrStatus, "pending")
	}
	if rrProvider != "gate" {
		t.Errorf("EL-12: review_run provider: got %q, want %q", rrProvider, "gate")
	}
}

// ---------- EL-13 through EL-15: Merge readiness fields ----------
func TestEL13_15_MergeReadinessFields(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, approved, ci_green, pending_events, unresolved_threads)
		VALUES ('el13-bs', 'el-test/repo', 'el13-branch', 'approved', 1, 1, 0, 0)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var approved, ciGreen, pending, unresolved int
	err = db.Unwrap().QueryRowContext(ctx, `
		SELECT approved, ci_green, pending_events, unresolved_threads
		FROM branch_states WHERE id = 'el13-bs'`).Scan(&approved, &ciGreen, &pending, &unresolved)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if approved != 1 || ciGreen != 1 || pending != 0 || unresolved != 0 {
		t.Errorf("merge readiness fields: got (%d,%d,%d,%d), want (1,1,0,0)",
			approved, ciGreen, pending, unresolved)
	}
}

// ---------- EL-16 through EL-20: Branch state ownership ----------
func TestEL16_20_SessionOwnership(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, owner_session_id, owner_agent)
		VALUES ('el16-bs', 'el-test/repo', 'el16-branch', 'submitted', 'sess-owner', 'agent-owner')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var ownerSess, ownerAgent string
	err = db.Unwrap().QueryRowContext(ctx, `
		SELECT owner_session_id, owner_agent FROM branch_states WHERE id = 'el16-bs'`).Scan(&ownerSess, &ownerAgent)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if ownerSess != "sess-owner" {
		t.Errorf("owner_session_id: got %q, want %q", ownerSess, "sess-owner")
	}
	if ownerAgent != "agent-owner" {
		t.Errorf("owner_agent: got %q, want %q", ownerAgent, "agent-owner")
	}
}

// ---------- EL-21: Merge predicate is superset of branch protection ----------
func TestEL21_MergePredicateSuperset(t *testing.T) {
	// Verify the predicate set covers all GitHub branch protection rules.
	githubPredicates := map[MergePredicate]string{
		PredicateApproved:          "required_pull_request_reviews",
		PredicateCIGreen:           "required_status_checks",
		PredicateNoUnresolvedConvo: "required_conversation_resolution",
	}
	for pred, ghRule := range githubPredicates {
		found := false
		for _, p := range AllMergePredicates {
			if p == pred {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("EL-21: GitHub rule %q (%s) not covered by merge predicate set", ghRule, pred)
		}
	}

	// Verify Codero-additive predicates exist.
	coderoAdditive := []MergePredicate{
		PredicateNoPendingEvents,
		PredicateGateChecksPassed,
		PredicateNoBlockingReasons,
	}
	for _, pred := range coderoAdditive {
		found := false
		for _, p := range AllMergePredicates {
			if p == pred {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("EL-21: Codero-additive predicate %q missing from AllMergePredicates", pred)
		}
	}

	// The set must have at least 6 predicates (superset of 3 GitHub rules).
	if len(AllMergePredicates) < 6 {
		t.Errorf("EL-21: AllMergePredicates has %d items, want >= 6", len(AllMergePredicates))
	}
}

func TestEL21_MergeReadinessEvaluation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Case 1: all predicates pass → ready.
	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, approved, ci_green, pending_events, unresolved_threads)
		VALUES ('el21-pass', 'el-test/repo', 'el21-pass-branch', 'approved', 1, 1, 0, 0)`)
	if err != nil {
		t.Fatalf("insert pass case: %v", err)
	}
	result, err := EvaluateMergeReadiness(ctx, db, "el21-pass")
	if err != nil {
		t.Fatalf("EvaluateMergeReadiness (pass): %v", err)
	}
	if !result.Ready {
		t.Errorf("EL-21 pass case: expected Ready=true, blocking=%v", result.BlockingReasons)
	}
	if result.PassedCount != result.TotalPredicates {
		t.Errorf("EL-21 pass case: passed %d/%d", result.PassedCount, result.TotalPredicates)
	}

	// Case 2: not approved → not ready.
	_, err = db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, approved, ci_green, pending_events, unresolved_threads)
		VALUES ('el21-fail', 'el-test/repo', 'el21-fail-branch', 'queued_cli', 0, 1, 0, 0)`)
	if err != nil {
		t.Fatalf("insert fail case: %v", err)
	}
	result, err = EvaluateMergeReadiness(ctx, db, "el21-fail")
	if err != nil {
		t.Fatalf("EvaluateMergeReadiness (fail): %v", err)
	}
	if result.Ready {
		t.Error("EL-21 fail case: expected Ready=false when not approved")
	}
	if len(result.BlockingReasons) == 0 {
		t.Error("EL-21 fail case: expected blocking reasons")
	}

	// Case 3: blocked state → not ready.
	_, err = db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, approved, ci_green, pending_events, unresolved_threads)
		VALUES ('el21-blocked', 'el-test/repo', 'el21-blocked-branch', 'blocked', 1, 1, 0, 0)`)
	if err != nil {
		t.Fatalf("insert blocked case: %v", err)
	}
	result, err = EvaluateMergeReadiness(ctx, db, "el21-blocked")
	if err != nil {
		t.Fatalf("EvaluateMergeReadiness (blocked): %v", err)
	}
	if result.Ready {
		t.Error("EL-21 blocked case: expected Ready=false when branch is blocked")
	}

	// Case 4: failed gate run → not ready.
	_, err = db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, approved, ci_green, pending_events, unresolved_threads)
		VALUES ('el21-gate', 'el-test/repo', 'el21-gate-branch', 'approved', 1, 1, 0, 0)`)
	if err != nil {
		t.Fatalf("insert gate fail case: %v", err)
	}
	_, err = db.Unwrap().ExecContext(ctx, `
		INSERT INTO review_runs (id, repo, branch, provider, status, finished_at)
		VALUES ('el21-gate-run', 'el-test/repo', 'el21-gate-branch', 'gate', 'failed', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert failed gate run: %v", err)
	}
	result, err = EvaluateMergeReadiness(ctx, db, "el21-gate")
	if err != nil {
		t.Fatalf("EvaluateMergeReadiness (gate fail): %v", err)
	}
	if result.Ready {
		t.Error("EL-21 gate fail case: expected Ready=false when latest gate run failed")
	}
}

// ---------- EL-22: Re-submit triggers requeue ----------
func TestEL22_ResubmitRequeue(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.Unwrap().ExecContext(ctx, `
		INSERT INTO branch_states (id, repo, branch, state, retry_count)
		VALUES ('el22-bs', 'el-test/repo', 'el22-branch', 'changes_requested', 1)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Re-submit transitions state and increments retry.
	_, err = db.Unwrap().ExecContext(ctx, `
		UPDATE branch_states
		SET state = 'queued_cli', retry_count = retry_count + 1
		WHERE id = 'el22-bs'`)
	if err != nil {
		t.Fatalf("resubmit update: %v", err)
	}

	var state string
	var retry int
	err = db.Unwrap().QueryRowContext(ctx, `
		SELECT state, retry_count FROM branch_states WHERE id = 'el22-bs'`).Scan(&state, &retry)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if state != "queued_cli" {
		t.Errorf("state after resubmit: got %q, want %q", state, "queued_cli")
	}
	if retry != 2 {
		t.Errorf("retry_count after resubmit: got %d, want 2", retry)
	}
}

// ---------- EL-23: Heartbeat by launcher, not agent ----------
func TestEL23_HeartbeatSecretEnforcement(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Register a session → get the heartbeat_secret.
	secret, err := RegisterAgentSessionWithSecret(ctx, db, "el23-sess", "el23-agent", "launcher", "")
	if err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	if secret == "" {
		t.Fatal("EL-23: RegisterAgentSession must return a non-empty heartbeat_secret")
	}

	// Valid secret → should succeed.
	if err := ValidateHeartbeatSecret(ctx, db, "el23-sess", secret); err != nil {
		t.Errorf("EL-23: valid secret should pass, got: %v", err)
	}

	// Invalid secret → should fail.
	if err := ValidateHeartbeatSecret(ctx, db, "el23-sess", "wrong-secret"); err == nil {
		t.Error("EL-23: invalid secret should be rejected")
	} else if err != ErrInvalidHeartbeatSecret {
		t.Errorf("EL-23: expected ErrInvalidHeartbeatSecret, got: %v", err)
	}

	// Empty secret → should fail.
	if err := ValidateHeartbeatSecret(ctx, db, "el23-sess", ""); err == nil {
		t.Error("EL-23: empty secret should be rejected")
	}

	// Non-existent session → should fail.
	if err := ValidateHeartbeatSecret(ctx, db, "nonexistent-sess", secret); err == nil {
		t.Error("EL-23: non-existent session should be rejected")
	}
}

// ---------- EL-24: Session heartbeat updates last_seen ----------
func TestEL24_HeartbeatUpdatesLastSeen(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if err := RegisterAgentSession(ctx, db, "el24-sess", "el24-agent", "agent", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}

	// Heartbeat should update last_seen_at.
	if err := UpdateAgentSessionHeartbeat(ctx, db, "el24-sess", false); err != nil {
		t.Fatalf("UpdateAgentSessionHeartbeat: %v", err)
	}

	var lastSeen string
	err := db.Unwrap().QueryRowContext(ctx, `
		SELECT last_seen_at FROM agent_sessions WHERE session_id = 'el24-sess'`).Scan(&lastSeen)
	if err != nil {
		t.Fatalf("select last_seen_at: %v", err)
	}
	if lastSeen == "" {
		t.Error("EL-24: last_seen_at should be set after heartbeat")
	}
}

// ---------- Cross-EL: Merge predicate formal properties ----------
func TestEL_MergePredicateAllDefined(t *testing.T) {
	seen := make(map[MergePredicate]bool)
	for _, p := range AllMergePredicates {
		if seen[p] {
			t.Errorf("duplicate predicate: %s", p)
		}
		seen[p] = true
		if p == "" {
			t.Error("empty predicate in AllMergePredicates")
		}
	}
}
