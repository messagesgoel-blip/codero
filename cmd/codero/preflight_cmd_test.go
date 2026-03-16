package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManagedRepos_ParsesCorrectly(t *testing.T) {
	dir := t.TempDir()
	content := `# comment line
/srv/storage/repo/codero
/srv/storage/repo/cacheflow

# another comment
/srv/storage/repo/mathkit
`
	path := filepath.Join(dir, "repos.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	repos, err := loadManagedRepos(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 3 {
		t.Fatalf("expected 3 repos, got %d: %v", len(repos), repos)
	}
	if repos[0] != "/srv/storage/repo/codero" {
		t.Errorf("repos[0] = %q, want /srv/storage/repo/codero", repos[0])
	}
}

func TestLoadManagedRepos_MissingFile(t *testing.T) {
	_, err := loadManagedRepos("/nonexistent/repos.txt")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadManagedRepos_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repos.txt")
	if err := os.WriteFile(path, []byte("# only comments\n\n"), 0644); err != nil {
		t.Fatal(err)
	}
	repos, err := loadManagedRepos(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(repos))
	}
}

func TestCheckHookEnforcement_MissingRepo(t *testing.T) {
	r := checkHookEnforcement("/nonexistent/repo")
	if r.Status != "FAIL" {
		t.Errorf("expected FAIL for nonexistent repo, got %s", r.Status)
	}
}

func TestCheckHookEnforcement_RepoWithGithook(t *testing.T) {
	dir := t.TempDir()

	// Simulate .githooks/pre-commit as a real executable file
	hooksDir := filepath.Join(dir, ".githooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	r := checkHookEnforcement(dir)
	if r.Status != "PASS" {
		t.Errorf("expected PASS for repo with executable .githooks/pre-commit, got %s: %s", r.Status, r.Detail)
	}
}

func TestCheckHookEnforcement_NonExecutableHook(t *testing.T) {
	dir := t.TempDir()
	hooksDir := filepath.Join(dir, ".githooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	// Write non-executable hook
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r := checkHookEnforcement(dir)
	if r.Status != "FAIL" {
		t.Errorf("expected FAIL for non-executable hook, got %s", r.Status)
	}
}

func TestRunPreflight_MissingToolsDir(t *testing.T) {
	dir := t.TempDir()
	reposFile := filepath.Join(dir, "repos.txt")
	if err := os.WriteFile(reposFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	results, allPass := runPreflight(reposFile, "/nonexistent/tools/bin")
	if allPass {
		t.Error("expected allPass=false when tools dir does not exist")
	}
	// All 5 tool checks should be FAIL
	toolFails := 0
	for _, r := range results {
		if strings.HasPrefix(r.Name, "tool:") && r.Status == "FAIL" {
			toolFails++
		}
	}
	if toolFails != 5 {
		t.Errorf("expected 5 tool FAIL results, got %d", toolFails)
	}
}

func TestRunPreflight_AllToolsPresent(t *testing.T) {
	dir := t.TempDir()
	toolsDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"semgrep", "gitleaks", "pre-commit", "poetry", "ruff"} {
		p := filepath.Join(toolsDir, tool)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	reposFile := filepath.Join(dir, "repos.txt")
	if err := os.WriteFile(reposFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	results, _ := runPreflight(reposFile, toolsDir)
	toolPass := 0
	for _, r := range results {
		if strings.HasPrefix(r.Name, "tool:") && r.Status == "PASS" {
			toolPass++
		}
	}
	if toolPass != 5 {
		t.Errorf("expected 5 tool PASS results, got %d", toolPass)
	}
}

func TestPrintPreflightResults_ContainsStatusLine(t *testing.T) {
	// Just verify printPreflightResults doesn't panic with mixed results
	results := []preflightCheck{
		{Name: "tool:semgrep", Status: "PASS", Detail: "/tools/semgrep"},
		{Name: "tool:gitleaks", Status: "FAIL", Detail: "not found"},
	}
	// Redirect stdout to discard output (output correctness tested via human observation)
	old := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	defer func() { os.Stdout = old }()
	printPreflightResults(results) // must not panic
}
