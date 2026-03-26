package gitops

import (
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Stage adds all changes in the worktree (equivalent to `git add -A`).
func Stage(worktreePath string) error {
	repo, err := git.PlainOpen(worktreePath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("stage all: %w", err)
	}
	return nil
}

// CommitOpts holds parameters for a commit.
type CommitOpts struct {
	Message        string
	AuthorName     string
	AuthorEmail    string
	CommitterName  string
	CommitterEmail string
}

// Commit creates a commit with the staged changes and returns the commit SHA.
// Returns an error if there are no staged changes.
func Commit(worktreePath string, opts CommitOpts) (string, error) {
	repo, err := git.PlainOpen(worktreePath)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("get worktree: %w", err)
	}

	// Check if there are staged changes
	status, err := wt.Status()
	if err != nil {
		return "", fmt.Errorf("get status: %w", err)
	}
	if status.IsClean() {
		return "", fmt.Errorf("nothing to commit")
	}

	now := time.Now()
	hash, err := wt.Commit(opts.Message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  opts.AuthorName,
			Email: opts.AuthorEmail,
			When:  now,
		},
		Committer: &object.Signature{
			Name:  opts.CommitterName,
			Email: opts.CommitterEmail,
			When:  now,
		},
	})
	if err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return hash.String(), nil
}

// FormatMessage builds the standard codero commit message format.
func FormatMessage(taskID string, version int, summary string) string {
	return fmt.Sprintf("[codero] %s v%d: %s", taskID, version, summary)
}
