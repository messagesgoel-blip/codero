package gitops

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
)

// Push pushes the current branch to the remote. If the push fails with a
// non-fast-forward error, it attempts a shell `git rebase` and retries once.
// go-git lacks native rebase support, hence the shell fallback.
func Push(worktreePath, remote, branch string) error {
	repo, err := git.PlainOpen(worktreePath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))

	err = repo.Push(&git.PushOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{refSpec},
	})
	if err == nil {
		return nil
	}

	// Check if it's a non-fast-forward error
	if !isNonFastForward(err) {
		return fmt.Errorf("push: %w", err)
	}

	// Attempt rebase via shell (go-git doesn't support rebase)
	if err := shellRebase(worktreePath, remote, branch); err != nil {
		return fmt.Errorf("rebase failed: %w", err)
	}

	// Retry push after rebase
	err = repo.Push(&git.PushOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{refSpec},
	})
	if err != nil {
		return fmt.Errorf("push after rebase: non-fast-forward or conflict: %w", err)
	}
	return nil
}

// isNonFastForward checks if the error is a non-fast-forward rejection.
func isNonFastForward(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "non fast-forward") ||
		strings.Contains(msg, "fetch first") ||
		strings.Contains(msg, "failed to push")
}

// shellRebase runs `git fetch && git rebase` via shell since go-git lacks rebase.
func shellRebase(worktreePath, remote, branch string) error {
	fetch := exec.Command("git", "-C", worktreePath, "fetch", remote, branch)
	fetch.Env = sanitizedGitEnv()
	if out, err := fetch.CombinedOutput(); err != nil {
		return fmt.Errorf("fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}

	rebase := exec.Command("git", "-C", worktreePath, "rebase", remote+"/"+branch)
	rebase.Env = sanitizedGitEnv()
	if out, err := rebase.CombinedOutput(); err != nil {
		abort := exec.Command("git", "-C", worktreePath, "rebase", "--abort")
		abort.Env = sanitizedGitEnv()
		_ = abort.Run()
		return fmt.Errorf("conflict: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func sanitizedGitEnv() []string {
	deny := map[string]struct{}{
		"GIT_DIR":                          {},
		"GIT_WORK_TREE":                    {},
		"GIT_INDEX_FILE":                   {},
		"GIT_COMMON_DIR":                   {},
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": {},
		"GIT_OBJECT_DIRECTORY":             {},
	}
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key := strings.SplitN(kv, "=", 2)[0]
		if _, blocked := deny[key]; blocked {
			continue
		}
		env = append(env, kv)
	}
	return env
}
