package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	ghclient "github.com/codero/codero/internal/github"
	"github.com/codero/codero/internal/gitops"
	"github.com/codero/codero/internal/state"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// GitHubSubmitter abstracts GitHub operations for testing.
type GitHubSubmitter interface {
	FindOpenPR(ctx context.Context, repo, branch string) (*ghclient.PRInfo, error)
	CreatePR(ctx context.Context, repo, head, base, title, body string) (int, error)
	RequestReviewers(ctx context.Context, repo string, prNumber int, reviewers []string) error
}

// GitOps abstracts git operations for testing.
type GitOps interface {
	Commit(worktreePath string, opts gitops.CommitOpts) (string, error)
	Push(worktreePath, remote, branch string) error
	DiffHash(ctx context.Context, worktreePath string) (string, error)
	HeadSHA(ctx context.Context, worktreePath string) (string, error)
}

// realGitOps implements GitOps using real git operations.
type realGitOps struct{}

func (g realGitOps) Commit(worktreePath string, opts gitops.CommitOpts) (string, error) {
	return gitops.Commit(worktreePath, opts)
}

func (g realGitOps) Push(worktreePath, remote, branch string) error {
	return gitops.Push(worktreePath, remote, branch)
}

func (g realGitOps) DiffHash(ctx context.Context, worktreePath string) (string, error) {
	return gitops.DiffHash(ctx, worktreePath)
}

func (g realGitOps) HeadSHA(ctx context.Context, worktreePath string) (string, error) {
	return gitops.HeadSHA(ctx, worktreePath)
}

