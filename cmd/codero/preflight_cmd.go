package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// preflightCheck represents the result of a single preflight validation.
type preflightCheck struct {
	Name   string
	Status string // "PASS" | "FAIL"
	Detail string
}

// preflightCmd validates shared tooling, heartbeat binary, and hook enforcement.
func preflightCmd() *cobra.Command {
	var (
		reposFile string
		toolsDir  string
		quiet     bool
	)

	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Validate shared tooling, heartbeat binary, and hook enforcement",
		Long: `Run dependency and enforcement baseline checks before daily proving collection.

Checks:
  1. Shared tools reachable and executable (semgrep, gitleaks, pre-commit, poetry, ruff)
  2. Shared gate-heartbeat contract binary present and executable
  3. Pre-commit hook installed on every repo in the managed repos list

Exit code 0 if all checks pass. Non-zero if any check fails.

Examples:
  codero preflight
  codero preflight --repos-file docs/managed-repos.txt
  codero preflight --quiet   # exit code only, no output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			results, allPass := runPreflight(reposFile, toolsDir)
			if !quiet {
				printPreflightResults(results)
			}
			if !allPass {
				return fmt.Errorf("preflight: one or more checks FAILED")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&reposFile, "repos-file", "docs/managed-repos.txt",
		"path to managed repos list file")
	cmd.Flags().StringVar(&toolsDir, "tools-dir", "/srv/storage/shared/tools/bin",
		"path to shared tools directory")
	cmd.Flags().BoolVar(&quiet, "quiet", false,
		"suppress per-check output (exit code still reflects overall status)")

	return cmd
}

// runPreflight executes all baseline checks and returns results plus an overall pass flag.
// It is exported-by-convention (lowercase) and used by the daily-snapshot command.
func runPreflight(reposFile, toolsDir string) ([]preflightCheck, bool) {
	var results []preflightCheck
	allPass := true

	add := func(r preflightCheck) {
		results = append(results, r)
		if r.Status == "FAIL" {
			allPass = false
		}
	}

	// 1. Required shared tools
	for _, tool := range []string{"semgrep", "gitleaks", "pre-commit", "poetry", "ruff"} {
		p := filepath.Join(toolsDir, tool)
		info, err := os.Stat(p)
		if err != nil {
			add(preflightCheck{Name: "tool:" + tool, Status: "FAIL", Detail: "not found: " + p})
			continue
		}
		if info.Mode()&0o111 == 0 {
			add(preflightCheck{Name: "tool:" + tool, Status: "FAIL", Detail: "not executable: " + p})
			continue
		}
		add(preflightCheck{Name: "tool:" + tool, Status: "PASS", Detail: p})
	}

	// 2. Gate-heartbeat binary
	const heartbeatBin = "/srv/storage/shared/agent-toolkit/bin/gate-heartbeat"
	if info, err := os.Stat(heartbeatBin); err != nil || info.Mode()&0o111 == 0 {
		add(preflightCheck{Name: "gate-heartbeat", Status: "FAIL",
			Detail: "not found or not executable: " + heartbeatBin})
	} else {
		add(preflightCheck{Name: "gate-heartbeat", Status: "PASS", Detail: heartbeatBin})
	}

	// 3. Hook enforcement on managed repos
	repos, err := loadManagedRepos(reposFile)
	if err != nil {
		add(preflightCheck{Name: "repos-file", Status: "FAIL", Detail: err.Error()})
		return results, false
	}
	add(preflightCheck{Name: "repos-file", Status: "PASS",
		Detail: fmt.Sprintf("%d repos in %s", len(repos), reposFile)})

	for _, repo := range repos {
		add(checkHookEnforcement(repo))
	}

	return results, allPass
}

// checkHookEnforcement verifies that a pre-commit hook is installed and executable
// for the given repository path. It checks both .githooks/ (symlink approach) and
// the canonical .git/hooks/ location.
func checkHookEnforcement(repoPath string) preflightCheck {
	label := "hook:" + filepath.Base(repoPath)

	// Prefer .githooks/pre-commit (shared toolkit symlink approach)
	githookPath := filepath.Join(repoPath, ".githooks", "pre-commit")
	if lstat, err := os.Lstat(githookPath); err == nil {
		if isExecutable(githookPath, lstat) {
			return preflightCheck{Name: label, Status: "PASS", Detail: githookPath}
		}
		return preflightCheck{Name: label, Status: "FAIL",
			Detail: "hook exists but is not executable (check symlink target): " + githookPath}
	}

	// Fallback: .git/hooks/pre-commit via git rev-parse
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--git-common-dir").Output() //nolint:gosec
	if err != nil {
		return preflightCheck{Name: label, Status: "FAIL",
			Detail: "not a git repository: " + repoPath}
	}
	hookPath := filepath.Join(strings.TrimSpace(string(out)), "hooks", "pre-commit")
	if info, err := os.Stat(hookPath); err != nil || info.Mode()&0o111 == 0 {
		return preflightCheck{Name: label, Status: "FAIL",
			Detail: "pre-commit hook missing or not executable at " + hookPath +
				" — reinstall with: scripts/review/install-pre-commit.sh " + repoPath}
	}
	return preflightCheck{Name: label, Status: "PASS", Detail: hookPath}
}

// isExecutable returns true if the file (or symlink target) has any execute bit set.
func isExecutable(path string, lstat os.FileInfo) bool {
	if lstat.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false
		}
		info, err := os.Stat(target)
		if err != nil {
			return false
		}
		return info.Mode()&0o111 != 0
	}
	return lstat.Mode()&0o111 != 0
}

// loadManagedRepos parses a repos list file, stripping comments and blank lines.
func loadManagedRepos(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open repos file %q: %w", path, err)
	}
	defer f.Close()

	var repos []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line != "" {
			repos = append(repos, line)
		}
	}
	return repos, s.Err()
}

// printPreflightResults prints the check results and a final STATUS line to stdout.
func printPreflightResults(results []preflightCheck) {
	passed, failed := 0, 0
	for _, r := range results {
		if r.Status == "PASS" {
			fmt.Printf("  ✓ PASS  %s\n", r.Name)
			passed++
		} else {
			fmt.Printf("  ✗ FAIL  %s\n", r.Name)
			fmt.Printf("         → %s\n", r.Detail)
			failed++
		}
	}
	fmt.Printf("\nPreflight: %d passed, %d failed\n", passed, failed)
	if failed == 0 {
		fmt.Println("STATUS: PASS")
	} else {
		fmt.Println("STATUS: FAIL")
	}
}
