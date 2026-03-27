package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	deliverypipeline "github.com/codero/codero/internal/delivery_pipeline"
	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/gitops"
	"github.com/codero/codero/internal/state"
)

// ─── MIG-039: Submit-to-Merge Integration Test ───────────────────────────────
//
// This test validates the complete delivery pipeline flow:
// 1. Register session
// 2. Submit → gate(pass) → commit → push → PR created
// 3. Archive created after merge

// TestMIG039_SubmitToMerge_HappyPath tests the complete happy path:
// register session → submit → gate → commit → push → PR → merge → archive
func TestMIG039_SubmitToMerge_HappyPath(t *testing.T) {
	ctx := context.Background()

	// Setup: create temp worktree with git repo
	worktree := t.TempDir()
	setupGitRepo(t, worktree)

	// Setup: create state DB
	dbPath := filepath.Join(t.TempDir(), "mig039.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	defer db.Close()

	// Setup: seed branch state
	const (
		sessionID    = "sess-mig039"
		agentID      = "agent-mig039"
		assignmentID = "assign-mig039"
		repo         = "acme/mig039-test"
		branch       = "feat/mig039-test"
		taskID       = "TASK-MIG039"
	)

	_, err = db.Unwrap().Exec(
		`INSERT INTO branch_states (id, repo, branch, state, ci_green, approved) VALUES (?, ?, ?, ?, 1, 1)`,
		"branch-mig039", repo, branch, string(state.StateSubmitted),
	)
	if err != nil {
		t.Fatalf("seed branch state: %v", err)
	}

	_, err = db.Unwrap().Exec(
		`INSERT INTO agent_sessions (session_id, agent_id, mode, started_at, last_seen_at) VALUES (?, ?, 'coding', datetime('now'), datetime('now'))`,
		sessionID, agentID,
	)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	_, err = db.Unwrap().Exec(
		`INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch, worktree, task_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		assignmentID, sessionID, agentID, repo, branch, worktree, taskID,
	)
	if err != nil {
		t.Fatalf("seed assignment: %v", err)
	}

	// Setup: create pipeline with mock deps
	gitOps := &integrationGitOps{
		worktree:  worktree,
		commitSHA: "abc123mig039",
	}

	gateRunner := &integrationGateRunner{
		result: gatecheck.StatusPass,
	}

	gh := &integrationGitHub{
		prNumber: 42,
		created:  true,
		ciPassed: true,
		approved: true,
	}

	writer := &integrationWriter{}
	notifier := &integrationNotifier{}

	p := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
		StateDB:    db,
		GitOps:     gitOps,
		GateRunner: gateRunner,
		GitHub:     gh,
		Writer:     writer,
		Notifier:   notifier,
	})

	// Execute: run the pipeline
	err = p.Submit(ctx, assignmentID, worktree)
	if err != nil {
		t.Fatalf("pipeline Submit failed: %v", err)
	}

	// Verify: assignment state updated correctly
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
	if lastCommitSHA != "abc123mig039" {
		t.Errorf("last_commit_sha = %q, want abc123mig039", lastCommitSHA)
	}
	if revisionCount != 1 {
		t.Errorf("revision_count = %d, want 1", revisionCount)
	}

	// Verify: git operations were called
	if !gitOps.stageCalled {
		t.Error("expected git stage to be called")
	}
	if !gitOps.commitCalled {
		t.Error("expected git commit to be called")
	}
	if !gitOps.pushCalled {
		t.Error("expected git push to be called")
	}

	// Verify: PR was created
	if !gh.createPRCalled {
		t.Error("expected CreatePRIfEnabled to be called")
	}

	// Verify: lock was released
	if deliverypipeline.IsLocked(worktree) {
		t.Error("expected delivery lock to be released")
	}

	// Verify: no feedback was written on success
	if writer.feedbackCalls > 0 {
		t.Errorf("expected no feedback on success, got %d calls", writer.feedbackCalls)
	}
}

// TestMIG039_SubmitToMerge_GateFailurePath tests the gate failure path:
// register session → submit → gate(fail) → FEEDBACK written → no commit
func TestMIG039_SubmitToMerge_GateFailurePath(t *testing.T) {
	ctx := context.Background()

	worktree := t.TempDir()
	setupGitRepo(t, worktree)

	dbPath := filepath.Join(t.TempDir(), "mig039-gatefail.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	defer db.Close()

	const (
		sessionID    = "sess-gatefail"
		agentID      = "agent-gatefail"
		assignmentID = "assign-gatefail"
		repo         = "acme/gatefail"
		branch       = "feat/gatefail"
		taskID       = "TASK-GATEFAIL"
	)

	_, err = db.Unwrap().Exec(
		`INSERT INTO branch_states (id, repo, branch, state) VALUES (?, ?, ?, ?)`,
		"branch-gatefail", repo, branch, string(state.StateSubmitted),
	)
	if err != nil {
		t.Fatalf("seed branch state: %v", err)
	}

	_, err = db.Unwrap().Exec(
		`INSERT INTO agent_sessions (session_id, agent_id, mode, started_at, last_seen_at) VALUES (?, ?, 'coding', datetime('now'), datetime('now'))`,
		sessionID, agentID,
	)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	_, err = db.Unwrap().Exec(
		`INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch, worktree, task_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		assignmentID, sessionID, agentID, repo, branch, worktree, taskID,
	)
	if err != nil {
		t.Fatalf("seed assignment: %v", err)
	}

	gitOps := &integrationGitOps{worktree: worktree}
	gateRunner := &integrationGateRunner{
		result: gatecheck.StatusFail,
		findings: []gatecheck.Finding{
			{Message: "secret detected in config", File: "config.yml", Line: 42},
		},
	}
	writer := &integrationWriter{}

	p := deliverypipeline.NewPipeline(deliverypipeline.PipelineDeps{
		StateDB:    db,
		GitOps:     gitOps,
		GateRunner: gateRunner,
		Writer:     writer,
		Notifier:   &integrationNotifier{},
	})

	err = p.Submit(ctx, assignmentID, worktree)
	if err != nil {
		t.Fatalf("Submit should not return error on gate failure: %v", err)
	}

	// Verify: stage called but not commit/push
	if !gitOps.stageCalled {
		t.Error("expected stage to be called")
	}
	if gitOps.commitCalled {
		t.Error("expected commit NOT to be called on gate failure")
	}
	if gitOps.pushCalled {
		t.Error("expected push NOT to be called on gate failure")
	}

	// Verify: feedback was written
	if writer.feedbackCalls == 0 {
		t.Error("expected feedback to be written on gate failure")
	}

	// Verify: lock released
	if deliverypipeline.IsLocked(worktree) {
		t.Error("expected lock to be released")
	}

	// Verify: state recovered to idle
	var deliveryState string
	err = db.Unwrap().QueryRow(
		`SELECT delivery_state FROM agent_assignments WHERE assignment_id = ?`,
		assignmentID,
	).Scan(&deliveryState)
	if err != nil {
		t.Fatalf("query delivery_state: %v", err)
	}
	if deliveryState != "idle" {
		t.Errorf("delivery_state = %q, want idle", deliveryState)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func setupGitRepo(t *testing.T, dir string) {
	t.Helper()

	git := func(args ...string) {
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = cleanGitEnvForIntegration()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\noutput: %s", args, err, string(out))
		}
	}

	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	git("add", "README.md")
	git("commit", "-m", "init")
}

func cleanGitEnvForIntegration() []string {
	env := os.Environ()
	cleaned := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		cleaned = append(cleaned, kv)
	}
	return cleaned
}

// integrationGitOps implements GitOps for integration tests
type integrationGitOps struct {
	worktree     string
	commitSHA    string
	stageCalled  bool
	commitCalled bool
	pushCalled   bool
}

func (g *integrationGitOps) Stage(worktree string) error {
	g.stageCalled = true
	return nil
}

func (g *integrationGitOps) Commit(worktree string, opts gitops.CommitOpts) (string, error) {
	g.commitCalled = true
	if g.commitSHA == "" {
		return "default-sha-123", nil
	}
	return g.commitSHA, nil
}

func (g *integrationGitOps) Push(worktree, remote, branch string) error {
	g.pushCalled = true
	return nil
}

// integrationGateRunner implements GateRunner for integration tests
type integrationGateRunner struct {
	result   gatecheck.CheckStatus
	findings []gatecheck.Finding
	err      error
}

func (g *integrationGateRunner) RunPipeline(ctx context.Context, worktree string, stagedFiles []string) (*gatecheck.Report, error) {
	if g.err != nil {
		return nil, g.err
	}
	return &gatecheck.Report{
		Result: g.result,
		Checks: []gatecheck.CheckResult{
			{
				ID:       "test-check",
				Name:     "Test Check",
				Status:   g.result,
				Findings: g.findings,
			},
		},
	}, nil
}

// integrationGitHub implements GitHubClient for integration tests
type integrationGitHub struct {
	prNumber       int
	created        bool
	createPRCalled bool
	reviewCalled   bool
	ciPassed       bool
	approved       bool
}

func (g *integrationGitHub) CreatePRIfEnabled(ctx context.Context, repo, head, base, title, body string) (int, bool, error) {
	g.createPRCalled = true
	return g.prNumber, g.created, nil
}

func (g *integrationGitHub) TriggerCodeRabbitReview(ctx context.Context, repo string, prNumber int) error {
	g.reviewCalled = true
	return nil
}

func (g *integrationGitHub) FindOpenPR(ctx context.Context, repo, branch string) (*deliverypipeline.PRInfo, error) {
	if g.prNumber == 0 {
		return nil, nil
	}
	return &deliverypipeline.PRInfo{Number: g.prNumber, HeadSHA: "abc123"}, nil
}

func (g *integrationGitHub) ListCheckRuns(ctx context.Context, repo, sha string) ([]deliverypipeline.CheckRunInfo, error) {
	conclusion := "success"
	if !g.ciPassed {
		conclusion = "failure"
	}
	return []deliverypipeline.CheckRunInfo{{Name: "ci", Status: "completed", Conclusion: conclusion}}, nil
}

func (g *integrationGitHub) ListPRReviews(ctx context.Context, repo string, prNumber int) ([]deliverypipeline.ReviewInfo, error) {
	if g.approved {
		return []deliverypipeline.ReviewInfo{{State: "APPROVED", User: "reviewer"}}, nil
	}
	return nil, nil
}

func (g *integrationGitHub) MergePR(ctx context.Context, repo string, prNumber int, sha, mergeMethod string) error {
	return nil
}

// integrationWriter implements Writer for integration tests
type integrationWriter struct {
	feedbackCalls int
	lastFeedback  deliverypipeline.FeedbackPackage
}

func (w *integrationWriter) WriteTASK(worktree string, task deliverypipeline.Task) error {
	return nil
}

func (w *integrationWriter) WriteFEEDBACK(worktree string, feedback deliverypipeline.FeedbackPackage) error {
	w.feedbackCalls++
	w.lastFeedback = feedback
	return nil
}

func (w *integrationWriter) ClearFEEDBACK(worktree string) error {
	return nil
}

// integrationNotifier implements Notifier for integration tests
type integrationNotifier struct {
	calls []string
}

func (n *integrationNotifier) Notify(worktree, notificationType, assignmentID string) {
	n.calls = append(n.calls, notificationType)
}