// submitCmd returns the "codero submit" command that performs the full git+PR flow:
// commit staged changes, push the branch, create/find PR, update state.
func submitCmd(configPath *string) *cobra.Command {
	var (
		worktree    string
		repo        string
		branch      string
		title       string
		body        string
		base        string
		reviewers   []string
		authorName  string
		authorEmail string
	)

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Commit, push, and create PR for the current branch",
		Long: `Submit commits all staged changes, pushes the branch to origin,
and creates a GitHub pull request (or reuses an existing one).

The PR number is recorded in the branch_states table, and the active
agent assignment's substatus is set to "submitted".

Requires GITHUB_TOKEN for PR operations. Without it, commit/push still
succeed but GitHub steps are skipped with a warning.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubmit(cmd.Context(), cmd, *configPath, submitOpts{
				worktree:    worktree,
				repo:        repo,
				branch:      branch,
				title:       title,
				body:        body,
				base:        base,
				reviewers:   reviewers,
				authorName:  authorName,
				authorEmail: authorEmail,
			})
		},
	}

	cwd, _ := os.Getwd()
	cmd.Flags().StringVar(&worktree, "worktree", cwd, "path to git worktree (defaults to $PWD)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository (owner/repo, e.g. messagesgoel-blip/codero)")
	cmd.Flags().StringVar(&branch, "branch", "", "branch name to push and create PR from")
	cmd.Flags().StringVar(&title, "title", "", "PR title")
	cmd.Flags().StringVar(&body, "body", "", "PR body (optional)")
	cmd.Flags().StringVar(&base, "base", "main", "base branch for PR")
	cmd.Flags().StringArrayVar(&reviewers, "reviewer", nil, "GitHub usernames to request review from (repeatable)")
	cmd.Flags().StringVar(&authorName, "author-name", "Codero Agent", "git commit author name")
	cmd.Flags().StringVar(&authorEmail, "author-email", "agent@codero.dev", "git commit author email")

	return cmd
}

type submitOpts struct {
	worktree    string
	repo        string
	branch      string
	title       string
	body        string
	base        string
	reviewers   []string
	authorName  string
	authorEmail string
	// ghClient allows injecting a mock for testing
	ghClient GitHubSubmitter
	// gitOps allows injecting a mock for testing
	gitOps GitOps
}

func runSubmit(ctx context.Context, cmd *cobra.Command, configPath string, opts submitOpts) error {
	// Validate required flags
	if opts.repo == "" {
		return usageErrorf("--repo is required")
	}
	if opts.branch == "" {
		return usageErrorf("--branch is required")
	}
	if opts.title == "" {
		return usageErrorf("--title is required")
	}

	// Open state DB
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("codero: config: %w", err)
	}
	db, err := state.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open state db: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Use injected gitOps or real implementation
	var git GitOps
	if opts.gitOps != nil {
		git = opts.gitOps
	} else {
		git = realGitOps{}
	}

	// Compute diff hash before commit — if empty, worktree is clean.
	diffHash, err := git.DiffHash(ctx, opts.worktree)
	if err != nil {
		return fmt.Errorf("compute diff hash: %w", err)
	}
	if diffHash == "" {
		return fmt.Errorf("no changes to submit: worktree is clean")
	}

	// Get HEAD SHA before the new commit.
	headSHA, err := git.HeadSHA(ctx, opts.worktree)
	if err != nil {
		return fmt.Errorf("get HEAD SHA: %w", err)
	}

	// Resolve assignment ID and session ID for dedup record.
	sessionID := resolveSessionIDFromEnv()
	var assignmentID string
	if sessionID != "" {
		row := db.Unwrap().QueryRowContext(ctx,
			`SELECT assignment_id FROM agent_assignments WHERE session_id = ? AND ended_at IS NULL LIMIT 1`,
			sessionID)
		if err := row.Scan(&assignmentID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			// Real DB error — warn but don't block the submission.
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to resolve assignment: %v\n", err)
		}
	}

	// Pre-check dedup before committing to avoid dangling records on commit failure.
	if assignmentID != "" {
		var existing int
		if err := db.Unwrap().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM submissions WHERE assignment_id = ? AND diff_hash = ? AND head_sha = ?`,
			assignmentID, diffHash, headSHA).Scan(&existing); err == nil && existing > 0 {
			return fmt.Errorf("duplicate submission: this exact diff has already been submitted — make new changes first")
		}
	}

	// Commit staged changes.
	commitMsg := gitops.FormatMessage("submit", 1, opts.title)
	sha, err := git.Commit(opts.worktree, gitops.CommitOpts{
		Message:        commitMsg,
		AuthorName:     opts.authorName,
		AuthorEmail:    opts.authorEmail,
		CommitterName:  opts.authorName,
		CommitterEmail: opts.authorEmail,
	})
	if err != nil {
		if strings.Contains(err.Error(), "nothing to commit") {
			return fmt.Errorf("no changes to submit: worktree is clean")
		}
		return fmt.Errorf("commit: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Committed: %s\n", sha[:8])

	// Record submission after a successful commit so retries work on commit failure.
	submissionID := uuid.New().String()
	rec := state.SubmissionRecord{
		SubmissionID: submissionID,
		AssignmentID: assignmentID,
		SessionID:    sessionID,
		Repo:         opts.repo,
		Branch:       opts.branch,
		HeadSHA:      headSHA,
		DiffHash:     diffHash,
		State:        "submitted",
	}
	if err := state.CreateSubmission(ctx, db, rec); err != nil && !errors.Is(err, state.ErrDuplicateSubmission) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to record submission: %v\n", err)
	}

	// Push to origin
	if err := git.Push(opts.worktree, "origin", opts.branch); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Pushed: %s\n", opts.branch)

	// GitHub operations (optional if no token)
	ghToken := os.Getenv("GITHUB_TOKEN")
	if ghToken == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Warning: GITHUB_TOKEN not set, skipping PR creation")
		return nil
	}

	// Use injected client or create real one
	var ghClient GitHubSubmitter
	if opts.ghClient != nil {
		ghClient = opts.ghClient
	} else {
		ghClient = ghclient.NewClient(ghToken)
	}

	// Find or create PR
	var prNumber int
	existingPR, err := ghClient.FindOpenPR(ctx, opts.repo, opts.branch)
	if err != nil {
		return fmt.Errorf("find open PR: %w", err)
	}
	if existingPR != nil {
		prNumber = existingPR.Number
		fmt.Fprintf(cmd.OutOrStdout(), "Found existing PR #%d\n", prNumber)
	} else {
		prNumber, err = ghClient.CreatePR(ctx, opts.repo, opts.branch, opts.base, opts.title, opts.body)
		if err != nil {
			return fmt.Errorf("create PR: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created PR #%d\n", prNumber)
	}

	// Request reviewers if specified
	if len(opts.reviewers) > 0 {
		if err := ghClient.RequestReviewers(ctx, opts.repo, prNumber, opts.reviewers); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to request reviewers: %v\n", err)
		}
	}

	// Record PR in branch_states
	if err := state.UpsertPRTracking(ctx, db, opts.repo, opts.branch, prNumber); err != nil {
		return fmt.Errorf("record PR: %w", err)
	}

	// Update assignment substatus to submitted (optional, skip gracefully if no session)
	// sessionID was resolved earlier in the function
	if sessionID != "" {
		res, err := db.Unwrap().ExecContext(ctx,
			`UPDATE agent_assignments SET substatus = 'submitted', updated_at = datetime('now')
			 WHERE session_id = ? AND ended_at IS NULL`,
			sessionID,
		)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to update assignment: %v\n", err)
		} else if n, _ := res.RowsAffected(); n == 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: no active assignment found for session %s\n", sessionID)
		}
	}

	prURL := fmt.Sprintf("https://github.com/%s/pull/%d", opts.repo, prNumber)
	fmt.Fprintf(cmd.OutOrStdout(), "Submitted: PR #%d — %s\n", prNumber, prURL)
	return nil
}
