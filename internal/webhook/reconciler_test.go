package webhook_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/webhook"
	"github.com/google/uuid"
)

func openTestDB(t *testing.T) *state.DB {
	t.Helper()
	db, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertBranch(t *testing.T, db *state.DB, repo, branch string, st state.State, headHash string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := db.Unwrap().Exec(`
		INSERT INTO branch_states (id, repo, branch, head_hash, state, max_retries, queue_priority)
		VALUES (?, ?, ?, ?, ?, 3, 0)
	`, id, repo, branch, headHash, string(st))
	if err != nil {
		t.Fatalf("insert branch: %v", err)
	}
	return id
}

func insertAssignment(t *testing.T, db *state.DB, repo, branch, worktree, substatus string) *state.AgentAssignment {
	t.Helper()
	ctx := context.Background()
	sessionID := uuid.New().String()
	agentID := "agent-" + sessionID[:8]
	if err := state.RegisterAgentSession(ctx, db, sessionID, agentID, "", ""); err != nil {
		t.Fatalf("RegisterAgentSession: %v", err)
	}
	assignment := &state.AgentAssignment{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		AgentID:   agentID,
		Repo:      repo,
		Branch:    branch,
		Worktree:  worktree,
		TaskID:    "TASK-" + sessionID[:8],
		Substatus: substatus,
	}
	if err := state.AttachAgentAssignment(ctx, db, assignment); err != nil {
		t.Fatalf("AttachAgentAssignment: %v", err)
	}
	return assignment
}

func insertLink(t *testing.T, db *state.DB, assignment *state.AgentAssignment) {
	t.Helper()
	ctx := context.Background()
	link := &state.GitHubLink{
		TaskID:       assignment.TaskID,
		RepoFullName: assignment.Repo,
		BranchName:   assignment.Branch,
		PRNumber:     1,
		PRState:      "open",
	}
	if err := state.UpsertGitHubLink(ctx, db, link); err != nil {
		t.Fatalf("UpsertGitHubLink: %v", err)
	}
}

func insertFeedbackCache(t *testing.T, db *state.DB, assignment *state.AgentAssignment, ci, coderabbit, human string) {
	t.Helper()
	ctx := context.Background()
	fc := &state.FeedbackCache{
		AssignmentID:        assignment.ID,
		SessionID:           assignment.SessionID,
		TaskID:              assignment.TaskID,
		CISnapshot:          ci,
		CoderabbitSnapshot:  coderabbit,
		HumanReviewSnapshot: human,
		CacheHash:           uuid.New().String(),
		SourceStatus:        "{}",
	}
	if err := state.UpsertFeedbackCache(ctx, db, fc); err != nil {
		t.Fatalf("UpsertFeedbackCache: %v", err)
	}
}

func getDBState(t *testing.T, db *state.DB, id string) state.State {
	t.Helper()
	var s string
	if err := db.Unwrap().QueryRow(`SELECT state FROM branch_states WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatalf("get state: %v", err)
	}
	return state.State(s)
}

func TestReconciler_ClosedPR(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feature/x"
	id := insertBranch(t, db, repo, branch, state.StateReviewApproved, "abc123")

	// GitHub says PR is closed.
	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {
				Repo:   repo,
				Branch: branch,
				PROpen: false,
			},
		},
	}

	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if got := getDBState(t, db, id); got != state.StateMerged {
		t.Errorf("state: got %q, want %q (PR closed)", got, state.StateMerged)
	}
}

func TestReconciler_StaleBranch(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feat/stale"
	id := insertBranch(t, db, repo, branch, state.StateReviewApproved, "old-hash")

	// GitHub reports a different HEAD (force-push).
	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {
				Repo:     repo,
				Branch:   branch,
				HeadHash: "new-hash",
				PROpen:   true,
			},
		},
	}

	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if got := getDBState(t, db, id); got != state.StateStale {
		t.Errorf("state: got %q, want %q (force-push detected)", got, state.StateStale)
	}
}

func TestReconciler_MergeReadyTransition(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feat/ready"
	id := insertBranch(t, db, repo, branch, state.StateReviewApproved, "abc123")

	// Set merge-readiness conditions already met.
	if err := state.UpdateMergeReadiness(db, id, true, true, 0, 0); err != nil {
		t.Fatalf("update merge readiness: %v", err)
	}

	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {
				Repo:              repo,
				Branch:            branch,
				HeadHash:          "abc123",
				PROpen:            true,
				Approved:          true,
				CIGreen:           true,
				PendingEvents:     0,
				UnresolvedThreads: 0,
			},
		},
	}

	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if got := getDBState(t, db, id); got != state.StateMergeReady {
		t.Errorf("state: got %q, want %q", got, state.StateMergeReady)
	}
}

func TestReconciler_MergeReadyRevoked(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feat/revoked"
	id := insertBranch(t, db, repo, branch, state.StateMergeReady, "abc123")

	// GitHub says approval was revoked.
	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {
				Repo:     repo,
				Branch:   branch,
				HeadHash: "abc123",
				PROpen:   true,
				Approved: false, // revoked
				CIGreen:  true,
			},
		},
	}

	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if got := getDBState(t, db, id); got != state.StateSubmitted {
		t.Errorf("state: got %q, want %q (approval revoked)", got, state.StateSubmitted)
	}
}

func TestReconciler_PollingOnlyMode(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "polling-branch"
	_ = insertBranch(t, db, repo, branch, state.StateReviewApproved, "abc123")

	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {
				Repo:   repo,
				Branch: branch,
				PROpen: true,
			},
		},
	}

	// Explicitly polling-only mode (webhookEnabled=false).
	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false)

	// Should start and run without panics or errors.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	<-done // ensure Run exits cleanly
}

func TestReconciler_NoPR_ReviewApprovedStateMerged(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "no-pr-reviewed"
	id := insertBranch(t, db, repo, branch, state.StateReviewApproved, "abc123")

	// GitHub returns nil (no PR exists).
	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{},
	}

	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// review_approved + no PR → merged (PR was expected to exist).
	if got := getDBState(t, db, id); got != state.StateMerged {
		t.Errorf("state: got %q, want %q (no PR → merge for review_approved state)", got, state.StateMerged)
	}
}

func TestReconciler_NoPR_SubmittedStateSkipped(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "no-pr-coding"
	id := insertBranch(t, db, repo, branch, state.StateSubmitted, "abc123")

	// GitHub returns nil (no PR). Normal for submitted state.
	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{},
	}

	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	// submitted + no PR → stay submitted (no PR is expected in pre-PR states).
	if got := getDBState(t, db, id); got != state.StateSubmitted {
		t.Errorf("state: got %q, want %q (submitted with no PR should not be closed)", got, state.StateSubmitted)
	}
}

func TestReconciler_AutoMerge_Success(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feat/automerge"
	id := insertBranch(t, db, repo, branch, state.StateReviewApproved, "abc123")

	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {
				Repo:              repo,
				Branch:            branch,
				HeadHash:          "abc123",
				PRNumber:          42,
				PROpen:            true,
				Approved:          true,
				CIGreen:           true,
				PendingEvents:     0,
				UnresolvedThreads: 0,
			},
		},
	}

	merger := &mockAutoMerger{}
	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false).
		WithAutoMerge(merger, "squash")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if !merger.called {
		t.Error("expected MergePR to be called, but it was not")
	}
	if merger.lastPRNumber != 42 {
		t.Errorf("MergePR called with PR#%d, want 42", merger.lastPRNumber)
	}
	if merger.lastMethod != "squash" {
		t.Errorf("MergePR called with method %q, want squash", merger.lastMethod)
	}
	if got := getDBState(t, db, id); got != state.StateMerged {
		t.Errorf("state: got %q, want merged (auto-merged)", got)
	}
}

func TestReconciler_AutoMerge_FailureLeavesMergeReady(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feat/mergefail"
	id := insertBranch(t, db, repo, branch, state.StateMergeReady, "abc123")

	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {
				Repo:     repo,
				Branch:   branch,
				HeadHash: "abc123",
				PRNumber: 7,
				PROpen:   true,
				Approved: true,
				CIGreen:  true,
			},
		},
	}

	merger := &mockAutoMerger{failWith: fmt.Errorf("merge conflict (409)")}
	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false).
		WithAutoMerge(merger, "squash")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rec.Run(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if !merger.called {
		t.Error("expected MergePR to be called")
	}
	// On failure the branch must remain merge_ready, not be closed.
	if got := getDBState(t, db, id); got != state.StateMergeReady {
		t.Errorf("state: got %q, want merge_ready (merge failed)", got)
	}
}

func TestReconciler_AutoMerge_DisabledWhenNoPRNumber(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feat/noprnumber"
	id := insertBranch(t, db, repo, branch, state.StateReviewApproved, "abc123")

	ghClient := &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {
				Repo: repo, Branch: branch, HeadHash: "abc123",
				PRNumber: 0, // unknown
				PROpen:   true, Approved: true, CIGreen: true,
			},
		},
	}

	merger := &mockAutoMerger{}
	rec := webhook.NewReconciler(db, ghClient, []string{repo}, false).
		WithAutoMerge(merger, "squash")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { rec.Run(ctx); close(done) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	if merger.called {
		t.Error("MergePR must not be called when PRNumber is 0")
	}
	// Should still reach merge_ready even without merging.
	if got := getDBState(t, db, id); got != state.StateMergeReady {
		t.Errorf("state: got %q, want merge_ready", got)
	}
}

func TestReconciler_PushesCIFeedback(t *testing.T) {
	db := openTestDB(t)
	repo, branch := "owner/repo", "feat/ci-feedback"
	headHash := "abc123"
	insertBranch(t, db, repo, branch, state.StateReviewApproved, headHash)

	worktree := t.TempDir()
	assignment := insertAssignment(t, db, repo, branch, worktree, state.AssignmentSubstatusWaitingForCI)
	insertLink(t, db, assignment)
	insertFeedbackCache(t, db, assignment, `[{"file":"main.go","line":12,"message":"CI failed"}]`, "", "")

	rec := webhook.NewReconciler(db, &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {Repo: repo, Branch: branch, PROpen: true, HeadHash: headHash},
		},
	}, []string{repo}, false)

	rec.RunOnce(context.Background())

	data, err := os.ReadFile(filepath.Join(worktree, ".codero", "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("read FEEDBACK.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "CI Failures") {
		t.Fatalf("FEEDBACK.md missing CI Failures section:\n%s", content)
	}
	if !strings.Contains(content, "CI failed") {
		t.Fatalf("FEEDBACK.md missing CI message:\n%s", content)
	}
}

func TestReconciler_PushesCodeReviewFeedback(t *testing.T) {
	db := openTestDB(t)
	repo, branch := "owner/repo", "feat/cr-feedback"
	headHash := "def456"
	insertBranch(t, db, repo, branch, state.StateReviewApproved, headHash)

	worktree := t.TempDir()
	assignment := insertAssignment(t, db, repo, branch, worktree, state.AssignmentSubstatusWaitingForCI)
	insertLink(t, db, assignment)
	insertFeedbackCache(t, db, assignment, "", `[{"message":"CodeRabbit wants changes"}]`, "")

	rec := webhook.NewReconciler(db, &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {Repo: repo, Branch: branch, PROpen: true, HeadHash: headHash},
		},
	}, []string{repo}, false)

	rec.RunOnce(context.Background())

	data, err := os.ReadFile(filepath.Join(worktree, ".codero", "FEEDBACK.md"))
	if err != nil {
		t.Fatalf("read FEEDBACK.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Code Review") {
		t.Fatalf("FEEDBACK.md missing Code Review section:\n%s", content)
	}
}

func TestReconciler_NoActionableFeedbackSkipsWrite(t *testing.T) {
	db := openTestDB(t)
	repo, branch := "owner/repo", "feat/clean"
	headHash := "clean123"
	insertBranch(t, db, repo, branch, state.StateReviewApproved, headHash)

	worktree := t.TempDir()
	assignment := insertAssignment(t, db, repo, branch, worktree, state.AssignmentSubstatusWaitingForCI)
	insertLink(t, db, assignment)
	insertFeedbackCache(t, db, assignment, `{"status":"success"}`, "", "")

	rec := webhook.NewReconciler(db, &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {Repo: repo, Branch: branch, PROpen: true, HeadHash: headHash},
		},
	}, []string{repo}, false)

	rec.RunOnce(context.Background())

	if _, err := os.Stat(filepath.Join(worktree, ".codero", "FEEDBACK.md")); !os.IsNotExist(err) {
		t.Fatalf("expected FEEDBACK.md to be absent, got err=%v", err)
	}
}

func TestReconciler_ActionableFeedbackUpdatesSubstatus(t *testing.T) {
	db := openTestDB(t)
	repo, branch := "owner/repo", "feat/substatus"
	headHash := "sub123"
	insertBranch(t, db, repo, branch, state.StateReviewApproved, headHash)

	worktree := t.TempDir()
	assignment := insertAssignment(t, db, repo, branch, worktree, state.AssignmentSubstatusWaitingForMergeApproval)
	insertLink(t, db, assignment)
	insertFeedbackCache(t, db, assignment, `[{"message":"Needs revision"}]`, "", "")

	rec := webhook.NewReconciler(db, &mockGitHubClient{
		state: map[string]*webhook.GitHubState{
			repo + "/" + branch: {Repo: repo, Branch: branch, PROpen: true, HeadHash: headHash},
		},
	}, []string{repo}, false)

	rec.RunOnce(context.Background())

	updated, err := state.GetAgentAssignmentByID(context.Background(), db, assignment.ID)
	if err != nil {
		t.Fatalf("GetAgentAssignmentByID: %v", err)
	}
	if updated.Substatus != state.AssignmentSubstatusNeedsRevision {
		t.Fatalf("substatus: got %q, want %q", updated.Substatus, state.AssignmentSubstatusNeedsRevision)
	}
}

// mockGitHubClient returns pre-configured states for testing.
type mockGitHubClient struct {
	state map[string]*webhook.GitHubState // key: "repo/branch"
}

func (m *mockGitHubClient) GetPRState(_ context.Context, repo, branch string) (*webhook.GitHubState, error) {
	key := repo + "/" + branch
	s, ok := m.state[key]
	if !ok {
		return nil, nil // no PR
	}
	return s, nil
}

// mockAutoMerger records calls to MergePR for assertions.
type mockAutoMerger struct {
	called       bool
	lastPRNumber int
	lastMethod   string
	failWith     error
}

func (m *mockAutoMerger) MergePR(_ context.Context, _ string, prNumber int, _, method string) error {
	m.called = true
	m.lastPRNumber = prNumber
	m.lastMethod = method
	return m.failWith
}
