package gitops

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initTestRepo creates a temp directory with an initialized git repo and initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0644); err != nil {
		t.Fatalf("write .gitkeep: %v", err)
	}
	if _, err := wt.Add(".gitkeep"); err != nil {
		t.Fatalf("add .gitkeep: %v", err)
	}
	_, err = wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@test.com", When: time.Now()},
	})
	if err != nil {
		t.Fatalf("initial commit: %v", err)
	}
	return dir
}

func TestStageAndCommit(t *testing.T) {
	dir := initTestRepo(t)

	// Create a new file
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := Stage(dir); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	msg := FormatMessage("TASK-1", 1, "add hello")
	sha, err := Commit(dir, CommitOpts{
		Message:        msg,
		AuthorName:     "Alice",
		AuthorEmail:    "alice@example.com",
		CommitterName:  "Bot",
		CommitterEmail: "bot@example.com",
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// SHA must be 40 hex chars
	if matched := regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(sha); !matched {
		t.Fatalf("expected 40-char hex SHA, got %q", sha)
	}

	// Verify via go-git
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("commit object: %v", err)
	}
	if commit.Message != msg {
		t.Errorf("message = %q, want %q", commit.Message, msg)
	}
	if commit.Author.Name != "Alice" {
		t.Errorf("author name = %q, want %q", commit.Author.Name, "Alice")
	}
	if commit.Committer.Name != "Bot" {
		t.Errorf("committer name = %q, want %q", commit.Committer.Name, "Bot")
	}
}

func TestCommit_NothingToCommit(t *testing.T) {
	dir := initTestRepo(t)

	// No changes made — commit should fail
	_, err := Commit(dir, CommitOpts{
		Message:        "empty",
		AuthorName:     "A",
		AuthorEmail:    "a@a.com",
		CommitterName:  "B",
		CommitterEmail: "b@b.com",
	})
	if err == nil {
		t.Fatal("expected error for nothing to commit")
	}
	if !strings.Contains(err.Error(), "nothing to commit") {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), "nothing to commit")
	}
}

func TestFormatMessage(t *testing.T) {
	got := FormatMessage("TASK-1", 2, "fix bug")
	want := "[codero] TASK-1 v2: fix bug"
	if got != want {
		t.Errorf("FormatMessage = %q, want %q", got, want)
	}
}

func TestCommit_AuthorAndCommitter(t *testing.T) {
	dir := initTestRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("some data"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := Stage(dir); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	opts := CommitOpts{
		Message:        "test author/committer",
		AuthorName:     "Dev User",
		AuthorEmail:    "dev@corp.com",
		CommitterName:  "CI Bot",
		CommitterEmail: "ci@corp.com",
	}
	_, err := Commit(dir, opts)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("commit object: %v", err)
	}

	if commit.Author.Name != opts.AuthorName {
		t.Errorf("author name = %q, want %q", commit.Author.Name, opts.AuthorName)
	}
	if commit.Author.Email != opts.AuthorEmail {
		t.Errorf("author email = %q, want %q", commit.Author.Email, opts.AuthorEmail)
	}
	if commit.Committer.Name != opts.CommitterName {
		t.Errorf("committer name = %q, want %q", commit.Committer.Name, opts.CommitterName)
	}
	if commit.Committer.Email != opts.CommitterEmail {
		t.Errorf("committer email = %q, want %q", commit.Committer.Email, opts.CommitterEmail)
	}
}
