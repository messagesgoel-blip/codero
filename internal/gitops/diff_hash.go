package gitops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
)

// DiffHash returns a SHA-256 hex digest of the staged diff.
// Returns ("", nil) if there are no staged changes.
func DiffHash(ctx context.Context, worktreePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "diff", "--cached")
	cmd.Env = sanitizedGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff --cached: %w", err)
	}
	if len(out) == 0 {
		return "", nil
	}
	sum := sha256.Sum256(out)
	return hex.EncodeToString(sum[:]), nil
}

// HeadSHA returns the current HEAD commit SHA.
// Returns ("", nil) on an unborn HEAD (no commits yet).
func HeadSHA(ctx context.Context, worktreePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "rev-parse", "HEAD")
	cmd.Env = sanitizedGitEnv()
	out, err := cmd.Output()
	if err != nil {
		// Exit code 128 means unborn HEAD (no commits yet) — not a fatal error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 128 {
			return "", nil
		}
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasStagedChanges returns true if there are staged changes in the worktree.
func HasStagedChanges(ctx context.Context, worktreePath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "diff", "--cached", "--quiet")
	cmd.Env = sanitizedGitEnv()
	err := cmd.Run()
	if err != nil {
		// Exit code 1 means there are differences (staged changes exist).
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
		return false, fmt.Errorf("git diff --cached --quiet: %w", err)
	}
	// Exit code 0 means no differences (no staged changes).
	return false, nil
}
