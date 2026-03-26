package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// setupPushTest creates a bare remote and a clone with an initial commit.
// Returns (clonePath, barePath).
func setupPushTest(t *testing.T) (string, string) {
	t.Helper()

	// Create bare remote
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("bare init: %v", err)
	}

	// Create working clone
	cloneDir := filepath.Join(t.TempDir(), "clone")
	repo, err := git.PlainInit(cloneDir, false)
	if err != nil {
		t.Fatalf("clone init: %v", err)
	}

	// Add remote
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	})
	if err != nil {
		t.Fatalf("create remote: %v", err)
	}

	// Create initial commit
	wt, _ := repo.Worktree()
	os.WriteFile(filepath.Join(cloneDir, "README.md"), []byte("hello"), 0644)
	wt.Add("README.md")
	wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()},
	})

	// Push initial commit to set up remote tracking
	err = repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"refs/heads/master:refs/heads/master"},
	})
	if err != nil {
		t.Fatalf("initial push: %v", err)
	}

	// Set git identity for shell operations (rebase needs it)
	setGitConfig(t, cloneDir)

	return cloneDir, bareDir
}

func setGitConfig(t *testing.T, dir string) {
	t.Helper()
	for _, kv := range [][2]string{
		{"user.email", "t@t.com"},
		{"user.name", "test"},
	} {
		cmd := exec.Command("git", "-C", dir, "config", kv[0], kv[1])
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %s: %v", kv[0], out, err)
		}
	}
}

func TestPush_Success(t *testing.T) {
	cloneDir, bareDir := setupPushTest(t)

	// Create and commit a new file in clone
	repo, _ := git.PlainOpen(cloneDir)
	wt, _ := repo.Worktree()

	os.WriteFile(filepath.Join(cloneDir, "new.txt"), []byte("new content"), 0644)
	wt.Add("new.txt")
	wt.Commit("add new file", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()},
	})

	// Push should succeed
	if err := Push(cloneDir, "origin", "master"); err != nil {
		t.Fatalf("expected push to succeed: %v", err)
	}

	// Verify the bare remote received the commit
	bareRepo, _ := git.PlainOpen(bareDir)
	ref, err := bareRepo.Reference("refs/heads/master", true)
	if err != nil {
		t.Fatalf("read bare ref: %v", err)
	}
	commit, err := bareRepo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("read bare commit: %v", err)
	}
	if commit.Message != "add new file" {
		t.Errorf("expected bare HEAD message %q, got %q", "add new file", commit.Message)
	}
}

func TestPush_RebaseRetry(t *testing.T) {
	cloneDir, bareDir := setupPushTest(t)

	// Create a second clone via shell git
	clone2Dir := filepath.Join(t.TempDir(), "clone2")
	cmd := exec.Command("git", "clone", bareDir, clone2Dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone2: %s: %v", out, err)
	}

	// Configure git identity for clone2
	runGit(t, clone2Dir, "config", "user.email", "t@t.com")
	runGit(t, clone2Dir, "config", "user.name", "test")

	// In clone2: add a different file, commit, push
	os.WriteFile(filepath.Join(clone2Dir, "other.txt"), []byte("from clone2"), 0644)
	runGit(t, clone2Dir, "add", "other.txt")
	runGit(t, clone2Dir, "commit", "-m", "clone2 commit")
	runGit(t, clone2Dir, "push", "origin", "master")

	// In clone1: add yet another different file (no conflict)
	repo, _ := git.PlainOpen(cloneDir)
	wt, _ := repo.Worktree()

	os.WriteFile(filepath.Join(cloneDir, "feature.txt"), []byte("from clone1"), 0644)
	wt.Add("feature.txt")
	wt.Commit("clone1 commit", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()},
	})

	// Push from clone1 — should fail initially but succeed after rebase-retry
	if err := Push(cloneDir, "origin", "master"); err != nil {
		t.Fatalf("expected push with rebase-retry to succeed: %v", err)
	}

	// Verify bare remote has both commits
	bareRepo, _ := git.PlainOpen(bareDir)
	ref, _ := bareRepo.Reference("refs/heads/master", true)
	commit, _ := bareRepo.CommitObject(ref.Hash())
	if commit.Message != "clone1 commit" {
		t.Errorf("expected bare HEAD message %q, got %q", "clone1 commit", commit.Message)
	}
}

func TestPush_ConflictError(t *testing.T) {
	cloneDir, bareDir := setupPushTest(t)

	// Create a second clone via shell git
	clone2Dir := filepath.Join(t.TempDir(), "clone2")
	cmd := exec.Command("git", "clone", bareDir, clone2Dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone2: %s: %v", out, err)
	}

	// Configure git identity for clone2
	runGit(t, clone2Dir, "config", "user.email", "t@t.com")
	runGit(t, clone2Dir, "config", "user.name", "test")

	// In clone2: modify README.md with conflicting content, push
	os.WriteFile(filepath.Join(clone2Dir, "README.md"), []byte("clone2 version"), 0644)
	runGit(t, clone2Dir, "add", "README.md")
	runGit(t, clone2Dir, "commit", "-m", "clone2 conflict")
	runGit(t, clone2Dir, "push", "origin", "master")

	// In clone1: modify the same file with different content
	repo, _ := git.PlainOpen(cloneDir)
	wt, _ := repo.Worktree()

	os.WriteFile(filepath.Join(cloneDir, "README.md"), []byte("clone1 version"), 0644)
	wt.Add("README.md")
	wt.Commit("clone1 conflict", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "t@t.com", When: time.Now()},
	})

	// Push from clone1 — should fail with conflict/rebase error
	err := Push(cloneDir, "origin", "master")
	if err == nil {
		t.Fatal("expected push to fail due to conflict")
	}
	msg := err.Error()
	if !strings.Contains(msg, "conflict") && !strings.Contains(msg, "rebase failed") {
		t.Errorf("expected error to mention conflict or rebase failed, got: %s", msg)
	}
}

// runGit is a test helper that runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
}
