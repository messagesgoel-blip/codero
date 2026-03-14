package webhook_test

import (
	"context"
	"path/filepath"
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
	id := insertBranch(t, db, repo, branch, state.StateReviewed, "abc123")

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

	if got := getDBState(t, db, id); got != state.StateClosed {
		t.Errorf("state: got %q, want %q (PR closed)", got, state.StateClosed)
	}
}

func TestReconciler_StaleBranch(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feat/stale"
	id := insertBranch(t, db, repo, branch, state.StateReviewed, "old-hash")

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

	if got := getDBState(t, db, id); got != state.StateStaleBranch {
		t.Errorf("state: got %q, want %q (force-push detected)", got, state.StateStaleBranch)
	}
}

func TestReconciler_MergeReadyTransition(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "feat/ready"
	id := insertBranch(t, db, repo, branch, state.StateReviewed, "abc123")

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

	if got := getDBState(t, db, id); got != state.StateCoding {
		t.Errorf("state: got %q, want %q (approval revoked)", got, state.StateCoding)
	}
}

func TestReconciler_PollingOnlyMode(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "polling-branch"
	_ = insertBranch(t, db, repo, branch, state.StateReviewed, "abc123")

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

func TestReconciler_NoPR_ReviewedStateClosed(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "no-pr-reviewed"
	id := insertBranch(t, db, repo, branch, state.StateReviewed, "abc123")

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

	// reviewed + no PR → closed (PR was expected to exist).
	if got := getDBState(t, db, id); got != state.StateClosed {
		t.Errorf("state: got %q, want %q (no PR → close for reviewed state)", got, state.StateClosed)
	}
}

func TestReconciler_NoPR_CodingStateSkipped(t *testing.T) {
	db := openTestDB(t)

	repo, branch := "owner/repo", "no-pr-coding"
	id := insertBranch(t, db, repo, branch, state.StateCoding, "abc123")

	// GitHub returns nil (no PR). Normal for coding state.
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

	// coding + no PR → stay coding (no PR is expected in pre-PR states).
	if got := getDBState(t, db, id); got != state.StateCoding {
		t.Errorf("state: got %q, want %q (coding with no PR should not be closed)", got, state.StateCoding)
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
