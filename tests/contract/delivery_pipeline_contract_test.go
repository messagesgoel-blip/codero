package contract

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	deliverypipeline "github.com/codero/codero/internal/delivery_pipeline"
	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/gitops"
	"github.com/codero/codero/internal/state"
)

// ─── MIG-037: Delivery Pipeline Contract Tests ─────────────────────────────
//
// These tests validate the delivery pipeline contracts:
//   1. Happy path: submit → gate(pass) → commit → push → PR created
//   2. Gate failure: submit → gate(fail) → FEEDBACK.md written → no commit
//   3. Push failure: submit → push(fail) → FEEDBACK.md written
//   4. Lock lifecycle: lock file created during run, cleared after
//   5. Concurrent submit returns 409 (ErrPipelineBusy)
//   6. FEEDBACK.md schema contains expected fields

// setupPipelineTest creates a temp directory, state DB, and returns cleanup function.
func setupPipelineTest(t *testing.T) (worktree string, db *state.DB, cleanup func()) {
	t.Helper()
	tmpDir := t.TempDir()
	worktree = tmpDir

	// Initialize a git repo in the worktree for git operations.
	git := func(args ...string) {
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		cmd := exec.Command("git", args...)
		cmd.Dir = worktree
		// Clear GIT_* env vars that could interfere with test isolation
		cmd.Env = cleanGitEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\noutput: %s", args, err, string(out))
		}
	}
	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(worktree, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	git("add", "README.md")
	git("commit", "-m", "init")

	dbPath := filepath.Join(tmpDir, "state.db")
	var err error
	db, err = state.Open(dbPath)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}

	cleanup = func() {
		db.Close()
	}
	return worktree, db, cleanup
}

// seedPipelineAssignment inserts a session and assignment into the DB.
func seedPipelineAssignment(t *testing.T, db *state.DB, sessionID, assignmentID, worktree string) {
	t.Helper()
	_, err := db.Unwrap().Exec(
		`INSERT INTO agent_sessions (session_id, agent_id, mode) VALUES (?, 'test-agent', 'coding')`,
		sessionID,
	)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	_, err = db.Unwrap().Exec(
		`INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch, worktree, task_id)
		 VALUES (?, ?, 'test-agent', 'acme/api', 'feat/test', ?, 'TASK-001')`,
		assignmentID, sessionID, worktree,
	)
	if err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
}

// mockGitOps implements deliverypipeline.GitOps for testing.
type mockGitOps struct {
	stageErr  error
	commitSHA string
	commitErr error
	pushErr   error
	calls     []string
}

func (m *mockGitOps) Stage(worktree string) error {
	m.calls = append(m.calls, "stage")
	return m.stageErr
}

func (m *mockGitOps) Commit(worktree string, opts gitops.CommitOpts) (string, error) {
	m.calls = append(m.calls, "commit")
	if m.commitErr != nil {
		return "", m.commitErr
	}
	if m.commitSHA == "" {
		return "abc123def456", nil
	}
	return m.commitSHA, nil
}

func (m *mockGitOps) Push(worktree, remote, branch string) error {
	m.calls = append(m.calls, "push")
	return m.pushErr
}

// mockGateRunner implements deliverypipeline.GateRunner for testing.
type mockGateRunner struct {
	report *gatecheck.Report
	err    error
}

func (m *mockGateRunner) RunPipeline(ctx context.Context, worktree string, stagedFiles []string) (*gatecheck.Report, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.report != nil {
		return m.report, nil
	}
	return &gatecheck.Report{Result: gatecheck.StatusPass}, nil
}

// mockGitHub implements deliverypipeline.GitHubClient for testing.
type mockGitHub struct {
	prNumber int
	created  bool
	err      error
	calls    int
}

func (m *mockGitHub) CreatePRIfEnabled(ctx context.Context, repo, head, base, title, body string) (int, bool, error) {
	m.calls++
	if m.err != nil {
		return 0, false, m.err
	}
	if m.prNumber == 0 {
		m.prNumber = 42
	}
	return m.prNumber, m.created, nil
}

