package deliverypipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/gitops"
	"github.com/codero/codero/internal/state"
)

type fakeGitOps struct {
	mu        sync.Mutex
	calls     []string
	stageErr  error
	commitErr error
	pushErr   error
	commitSHA string
	checkLock func() error
}

func (f *fakeGitOps) Stage(worktree string) error {
	f.record("stage")
	if f.checkLock != nil {
		if err := f.checkLock(); err != nil {
			return err
		}
	}
	return f.stageErr
}

func (f *fakeGitOps) Commit(worktree string, _ gitops.CommitOpts) (string, error) {
	f.record("commit")
	if f.commitErr != nil {
		return "", f.commitErr
	}
	if f.commitSHA == "" {
		f.commitSHA = "deadbeef"
	}
	return f.commitSHA, nil
}

func (f *fakeGitOps) Push(worktree, remote, branch string) error {
	f.record("push")
	return f.pushErr
}

func (f *fakeGitOps) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeGitOps) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type fakeGateRunner struct {
	report *gatecheck.Report
	err    error
}

func (f *fakeGateRunner) RunPipeline(ctx context.Context, worktree string, stagedFiles []string) (*gatecheck.Report, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.report != nil {
		return f.report, nil
	}
	return &gatecheck.Report{Result: gatecheck.StatusPass}, nil
}

type fakeGitHub struct {
	created   bool
	prNumber  int
	err       error
	seenCalls int
}

func (f *fakeGitHub) CreatePRIfEnabled(ctx context.Context, repo, head, base, title, body string) (int, bool, error) {
	f.seenCalls++
	if f.err != nil {
		return 0, false, f.err
	}
	if f.prNumber == 0 {
		f.prNumber = 42
	}
	return f.prNumber, f.created, nil
}

func (f *fakeGitHub) TriggerCodeRabbitReview(ctx context.Context, repo string, prNumber int) error {
	f.seenCalls++
	return nil
}

type fakeWriter struct {
	last  FeedbackPackage
	calls int
}

func (f *fakeWriter) WriteTASK(worktree string, task Task) error { return nil }
func (f *fakeWriter) ClearFEEDBACK(worktree string) error        { return nil }
func (f *fakeWriter) WriteFEEDBACK(worktree string, feedback FeedbackPackage) error {
	f.calls++
	f.last = feedback
	return nil
}

type fakeNotifier struct {
	calls []string
}

func (f *fakeNotifier) Notify(worktree, notificationType, assignmentID string) {
	f.calls = append(f.calls, notificationType)
}

func setupPipelineDB(t *testing.T, worktree string) (*state.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pipeline.db")
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	assignmentID := "assign-1"
	_, err = db.Unwrap().Exec(`
		INSERT INTO agent_sessions (session_id, agent_id, mode)
		VALUES ('sess-1', 'agent-1', 'coding')`)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	_, err = db.Unwrap().Exec(`
		INSERT INTO agent_assignments (assignment_id, session_id, agent_id, repo, branch, worktree, task_id)
		VALUES (?, 'sess-1', 'agent-1', 'acme/api', 'feat/test', ?, 'TASK-1')`,
		assignmentID, worktree,
	)
	if err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
	return db, assignmentID
}

func TestPipeline_HappyPath(t *testing.T) {
	worktree := t.TempDir()
	db, assignmentID := setupPipelineDB(t, worktree)

	var states []string
	gitops := &fakeGitOps{
		commitSHA: "abc123",
		checkLock: func() error {
			if !IsLocked(worktree) {
				return errors.New("lock not held")
			}
			return nil
		},
	}
	gate := &fakeGateRunner{report: &gatecheck.Report{Result: gatecheck.StatusPass}}
	gh := &fakeGitHub{created: true}
	writer := &fakeWriter{}
	notifier := &fakeNotifier{}

	p := NewPipeline(PipelineDeps{
		StateDB:    db,
		GitOps:     gitops,
		GateRunner: gate,
		GitHub:     gh,
		Writer:     writer,
		Notifier:   notifier,
		StateHook: func(state string) {
			states = append(states, state)
		},
	})

	if err := p.Submit(context.Background(), assignmentID, worktree); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if IsLocked(worktree) {
		t.Fatalf("expected lock removed after pipeline")
	}
	if got := gitops.snapshot(); !reflect.DeepEqual(got, []string{"stage", "commit", "push"}) {
		t.Fatalf("gitops calls: %#v", got)
	}

	var deliveryState, lastGateResult, lastCommit string
	var lastPushAt sql.NullTime
	var revisionCount int
	err := db.Unwrap().QueryRow(`
		SELECT delivery_state, last_gate_result, last_commit_sha, last_push_at, revision_count
		FROM agent_assignments WHERE assignment_id = ?`,
		assignmentID,
	).Scan(&deliveryState, &lastGateResult, &lastCommit, &lastPushAt, &revisionCount)
	if err != nil {
		t.Fatalf("query assignment: %v", err)
	}
	if deliveryState != stateIdle {
		t.Fatalf("delivery_state=%q, want %q", deliveryState, stateIdle)
	}
	if lastGateResult != "pass" {
		t.Fatalf("last_gate_result=%q, want pass", lastGateResult)
	}
	if lastCommit != "abc123" {
		t.Fatalf("last_commit_sha=%q, want abc123", lastCommit)
	}
	if !lastPushAt.Valid {
		t.Fatalf("last_push_at should be set")
	}
	if revisionCount != 1 {
		t.Fatalf("revision_count=%d, want 1", revisionCount)
	}

	expectedStates := []string{stateStaging, stateGating, stateCommitting, statePushing, statePRManagement, stateMonitoring, stateFeedbackDelivery, stateMergeEvaluation, stateMerging, statePostMerge, stateIdle}
	if !reflect.DeepEqual(states, expectedStates) {
		t.Fatalf("states=%v, want %v", states, expectedStates)
	}
}

