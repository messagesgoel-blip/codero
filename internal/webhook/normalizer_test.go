package webhook

import (
	"strings"
	"testing"
)

func TestNormalizeReviewFindings(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		stateStr     string
		source       string
		repo         string
		branch       string
		wantCount    int
		wantSeverity string
		wantFile     string
		wantLine     int
	}{
		{
			name: "empty body", body: "", stateStr: "changes_requested",
			source: "coderabbitai[bot]", repo: "owner/repo", branch: "feat",
			wantCount: 0,
		},
		{
			name: "single structured finding", body: "main.go:10: unused variable x",
			stateStr: "changes_requested", source: "coderabbitai[bot]",
			repo: "owner/repo", branch: "feat",
			wantCount: 1, wantSeverity: "error", wantFile: "main.go", wantLine: 10,
		},
		{
			name: "multiple structured findings", body: "main.go:10: unused variable x\nconfig.go:5: missing doc comment\n",
			stateStr: "approved", source: "reviewer",
			repo: "owner/repo", branch: "feat",
			wantCount: 2, wantSeverity: "info",
		},
		{
			name: "dash-prefixed finding", body: "- utils.go line 42 potential nil dereference",
			stateStr: "changes_requested", source: "coderabbitai[bot]",
			repo: "owner/repo", branch: "feat",
			wantCount: 1, wantSeverity: "error", wantFile: "utils.go", wantLine: 42,
		},
		{
			name:     "fallback single finding for unstructured body",
			body:     "This PR needs better error handling throughout",
			stateStr: "changes_requested", source: "reviewer",
			repo: "owner/repo", branch: "feat",
			wantCount: 1, wantSeverity: "error",
		},
		{
			name:     "severity approved maps to info",
			body:     "Looks good overall. main.go:1: minor nit",
			stateStr: "approved", source: "reviewer",
			repo: "owner/repo", branch: "feat",
			wantCount: 1, wantSeverity: "info",
		},
		{
			name:     "severity commented maps to warning",
			body:     "main.go:15: consider refactoring",
			stateStr: "commented", source: "reviewer",
			repo: "owner/repo", branch: "feat",
			wantCount: 1, wantSeverity: "warning",
		},
		{
			name:     "long body truncated in fallback",
			body:     strings.Repeat("a", 600),
			stateStr: "changes_requested", source: "reviewer",
			repo: "owner/repo", branch: "feat",
			wantCount: 1, wantSeverity: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := normalizeReviewFindings(tt.repo, tt.branch, tt.body, tt.stateStr, tt.source, "run-1")
			if len(findings) != tt.wantCount {
				t.Fatalf("count: got %d, want %d", len(findings), tt.wantCount)
			}
			if tt.wantCount == 0 {
				return
			}
			f := findings[0]
			if tt.wantSeverity != "" && f.Severity != tt.wantSeverity {
				t.Errorf("severity: got %q, want %q", f.Severity, tt.wantSeverity)
			}
			if tt.wantFile != "" && f.File != tt.wantFile {
				t.Errorf("file: got %q, want %q", f.File, tt.wantFile)
			}
			if tt.wantLine != 0 && f.Line != tt.wantLine {
				t.Errorf("line: got %d, want %d", f.Line, tt.wantLine)
			}
			if f.Repo != tt.repo {
				t.Errorf("repo: got %q, want %q", f.Repo, tt.repo)
			}
			if f.Branch != tt.branch {
				t.Errorf("branch: got %q, want %q", f.Branch, tt.branch)
			}
			if f.Source != tt.source {
				t.Errorf("source: got %q, want %q", f.Source, tt.source)
			}
			if tt.name == "long body truncated in fallback" && len(f.Message) > 500 {
				t.Errorf("message not truncated: len=%d", len(f.Message))
			}
		})
	}
}

func TestReviewSeverity(t *testing.T) {
	tests := []struct {
		stateStr string
		want     string
	}{
		{"changes_requested", "error"},
		{"approved", "info"},
		{"commented", "warning"},
		{"dismissed", "warning"},
		{"", "warning"},
	}
	for _, tt := range tests {
		t.Run(tt.stateStr, func(t *testing.T) {
			got := reviewSeverity(tt.stateStr)
			if got != tt.want {
				t.Errorf("reviewSeverity(%q) = %q, want %q", tt.stateStr, got, tt.want)
			}
		})
	}
}