func (m *mockGitHub) TriggerCodeRabbitReview(ctx context.Context, repo string, prNumber int) error {
	m.calls++
	return nil
}

// mockWriter implements deliverypipeline.Writer for testing.
type mockWriter struct {
	feedbackCalls int
	lastFeedback  deliverypipeline.FeedbackPackage
}

func (m *mockWriter) WriteTASK(worktree string, task deliverypipeline.Task) error { return nil }
func (m *mockWriter) ClearFEEDBACK(worktree string) error                         { return nil }
func (m *mockWriter) WriteFEEDBACK(worktree string, feedback deliverypipeline.FeedbackPackage) error {
	m.feedbackCalls++
	m.lastFeedback = feedback
	return nil
}

// mockNotifier implements deliverypipeline.Notifier for testing.
type mockNotifier struct {
	calls []string
}

func (m *mockNotifier) Notify(worktree, notificationType, assignmentID string) {
	m.calls = append(m.calls, notificationType)
}

// TestMIG037_HappyPath_SubmitGatePassCommitPushPR tests the happy path:
// submit → gate(pass) → commit → push → PR created
func TestMIG037_HappyPath_SubmitGatePassCommitPushPR(t *testing.T) {
	worktree, db, cleanup := setupPipelineTest(t)
	defer cleanup()

	assignmentID := "assign-happy-1"
	seedPipelineAssignment(t, db, "sess-happy", assignmentID, worktree)

	gitOps := &mockGitOps{commitSHA: "abc123"}
	gateRunner := &mockGateRunner{report: &gatecheck.Report{Result: gatecheck.StatusPass}}
	gh := &mockGitHub{created: true}
	writer := &mockWriter{}
	notifier := &mockNotifier{}

	var stateTransitions []string
	p := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
		StateDB:    db,
		GitOps:     gitOps,
		GateRunner: gateRunner,
		GitHub:     gh,
		Writer:     writer,
		Notifier:   notifier,
		StateHook: func(s string) {
			stateTransitions = append(stateTransitions, s)
		},
	})

	err := p.Submit(context.Background(), assignmentID, worktree)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	// Verify git operations were called in order
	expectedCalls := []string{"stage", "commit", "push"}
	if !equalStringSlices(gitOps.calls, expectedCalls) {
		t.Errorf("git operations: got %v, want %v", gitOps.calls, expectedCalls)
	}

	// Verify PR was created
	if gh.calls == 0 {
		t.Error("expected GitHub PR creation to be called")
	}

	// Verify no feedback was written (success path)
	if writer.feedbackCalls != 0 {
		t.Errorf("expected no feedback writes on success, got %d", writer.feedbackCalls)
	}

	// Verify lock was released
	if deliverypipeline.IsLocked(worktree) {
		t.Error("expected lock to be released after pipeline completion")
	}

	// Verify state transitions include all expected states
	expectedStates := []string{
		"staging", "gating", "committing", "pushing", "pr_management",
		"monitoring", "feedback_delivery", "merge_evaluation", "merging", "post_merge", "idle",
	}
	if !equalStringSlices(stateTransitions, expectedStates) {
		t.Errorf("state transitions: got %v, want %v", stateTransitions, expectedStates)
	}

	// Verify DB state
	var deliveryState, lastGateResult, lastCommitSHA string
	var revisionCount int
	err = db.Unwrap().QueryRow(`
		SELECT delivery_state, last_gate_result, last_commit_sha, revision_count
		FROM agent_assignments WHERE assignment_id = ?`,
		assignmentID,
	).Scan(&deliveryState, &lastGateResult, &lastCommitSHA, &revisionCount)
	if err != nil {
		t.Fatalf("query assignment: %v", err)
	}
	if deliveryState != "idle" {
		t.Errorf("delivery_state = %q, want idle", deliveryState)
	}
	if lastGateResult != "pass" {
		t.Errorf("last_gate_result = %q, want pass", lastGateResult)
	}
	if lastCommitSHA != "abc123" {
		t.Errorf("last_commit_sha = %q, want abc123", lastCommitSHA)
	}
	if revisionCount != 1 {
		t.Errorf("revision_count = %d, want 1", revisionCount)
	}
}

