package gitops

import (
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
)

// DiffHash returns a SHA-256 hex digest of the staged diff.
// Returns ("", nil) if there are no staged changes.
func DiffHash(worktreePath string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "diff", "--cached")
	cmd.Env = sanitizedGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", nil
	}
	sum := sha256.Sum256(out)
	return hex.EncodeToString(sum[:]), nil
}

// HeadSHA returns the current HEAD commit SHA.
func HeadSHA(worktreePath string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD")
	cmd.Env = sanitizedGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// HasStagedChanges returns true if there are staged changes in the worktree.
func HasStagedChanges(worktreePath string) (bool, error) {
	cmd := exec.Command("git", "-C", worktreePath, "diff", "--cached", "--quiet")
	cmd.Env = sanitizedGitEnv()
	err := cmd.Run()
	if err != nil {
		// Exit code 1 means there are differences (staged changes exist)
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
		return false, err
	}
	// Exit code 0 means no differences (no staged changes)
	return false, nil
}
