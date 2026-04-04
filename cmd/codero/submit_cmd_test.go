package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ghclient "github.com/codero/codero/internal/github"
	"github.com/codero/codero/internal/gitops"
	"github.com/codero/codero/internal/state"
	"github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"
)

// mockGitHubClient implements GitHubSubmitter for testing.
type mockGitHubClient struct {
	findOpenPRFunc       func(ctx context.Context, repo, branch string) (*ghclient.PRInfo, error)
	createPRFunc         func(ctx context.Context, repo, head, base, title, body string) (int, error)
	requestReviewersFunc func(ctx context.Context, repo string, prNumber int, reviewers []string) error
	createPRCalls        int
}

func (m *mockGitHubClient) FindOpenPR(ctx context.Context, repo, branch string) (*ghclient.PRInfo, error) {
	if m.findOpenPRFunc != nil {
		return m.findOpenPRFunc(ctx, repo, branch)
	}
	return nil, nil
}

func (m *mockGitHubClient) CreatePR(ctx context.Context, repo, head, base, title, body string) (int, error) {
	m.createPRCalls++
	if m.createPRFunc != nil {
		return m.createPRFunc(ctx, repo, head, base, title, body)
	}
	return 42, nil
}

func (m *mockGitHubClient) RequestReviewers(ctx context.Context, repo string, prNumber int, reviewers []string) error {
	if m.requestReviewersFunc != nil {
		return m.requestReviewersFunc(ctx, repo, prNumber, reviewers)
	}
	return nil
}

// mockGitOps implements GitOps for testing.
type mockGitOps struct {
	commitFunc func(worktreePath string, opts gitops.CommitOpts) (string, error)
	pushFunc   func(worktreePath, remote, branch string) error
}

func (m *mockGitOps) Commit(worktreePath string, opts gitops.CommitOpts) (string, error) {
	if m.commitFunc != nil {
		return m.commitFunc(worktreePath, opts)
	}
	return "abc12345", nil
}

func (m *mockGitOps) Push(worktreePath, remote, branch string) error {
	if m.pushFunc != nil {
		return m.pushFunc(worktreePath, remote, branch)
	}
	return nil
}