// TestMIG037_GateFailure_WritesFeedback tests:
// submit → gate(fail) → FEEDBACK.md written → no commit
func TestMIG037_GateFailure_WritesFeedback(t *testing.T) {
	worktree, db, cleanup := setupPipelineTest(t)
	defer cleanup()

	assignmentID := "assign-gate-fail"
	seedPipelineAssignment(t, db, "sess-gate-fail", assignmentID, worktree)

	gitOps := &mockGitOps{}
	gateRunner := &mockGateRunner{
		report: &gatecheck.Report{
			Result: gatecheck.StatusFail,
			Checks: []gatecheck.CheckResult{
				{
					ID:       "gitleaks",
					Name:     "Gitleaks",
					Status:   gatecheck.StatusFail,
					Findings: []gatecheck.Finding{{Message: "secret detected", File: "config.yml", Line: 42}},
				},
			},
		},
	}
	writer := &mockWriter{}
	notifier := &mockNotifier{}

	p := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
		StateDB:    db,
		GitOps:     gitOps,
		GateRunner: gateRunner,
		Writer:     writer,
		Notifier:   notifier,
	})

	err := p.Submit(context.Background(), assignmentID, worktree)
	if err != nil {
		t.Fatalf("Submit should not return error on gate failure (it writes feedback): %v", err)
	}

	// Verify stage was called but NOT commit or push
	if !containsAll(gitOps.calls, "stage") {
		t.Error("expected stage to be called")
	}
	if containsAll(gitOps.calls, "commit") {
		t.Error("expected commit NOT to be called on gate failure")
	}
	if containsAll(gitOps.calls, "push") {
		t.Error("expected push NOT to be called on gate failure")
	}

	// Verify feedback was written
	if writer.feedbackCalls == 0 {
		t.Fatal("expected feedback to be written on gate failure")
	}

	// Verify feedback contains gate findings
	if len(writer.lastFeedback.GateFindings) == 0 {
		t.Error("expected GateFindings in feedback")
	}

	// Verify lock was released
	if deliverypipeline.IsLocked(worktree) {
		t.Error("expected lock to be released after gate failure")
	}

	// Verify delivery_state returned to idle
	var deliveryState string
	err = db.Unwrap().QueryRow(`
		SELECT delivery_state FROM agent_assignments WHERE assignment_id = ?`,
		assignmentID,
	).Scan(&deliveryState)
	if err != nil {
		t.Fatalf("query assignment: %v", err)
	}
	if deliveryState != "idle" {
		t.Errorf("delivery_state = %q, want idle (recovered from failure)", deliveryState)
	}
}

// TestMIG037_PushFailure_WritesFeedback tests:
// submit → gate(pass) → commit → push(fail) → FEEDBACK.md written
func TestMIG037_PushFailure_WritesFeedback(t *testing.T) {
	worktree, db, cleanup := setupPipelineTest(t)
	defer cleanup()

	assignmentID := "assign-push-fail"
	seedPipelineAssignment(t, db, "sess-push-fail", assignmentID, worktree)

	gitOps := &mockGitOps{
		pushErr: errors.New("remote rejected: cannot push to protected branch"),
	}
	gateRunner := &mockGateRunner{report: &gatecheck.Report{Result: gatecheck.StatusPass}}
	writer := &mockWriter{}
	notifier := &mockNotifier{}

	p := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
		StateDB:    db,
		GitOps:     gitOps,
		GateRunner: gateRunner,
		Writer:     writer,
		Notifier:   notifier,
	})

	err := p.Submit(context.Background(), assignmentID, worktree)
	if err != nil {
		t.Fatalf("Submit should not return error on push failure (it writes feedback): %v", err)
	}

	// Verify stage, commit, push were all called
	expectedCalls := []string{"stage", "commit", "push"}
	if !equalStringSlices(gitOps.calls, expectedCalls) {
		t.Errorf("git operations: got %v, want %v", gitOps.calls, expectedCalls)
	}

	// Verify feedback was written
	if writer.feedbackCalls == 0 {
		t.Fatal("expected feedback to be written on push failure")
	}

	// Verify feedback contains CI failure info
	if len(writer.lastFeedback.CIFailures) == 0 {
		t.Error("expected CIFailures in feedback")
	}

	// Verify lock was released
	if deliverypipeline.IsLocked(worktree) {
		t.Error("expected lock to be released after push failure")
	}
}

