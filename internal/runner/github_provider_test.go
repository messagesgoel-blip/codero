package runner

import (
	"context"
	"testing"

	ghclient "github.com/codero/codero/internal/github"
)

// stubReviewClient is a test double for githubReviewClient.
type stubReviewClient struct {
	pr       *ghclient.PRInfo
	comments []ghclient.ReviewComment
	reviews  []ghclient.Review
	prErr    error
}

func (s *stubReviewClient) FindOpenPR(_ context.Context, _, _ string) (*ghclient.PRInfo, error) {
	return s.pr, s.prErr
}
func (s *stubReviewClient) ListPRReviewComments(_ context.Context, _ string, _ int) ([]ghclient.ReviewComment, error) {
	return s.comments, nil
}
func (s *stubReviewClient) ListPRReviews(_ context.Context, _ string, _ int) ([]ghclient.Review, error) {
	return s.reviews, nil
}

func TestGitHubProvider_NoPR(t *testing.T) {
	p := &GitHubProvider{github: &stubReviewClient{pr: nil}}
	resp, err := p.Review(context.Background(), ReviewRequest{
		Repo: "owner/repo", Branch: "main",
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(resp.Findings) != 0 {
		t.Errorf("expected 0 findings for no PR, got %d", len(resp.Findings))
	}
}

func TestGitHubProvider_CodeRabbitCommentsOnly(t *testing.T) {
	stub := &stubReviewClient{
		pr: &ghclient.PRInfo{Number: 1, HeadSHA: "abc"},
		comments: []ghclient.ReviewComment{
			{ID: 1, User: "coderabbitai", Body: "Fix the error here.", Path: "main.go", Line: 10},
			{ID: 2, User: "human", Body: "Nice work!", Path: "util.go", Line: 5},
		},
		reviews: nil,
	}
	p := &GitHubProvider{github: stub}
	resp, err := p.Review(context.Background(), ReviewRequest{Repo: "owner/repo", Branch: "feat"})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("expected 1 finding (coderabbitai only), got %d", len(resp.Findings))
	}
	f := resp.Findings[0]
	if f.File != "main.go" {
		t.Errorf("file: want main.go, got %s", f.File)
	}
	if f.Line != 10 {
		t.Errorf("line: want 10, got %d", f.Line)
	}
}

func TestGitHubProvider_ReviewBodyIncluded(t *testing.T) {
	stub := &stubReviewClient{
		pr:       &ghclient.PRInfo{Number: 2, HeadSHA: "def"},
		comments: nil,
		reviews: []ghclient.Review{
			{User: "coderabbitai", State: "CHANGES_REQUESTED", Body: "Summary: critical security issue."},
			{User: "other", State: "APPROVED", Body: "Looks good."},
		},
	}
	p := &GitHubProvider{github: stub}
	resp, err := p.Review(context.Background(), ReviewRequest{Repo: "owner/repo", Branch: "feat"})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("expected 1 finding from coderabbit review body, got %d", len(resp.Findings))
	}
	if resp.Findings[0].Severity != "error" {
		t.Errorf("severity: want error (critical), got %s", resp.Findings[0].Severity)
	}
}

func TestInferSeverity(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{"This has a critical security vulnerability", "error"},
		{"There is a bug here", "error"},
		{"You should consider refactoring this", "warning"},
		{"Minor style improvement possible", "warning"},
		{"Looks fine to me", "info"},
	}
	for _, tc := range cases {
		got := inferSeverity(tc.body)
		if got != tc.want {
			t.Errorf("inferSeverity(%q): want %s, got %s", tc.body, tc.want, got)
		}
	}
}