func TestPipeline_GateFailureWritesFeedback(t *testing.T) {
	worktree := t.TempDir()
	db, assignmentID := setupPipelineDB(t, worktree)

	gate := &fakeGateRunner{report: &gatecheck.Report{
		Result: gatecheck.StatusFail,
		Checks: []gatecheck.CheckResult{
			{Status: gatecheck.StatusFail, Name: "gitleaks", Findings: []gatecheck.Finding{{Message: "secret"}}},
		},
	}}
	writer := &fakeWriter{}
	p := NewPipeline(PipelineDeps{
		StateDB:    db,
		GitOps:     &fakeGitOps{},
		GateRunner: gate,
		Writer:     writer,
		Notifier:   &fakeNotifier{},
	})

	if err := p.Submit(context.Background(), assignmentID, worktree); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if writer.calls == 0 {
		t.Fatalf("expected feedback to be written")
	}
	if IsLocked(worktree) {
		t.Fatalf("expected lock removed after failure")
	}
}

func TestPipeline_PushFailureWritesFeedback(t *testing.T) {
	worktree := t.TempDir()
	db, assignmentID := setupPipelineDB(t, worktree)

	gitops := &fakeGitOps{pushErr: errors.New("push failed")}
	p := NewPipeline(PipelineDeps{
		StateDB:    db,
		GitOps:     gitops,
		GateRunner: &fakeGateRunner{report: &gatecheck.Report{Result: gatecheck.StatusPass}},
		Writer:     &fakeWriter{},
		Notifier:   &fakeNotifier{},
	})

	if err := p.Submit(context.Background(), assignmentID, worktree); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got := gitops.snapshot(); !reflect.DeepEqual(got, []string{"stage", "commit", "push"}) {
		t.Fatalf("gitops calls: %#v", got)
	}
}

func TestPipeline_ConcurrentSubmit(t *testing.T) {
	worktree := t.TempDir()
	db, assignmentID := setupPipelineDB(t, worktree)

	started := make(chan struct{})
	release := make(chan struct{})

	gate := &fakeGateRunner{}
	gate.report = &gatecheck.Report{Result: gatecheck.StatusPass}

	gitops := &fakeGitOps{}
	gitops.checkLock = func() error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	}

	p := NewPipeline(PipelineDeps{
		StateDB:    db,
		GitOps:     gitops,
		GateRunner: gate,
		Writer:     &fakeWriter{},
		Notifier:   &fakeNotifier{},
	})

	var firstErr error
	done := make(chan struct{})
	go func() {
		firstErr = p.Submit(context.Background(), assignmentID, worktree)
		close(done)
	}()

	<-started
	if err := p.Submit(context.Background(), assignmentID, worktree); !errors.Is(err, ErrPipelineBusy) {
		t.Fatalf("expected ErrPipelineBusy, got %v", err)
	}
	close(release)
	<-done
	if firstErr != nil {
		t.Fatalf("first submit failed: %v", firstErr)
	}
}

func TestPipeline_InvalidTransition(t *testing.T) {
	f := newPipelineFSM()
	if err := f.Event(context.Background(), "commit"); err == nil {
		t.Fatalf("expected invalid transition error")
	}
}

func TestPipeline_ClearStaleLocks(t *testing.T) {
	t.Setenv("CODERO_DELIVERY_LOCK_TIMEOUT", "1s")
	worktree := t.TempDir()
	db, _ := setupPipelineDB(t, worktree)

	meta := LockMeta{
		SessionID:    "sess-1",
		AssignmentID: "assign-1",
		LockedAt:     time.Now().Add(-2 * time.Hour).UTC(),
	}
	data, _ := json.Marshal(meta)
	lockPath := filepath.Join(worktree, coderoDir, lockFileName)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	p := NewPipeline(PipelineDeps{StateDB: db})
	if err := p.ClearStaleLocks(context.Background()); err != nil {
		t.Fatalf("ClearStaleLocks: %v", err)
	}
	if IsLocked(worktree) {
		t.Fatalf("expected stale lock cleared")
	}
}