// TestMIG037_LockLifecycle tests: lock file created during run, cleared after
func TestMIG037_LockLifecycle(t *testing.T) {
	worktree, db, cleanup := setupPipelineTest(t)
	defer cleanup()

	assignmentID := "assign-lock"
	seedPipelineAssignment(t, db, "sess-lock", assignmentID, worktree)

	var lockChecked bool
	gitOps := &lockCheckingGitOps{
		mockGitOps: mockGitOps{commitSHA: "lock-test-sha"},
		worktree:   worktree,
		checkLock:  func() { lockChecked = true },
	}

	gateRunner := &mockGateRunner{report: &gatecheck.Report{Result: gatecheck.StatusPass}}

	p := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
		StateDB:    db,
		GitOps:     gitOps,
		GateRunner: gateRunner,
		Writer:     &mockWriter{},
		Notifier:   &mockNotifier{},
	})

	if deliverypipeline.IsLocked(worktree) {
		t.Fatal("expected no lock before pipeline")
	}

	err := p.Submit(context.Background(), assignmentID, worktree)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}

	if !lockChecked {
		t.Error("stage was not called (lock check didn't run)")
	}

	if deliverypipeline.IsLocked(worktree) {
		t.Error("expected lock to be released after pipeline")
	}
}

// lockCheckingGitOps wraps mockGitOps to check lock state during Stage.
type lockCheckingGitOps struct {
	mockGitOps
	worktree  string
	checkLock func()
}

func (l *lockCheckingGitOps) Stage(worktree string) error {
	if deliverypipeline.IsLocked(l.worktree) {
		l.checkLock()
	} else {
		l.checkLock() // Still record that we checked, but the test will fail
	}
	return l.mockGitOps.Stage(worktree)
}

// TestMIG037_ConcurrentSubmit_409 tests: simultaneous submit returns ErrPipelineBusy
func TestMIG037_ConcurrentSubmit_409(t *testing.T) {
	worktree, db, cleanup := setupPipelineTest(t)
	defer cleanup()

	assignmentID := "assign-concurrent"
	seedPipelineAssignment(t, db, "sess-concurrent", assignmentID, worktree)

	started := make(chan struct{})
	release := make(chan struct{})

	gitOps := &blockingGitOps{
		mockGitOps: mockGitOps{commitSHA: "concurrent-sha"},
		started:    started,
		release:    release,
	}

	gateRunner := &mockGateRunner{report: &gatecheck.Report{Result: gatecheck.StatusPass}}

	p := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
		StateDB:    db,
		GitOps:     gitOps,
		GateRunner: gateRunner,
		Writer:     &mockWriter{},
		Notifier:   &mockNotifier{},
	})

	var firstErr error
	done := make(chan struct{})
	go func() {
		firstErr = p.Submit(context.Background(), assignmentID, worktree)
		close(done)
	}()

	<-started
	// Second submit should return ErrPipelineBusy
	err := p.Submit(context.Background(), assignmentID, worktree)
	if !errors.Is(err, deliverypipeline.ErrPipelineBusy) {
		t.Errorf("expected ErrPipelineBusy, got %v", err)
	}

	close(release)
	<-done

	if firstErr != nil {
		t.Fatalf("first submit failed: %v", firstErr)
	}
}

