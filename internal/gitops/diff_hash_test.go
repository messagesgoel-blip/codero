package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDiffHash_StagedChanges(t *testing.T) {
	// Create temp dir for git repo
	dir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test User")

	// Create and commit an initial file
	if err := os.WriteFile(filepath.Join(dir, "initial.txt"), []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "initial.txt")
	runGitCmd(t, dir, "commit", "-m", "initial commit")

	// Create a new file and stage it
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "test.txt")

	// DiffHash should return non-empty hash
	hash, err := DiffHash(dir)
	if err != nil {
		t.Fatalf("DiffHash failed: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty hash for staged changes")
	}
	// SHA-256 produces 64 hex characters
	if len(hash) != 64 {
		t.Errorf("expected 64 char hash, got %d chars: %s", len(hash), hash)
	}
}

func TestDiffHash_Clean(t *testing.T) {
	// Create temp dir for git repo
	dir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test User")

	// Create and commit a file
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "test.txt")
	runGitCmd(t, dir, "commit", "-m", "initial commit")

	// No staged changes
	hash, err := DiffHash(dir)
	if err != nil {
		t.Fatalf("DiffHash failed: %v", err)
	}
	if hash != "" {
		t.Errorf("expected empty string for clean worktree, got %q", hash)
	}
}

func TestDiffHash_Deterministic(t *testing.T) {
	// Create temp dir for git repo
	dir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test User")

	// Create and commit initial file
	if err := os.WriteFile(filepath.Join(dir, "initial.txt"), []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "initial.txt")
	runGitCmd(t, dir, "commit", "-m", "initial commit")

	// Stage a change
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "test.txt")

	// Multiple calls should return the same hash
	hash1, err := DiffHash(dir)
	if err != nil {
		t.Fatalf("DiffHash 1 failed: %v", err)
	}
	hash2, err := DiffHash(dir)
	if err != nil {
		t.Fatalf("DiffHash 2 failed: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("DiffHash not deterministic: %s != %s", hash1, hash2)
	}
}

func TestHeadSHA(t *testing.T) {
	// Create temp dir for git repo
	dir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test User")

	// Create and commit a file
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "test.txt")
	runGitCmd(t, dir, "commit", "-m", "initial commit")

	sha, err := HeadSHA(dir)
	if err != nil {
		t.Fatalf("HeadSHA failed: %v", err)
	}
	// SHA-1 produces 40 hex characters
	if len(sha) != 40 {
		t.Errorf("expected 40 char SHA, got %d chars: %s", len(sha), sha)
	}
}

func TestHasStagedChanges(t *testing.T) {
	dir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test User")

	// Create and commit initial file
	if err := os.WriteFile(filepath.Join(dir, "initial.txt"), []byte("initial"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "initial.txt")
	runGitCmd(t, dir, "commit", "-m", "initial commit")

	// No staged changes
	has, err := HasStagedChanges(dir)
	if err != nil {
		t.Fatalf("HasStagedChanges failed: %v", err)
	}
	if has {
		t.Error("expected no staged changes")
	}

	// Stage a new file
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, dir, "add", "new.txt")

	has, err = HasStagedChanges(dir)
	if err != nil {
		t.Fatalf("HasStagedChanges failed: %v", err)
	}
	if !has {
		t.Error("expected staged changes")
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\noutput: %s", args, err, out)
	}
}