// setupTestDBAndConfig creates a temp config file and state DB.
func setupTestDBAndConfig(t *testing.T) (string, *state.DB) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "codero.db")
	configPath := filepath.Join(tmpDir, "codero.yaml")

	// Create minimal config with required fields
	configYAML := `github_token: ghp_test_token
repos:
  - test/repo
db_path: ` + dbPath + `
api_server:
  addr: ":8080"
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Open DB (auto-creates and migrates)
	db, err := state.Open(dbPath)
	if err != nil {
		t.Fatalf("open state db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return configPath, db
}

func TestSubmitCmd_CreatesPR(t *testing.T) {
	configPath, _ := setupTestDBAndConfig(t)

	// Set GITHUB_TOKEN to enable GitHub operations
	t.Setenv("GITHUB_TOKEN", "test-token")

	ghMock := &mockGitHubClient{
		findOpenPRFunc: func(ctx context.Context, repo, branch string) (*ghclient.PRInfo, error) {
			return nil, nil // No existing PR
		},
		createPRFunc: func(ctx context.Context, repo, head, base, title, body string) (int, error) {
			if repo != "test/repo" {
				t.Errorf("expected repo test/repo, got %s", repo)
			}
			if head != "feat/test" {
				t.Errorf("expected head feat/test, got %s", head)
			}
			if title != "Test PR" {
				t.Errorf("expected title 'Test PR', got %s", title)
			}
			return 123, nil
		},
	}

	gitMock := &mockGitOps{
		commitFunc: func(worktreePath string, opts gitops.CommitOpts) (string, error) {
			return "abc12345678901234567890123456789012345678", nil
		},
		pushFunc: func(worktreePath, remote, branch string) error {
			return nil
		},
	}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runSubmit(context.Background(), cmd, configPath, submitOpts{
		worktree:    t.TempDir(),
		repo:        "test/repo",
		branch:      "feat/test",
		title:       "Test PR",
		body:        "Test body",
		base:        "main",
		authorName:  "Test",
		authorEmail: "test@test.com",
		ghClient:    ghMock,
		gitOps:      gitMock,
	})

	if err != nil {
		t.Fatalf("runSubmit failed: %v", err)
	}

	if ghMock.createPRCalls != 1 {
		t.Errorf("expected 1 CreatePR call, got %d", ghMock.createPRCalls)
	}

	output := out.String()
	if !strings.Contains(output, "PR #123") {
		t.Errorf("expected output to contain 'PR #123', got:\n%s", output)
	}
}

func TestSubmitCmd_CleanWorktreeError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath, _ := setupTestDBAndConfig(t)

	// Initialize an empty git repo (no staged changes)
	_, err := git.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Mock gitOps that returns "nothing to commit"
	gitMock := &mockGitOps{
		commitFunc: func(worktreePath string, opts gitops.CommitOpts) (string, error) {
			return "", fmt.Errorf("nothing to commit, working tree clean")
		},
	}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err = runSubmit(context.Background(), cmd, configPath, submitOpts{
		worktree:    tmpDir,
		repo:        "test/repo",
		branch:      "feat/test",
		title:       "Test PR",
		authorName:  "Test",
		authorEmail: "test@test.com",
		gitOps:      gitMock,
	})

	if err == nil {
		t.Fatal("expected error for clean worktree")
	}
	if !strings.Contains(err.Error(), "no changes to submit") {
		t.Errorf("expected 'no changes to submit' error, got: %s", err.Error())
	}
}

func TestSubmitCmd_ReusesExistingPR(t *testing.T) {
	configPath, _ := setupTestDBAndConfig(t)

	t.Setenv("GITHUB_TOKEN", "test-token")

	ghMock := &mockGitHubClient{
		findOpenPRFunc: func(ctx context.Context, repo, branch string) (*ghclient.PRInfo, error) {
			return &ghclient.PRInfo{
				Number:  456,
				HeadSHA: "abc123",
				State:   "open",
			}, nil
		},
	}

	gitMock := &mockGitOps{
		commitFunc: func(worktreePath string, opts gitops.CommitOpts) (string, error) {
			return "abc12345678901234567890123456789012345678", nil
		},
		pushFunc: func(worktreePath, remote, branch string) error {
			return nil
		},
	}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runSubmit(context.Background(), cmd, configPath, submitOpts{
		worktree:    t.TempDir(),
		repo:        "test/repo",
		branch:      "feat/test",
		title:       "Test PR",
		authorName:  "Test",
		authorEmail: "test@test.com",
		ghClient:    ghMock,
		gitOps:      gitMock,
	})

	if err != nil {
		t.Fatalf("runSubmit failed: %v", err)
	}

	// Should NOT have called CreatePR since PR exists
	if ghMock.createPRCalls != 0 {
		t.Errorf("expected 0 CreatePR calls (PR exists), got %d", ghMock.createPRCalls)
	}

	output := out.String()
	if !strings.Contains(output, "Found existing PR #456") {
		t.Errorf("expected output to mention existing PR #456, got:\n%s", output)
	}
}

func TestSubmitCmd_RequiredFlags(t *testing.T) {
	configPath, _ := setupTestDBAndConfig(t)
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	tests := []struct {
		name      string
		opts      submitOpts
		wantError string
	}{
		{
			name:      "missing repo",
			opts:      submitOpts{branch: "feat/test", title: "Test"},
			wantError: "--repo is required",
		},
		{
			name:      "missing branch",
			opts:      submitOpts{repo: "test/repo", title: "Test"},
			wantError: "--branch is required",
		},
		{
			name:      "missing title",
			opts:      submitOpts{repo: "test/repo", branch: "feat/test"},
			wantError: "--title is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runSubmit(context.Background(), cmd, configPath, tc.opts)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("expected error containing %q, got: %s", tc.wantError, err.Error())
			}
		})
	}
}

func TestSubmitCmd_NoGitHubTokenWarning(t *testing.T) {
	configPath, _ := setupTestDBAndConfig(t)

	// Ensure no GITHUB_TOKEN
	t.Setenv("GITHUB_TOKEN", "")

	gitMock := &mockGitOps{
		commitFunc: func(worktreePath string, opts gitops.CommitOpts) (string, error) {
			return "abc12345678901234567890123456789012345678", nil
		},
		pushFunc: func(worktreePath, remote, branch string) error {
			return nil
		},
	}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	err := runSubmit(context.Background(), cmd, configPath, submitOpts{
		worktree:    t.TempDir(),
		repo:        "test/repo",
		branch:      "feat/test",
		title:       "Test PR",
		authorName:  "Test",
		authorEmail: "test@test.com",
		gitOps:      gitMock,
	})

	if err != nil {
		t.Fatalf("runSubmit failed: %v", err)
	}

	errOutput := errOut.String()
	if !strings.Contains(errOutput, "GITHUB_TOKEN not set") {
		t.Errorf("expected warning about GITHUB_TOKEN, got stderr:\n%s", errOutput)
	}
}

func TestSubmitCmd_RecordsPRInState(t *testing.T) {
	configPath, _ := setupTestDBAndConfig(t)

	t.Setenv("GITHUB_TOKEN", "test-token")

	ghMock := &mockGitHubClient{
		createPRFunc: func(ctx context.Context, repo, head, base, title, body string) (int, error) {
			return 789, nil
		},
	}

	gitMock := &mockGitOps{}

	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runSubmit(context.Background(), cmd, configPath, submitOpts{
		worktree:    t.TempDir(),
		repo:        "test/repo",
		branch:      "feat/recorded",
		title:       "Test PR",
		authorName:  "Test",
		authorEmail: "test@test.com",
		ghClient:    ghMock,
		gitOps:      gitMock,
	})

	if err != nil {
		t.Fatalf("runSubmit failed: %v", err)
	}

	// Re-open DB to verify PR was recorded (runSubmit closes its own handle)
	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	db, err := state.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("state.Open failed: %v", err)
	}
	defer db.Close()

	// Verify PR was recorded in branch_states
	prNumber, err := state.GetPRNumber(context.Background(), db, "test/repo", "feat/recorded")
	if err != nil {
		t.Fatalf("GetPRNumber failed: %v", err)
	}
	if prNumber != 789 {
		t.Errorf("expected PR #789 in state, got %d", prNumber)
	}
}