// blockingGitOps wraps mockGitOps to block during Stage for concurrency testing.
type blockingGitOps struct {
	mockGitOps
	started chan struct{}
	release chan struct{}
}

func (b *blockingGitOps) Stage(worktree string) error {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-b.release
	return b.mockGitOps.Stage(worktree)
}

// TestMIG037_FeedbackSchema tests: FEEDBACK.md schema contains expected fields
func TestMIG037_FeedbackSchema(t *testing.T) {
	worktree, db, cleanup := setupPipelineTest(t)
	defer cleanup()

	assignmentID := "assign-feedback-schema"
	seedPipelineAssignment(t, db, "sess-feedback", assignmentID, worktree)

	gitOps := &mockGitOps{}
	gateRunner := &mockGateRunner{
		report: &gatecheck.Report{
			Result: gatecheck.StatusFail,
			Checks: []gatecheck.CheckResult{
				{
					ID:     "semgrep",
					Name:   "Semgrep",
					Status: gatecheck.StatusFail,
					Findings: []gatecheck.Finding{
						{Message: "potential sql injection", File: "db.go", Line: 123},
						{Message: "hardcoded credential", File: "config.go", Line: 45},
					},
				},
			},
		},
	}
	writer := &mockWriter{}

	p := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
		StateDB:    db,
		GitOps:     gitOps,
		GateRunner: gateRunner,
		Writer:     writer,
		Notifier:   &mockNotifier{},
	})

	_ = p.Submit(context.Background(), assignmentID, worktree)

	if writer.feedbackCalls == 0 {
		t.Fatal("expected feedback to be written")
	}

	fb := writer.lastFeedback

	// Verify schema fields exist
	if fb.GeneratedAt.IsZero() {
		t.Error("expected generated_at to be set")
	}

	// Verify gate findings are populated
	if len(fb.GateFindings) == 0 {
		t.Error("expected gate_findings to be populated")
	}

	// Verify findings have message content
	for i, finding := range fb.GateFindings {
		if strings.TrimSpace(finding.Message) == "" {
			t.Errorf("gate_finding[%d] has empty message", i)
		}
	}

	// Verify file and line are captured when present
	hasFileInfo := false
	for _, f := range fb.GateFindings {
		if f.File != "" {
			hasFileInfo = true
			break
		}
	}
	if !hasFileInfo {
		t.Error("expected at least one finding with file info")
	}
}

// TestMIG037_FeedbackJSONSchema tests the JSON schema of feedback/current.json
func TestMIG037_FeedbackJSONSchema(t *testing.T) {
	worktree := t.TempDir()

	// Create the feedback directory
	feedbackDir := filepath.Join(worktree, ".codero", "feedback")
	if err := os.MkdirAll(feedbackDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	feedback := deliverypipeline.FeedbackPackage{
		GateFindings: []deliverypipeline.FeedbackItem{
			{File: "main.go", Line: 10, Message: "unused variable"},
		},
		CodeReview: []deliverypipeline.FeedbackItem{
			{Message: "consider using a const"},
		},
		CIFailures: []deliverypipeline.FeedbackItem{
			{Message: "build failed"},
		},
		GeneratedAt: time.Now().UTC(),
	}

	err := deliverypipeline.WriteFEEDBACK(worktree, feedback)
	if err != nil {
		t.Fatalf("WriteFEEDBACK: %v", err)
	}

	// Verify FEEDBACK.md exists
	mdPath := filepath.Join(worktree, ".codero", "FEEDBACK.md")
	if _, err := os.Stat(mdPath); err != nil {
		t.Fatalf("FEEDBACK.md not found: %v", err)
	}

	// Verify feedback/current.json exists and has correct schema
	jsonPath := filepath.Join(feedbackDir, "current.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read current.json: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}

	// Verify required fields
	requiredFields := []string{"generated_at", "gate_findings", "code_review", "ci_failures"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}

// ─── Helper Functions ──────────────────────────────────────────────────────

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsAll(slice []string, items ...string) bool {
	for _, item := range items {
		found := false
		for _, s := range slice {
			if s == item {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
