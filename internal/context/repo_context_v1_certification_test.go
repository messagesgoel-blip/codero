package context_test

// Repo-Context v1 Certification Tests
//
// Maps directly to codero_certification_matrix_v1.md §11 acceptance criteria.
// Each test name includes the clause ID for traceability.
//
// Existing tests in context_test.go, queries_test.go, and store_test.go
// provide primary coverage; these certification tests add explicit
// clause-mapped evidence for gaps and consolidate the acceptance surface.

import (
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	repocontext "github.com/codero/codero/internal/context"
)

// ---------------------------------------------------------------------------
// F-01 — Repository-local index: graph.db is per-repo, not global
// ---------------------------------------------------------------------------

func TestCert_RCv1_F01_RepoLocalIndex(t *testing.T) {
	store := openCertStore(t)
	defer store.Close()

	if !strings.Contains(store.Path(), ".codero/context/graph.db") {
		t.Errorf("store path %q does not contain .codero/context/graph.db", store.Path())
	}
	if _, err := os.Stat(store.Path()); err != nil {
		t.Fatalf("graph.db not created: %v", err)
	}
}

// ---------------------------------------------------------------------------
// F-02 — Rebuildability: delete + rebuild does not affect branch state
// ---------------------------------------------------------------------------

func TestCert_RCv1_F02_Rebuildability(t *testing.T) {
	repoRoot := initGitRepo(t)
	writeGoFile(t, repoRoot, "main.go", "package main\nfunc Hello() {}\n")
	gitAdd(t, repoRoot, ".")

	dbPath := repocontext.DBPath(repoRoot)
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Index once.
	res1, err := repocontext.Index(store, repoRoot, repocontext.IndexOptions{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	// Delete the DB.
	os.Remove(dbPath)
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("DB should be deleted")
	}

	// Rebuild.
	store2, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	res2, err := repocontext.Index(store2, repoRoot, repocontext.IndexOptions{Full: true})
	if err != nil {
		t.Fatal(err)
	}

	if res2.Symbols != res1.Symbols {
		t.Errorf("rebuild symbol count: got %d, want %d", res2.Symbols, res1.Symbols)
	}
}

// ---------------------------------------------------------------------------
// F-03 — Full and incremental refresh: both produce valid index
// ---------------------------------------------------------------------------

func TestCert_RCv1_F03_IncrementalRefresh(t *testing.T) {
	repoRoot := initGitRepo(t)
	writeGoFile(t, repoRoot, "a.go", "package main\nfunc A() {}\n")
	gitAdd(t, repoRoot, ".")
	gitCommit(t, repoRoot, "initial")

	dbPath := repocontext.DBPath(repoRoot)
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Full index.
	res1, err := repocontext.Index(store, repoRoot, repocontext.IndexOptions{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if res1.Symbols == 0 {
		t.Fatal("full index found no symbols")
	}

	// Add a new file and commit.
	writeGoFile(t, repoRoot, "b.go", "package main\nfunc B() {}\n")
	gitAdd(t, repoRoot, ".")
	gitCommit(t, repoRoot, "add b.go")

	// Incremental (Full: false).
	res2, err := repocontext.Index(store, repoRoot, repocontext.IndexOptions{Full: false})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Symbols < res1.Symbols {
		t.Errorf("incremental: symbol count decreased from %d to %d", res1.Symbols, res2.Symbols)
	}
}

func TestCert_RCv1_GitHelpersIgnoreInheritedGitEnv(t *testing.T) {
	otherRepo := initGitRepo(t)
	writeGoFile(t, otherRepo, "other.go", "package main\nfunc Other() {}\n")
	gitAdd(t, otherRepo, ".")
	gitCommit(t, otherRepo, "initial")
	otherHead := gitHeadForTest(t, otherRepo)
	if otherHead == "" {
		t.Fatal("other repo head should not be empty")
	}

	repoRoot := initGitRepo(t)
	writeGoFile(t, repoRoot, "a.go", "package main\nfunc A() {}\n")
	gitAdd(t, repoRoot, ".")
	gitCommit(t, repoRoot, "initial")
	initialHead := gitHeadForTest(t, repoRoot)
	if initialHead == "" {
		t.Fatal("repo head should not be empty")
	}

	t.Setenv("GIT_DIR", filepath.Join(otherRepo, ".git"))
	t.Setenv("GIT_WORK_TREE", otherRepo)

	writeGoFile(t, repoRoot, "b.go", "package main\nfunc B() {}\n")
	gitAdd(t, repoRoot, ".")
	gitCommit(t, repoRoot, "add b.go")

	if got := gitHeadForTest(t, repoRoot); got == "" || got == initialHead {
		t.Fatalf("repoRoot head = %q, want new commit distinct from %q", got, initialHead)
	}
	if got := gitHeadForTest(t, otherRepo); got != otherHead {
		t.Fatalf("otherRepo head changed under inherited git env: got %q, want %q", got, otherHead)
	}
}

// ---------------------------------------------------------------------------
// F-04 — Symbol discovery: find returns matching symbols
// ---------------------------------------------------------------------------

func TestCert_RCv1_F04_SymbolDiscovery(t *testing.T) {
	store := openCertStore(t)
	defer store.Close()

	store.InsertSymbol(repocontext.Symbol{
		ID: "sym:main.go:Hello", Name: "Hello", Kind: "function",
		FilePath: "main.go", LineStart: 1,
	})

	results, err := store.FindSymbols("Hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "Hello" {
		t.Errorf("find: got %d results, want 1 named Hello", len(results))
	}
}

// ---------------------------------------------------------------------------
// F-05 — Dependency traversal: deps and rdeps return correct edges
// ---------------------------------------------------------------------------

func TestCert_RCv1_F05_DependencyTraversal(t *testing.T) {
	store := openCertStore(t)
	defer store.Close()

	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:A", Name: "A", Kind: "function", FilePath: "a.go", LineStart: 1})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:b.go:B", Name: "B", Kind: "function", FilePath: "b.go", LineStart: 1})
	store.InsertEdge(repocontext.Edge{SourceID: "sym:a.go:A", TargetID: "sym:b.go:B", Kind: "calls", FilePath: "a.go", LineNumber: 2})

	deps, err := store.Deps("sym:a.go:A")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].Target.ID != "sym:b.go:B" {
		t.Errorf("deps: got %v, want edge to B", deps)
	}

	rdeps, err := store.Rdeps("sym:b.go:B")
	if err != nil {
		t.Fatal(err)
	}
	if len(rdeps) != 1 || rdeps[0].Source.ID != "sym:a.go:A" {
		t.Errorf("rdeps: got %v, want edge from A", rdeps)
	}
}

// ---------------------------------------------------------------------------
// F-06 — Change impact summary
// ---------------------------------------------------------------------------

func TestCert_RCv1_F06_ChangeImpact(t *testing.T) {
	store := openCertStore(t)
	defer store.Close()

	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:A", Name: "A", Kind: "function", FilePath: "a.go", LineStart: 1})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:b.go:B", Name: "B", Kind: "function", FilePath: "b.go", LineStart: 1})
	store.InsertEdge(repocontext.Edge{SourceID: "sym:b.go:B", TargetID: "sym:a.go:A", Kind: "calls", FilePath: "b.go", LineNumber: 2})

	resp, err := repocontext.Impact(store, "/repo", []string{"a.go"}, "explicit")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.TouchedSymbols) == 0 {
		t.Error("impact: no touched symbols for changed file")
	}
	if len(resp.Dependents) == 0 {
		t.Error("impact: no dependents for file with rdeps")
	}
	if resp.RiskLevel == "" {
		t.Error("impact: missing risk level")
	}
}

// ---------------------------------------------------------------------------
// F-08 — Graceful degradation: missing index → advisory, not crash
// ---------------------------------------------------------------------------

func TestCert_RCv1_F08_GracefulDegradation(t *testing.T) {
	dir := t.TempDir()
	state := repocontext.IndexState(dir)
	if state != repocontext.IndexMissing {
		t.Errorf("IndexState for no-DB dir: got %q, want %q", state, repocontext.IndexMissing)
	}
}

// ---------------------------------------------------------------------------
// F-11 — Go-first scope: .go files indexed; others skipped
// ---------------------------------------------------------------------------

func TestCert_RCv1_F11_GoFirstScope(t *testing.T) {
	repoRoot := initGitRepo(t)
	writeGoFile(t, repoRoot, "main.go", "package main\nfunc Main() {}\n")
	os.WriteFile(filepath.Join(repoRoot, "script.py"), []byte("def hello(): pass\n"), 0o644)
	os.WriteFile(filepath.Join(repoRoot, "index.js"), []byte("function hi() {}\n"), 0o644)
	gitAdd(t, repoRoot, ".")

	dbPath := repocontext.DBPath(repoRoot)
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, err = repocontext.Index(store, repoRoot, repocontext.IndexOptions{Full: true})
	if err != nil {
		t.Fatal(err)
	}

	// Should find Go symbols only.
	goSyms, _ := store.FindSymbols("Main")
	if len(goSyms) == 0 {
		t.Error("Go function Main not indexed")
	}
	// Python and JS should not be indexed.
	pySyms, _ := store.FindSymbols("hello")
	if len(pySyms) != 0 {
		t.Errorf("Python function indexed: %v", pySyms)
	}
	jsSyms, _ := store.FindSymbols("hi")
	if len(jsSyms) != 0 {
		t.Errorf("JS function indexed: %v", jsSyms)
	}
}

// ---------------------------------------------------------------------------
// F-12 — Multi-repo compatibility: independent DB per repo
// ---------------------------------------------------------------------------

func TestCert_RCv1_F12_MultiRepoIndependence(t *testing.T) {
	repo1 := initGitRepo(t)
	repo2 := initGitRepo(t)
	writeGoFile(t, repo1, "a.go", "package main\nfunc RepoOne() {}\n")
	writeGoFile(t, repo2, "a.go", "package main\nfunc RepoTwo() {}\n")
	gitAdd(t, repo1, ".")
	gitAdd(t, repo2, ".")

	s1, err := repocontext.OpenStore(repocontext.DBPath(repo1))
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()
	s2, err := repocontext.OpenStore(repocontext.DBPath(repo2))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	if _, err := repocontext.Index(s1, repo1, repocontext.IndexOptions{Full: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := repocontext.Index(s2, repo2, repocontext.IndexOptions{Full: true}); err != nil {
		t.Fatal(err)
	}

	r1, _ := s1.FindSymbols("RepoOne")
	r2, _ := s2.FindSymbols("RepoTwo")
	if len(r1) == 0 {
		t.Error("repo1 missing RepoOne symbol")
	}
	if len(r2) == 0 {
		t.Error("repo2 missing RepoTwo symbol")
	}

	// Cross-check: repo1 must not have repo2's symbols.
	cross, _ := s1.FindSymbols("RepoTwo")
	if len(cross) != 0 {
		t.Error("repo1 has repo2's symbols — stores are not independent")
	}
}

// ---------------------------------------------------------------------------
// §4.4 — Risk heuristic matches minimum rule table
// ---------------------------------------------------------------------------

func TestCert_RCv1_S4_4_RiskHeuristic(t *testing.T) {
	// The spec minimum rule table:
	// ≥6 files OR ≥11 symbols OR ≥6 dependents → high
	// ≥3 files OR ≥3 symbols OR ≥1 dependent  → medium
	// otherwise → low
	tests := []struct {
		name              string
		files, syms, deps int
		want              string
	}{
		{"trivial", 1, 1, 0, "low"},
		{"medium-files", 3, 1, 0, "medium"},
		{"medium-deps", 1, 1, 1, "medium"},
		{"high-files", 6, 1, 0, "high"},
		{"high-symbols", 1, 11, 0, "high"},
		{"high-deps", 1, 1, 6, "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openCertStore(t)
			defer store.Close()

			for i := 0; i < tt.syms; i++ {
				id := "sym:a.go:" + string(rune('A'+i))
				store.InsertSymbol(repocontext.Symbol{ID: id, Name: string(rune('A' + i)), Kind: "function", FilePath: "a.go", LineStart: i + 1})
			}
			for i := 0; i < tt.deps; i++ {
				depID := "sym:dep.go:" + string(rune('X'+i))
				store.InsertSymbol(repocontext.Symbol{ID: depID, Name: string(rune('X' + i)), Kind: "function", FilePath: "dep.go", LineStart: i + 1})
				if tt.syms > 0 {
					store.InsertEdge(repocontext.Edge{SourceID: depID, TargetID: "sym:a.go:A", Kind: "calls", FilePath: "dep.go", LineNumber: i + 1})
				}
			}

			files := make([]string, tt.files)
			for i := range files {
				files[i] = "a.go"
			}

			resp, err := repocontext.Impact(store, "/repo", files, "explicit")
			if err != nil {
				t.Fatal(err)
			}
			if resp.RiskLevel != tt.want {
				t.Errorf("risk: got %q, want %q", resp.RiskLevel, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// §4.5 — Repo root resolution: walks up to find .git; exit 2 if not found
// ---------------------------------------------------------------------------

func TestCert_RCv1_S4_5_RepoRootResolution(t *testing.T) {
	// Walks up.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	sub := filepath.Join(dir, "a", "b", "c")
	os.MkdirAll(sub, 0o755)

	root, err := repocontext.ResolveRepoRoot(sub)
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Errorf("root: got %q, want %q", root, dir)
	}

	// No .git → error.
	noGit := t.TempDir()
	_, err = repocontext.ResolveRepoRoot(noGit)
	if err == nil {
		t.Error("expected error for dir without .git")
	}
}

// ---------------------------------------------------------------------------
// KATA-01/02/09 — Advisory-only boundary: no workflow-control imports
// ---------------------------------------------------------------------------

func TestCert_RCv1_KATA_AdvisoryOnlyBoundary(t *testing.T) {
	// Verify that internal/context does NOT import any workflow-control
	// packages. This is a compile-time boundary assertion.
	forbidden := []string{
		"github.com/codero/codero/internal/state",
		"github.com/codero/codero/internal/daemon",
		"github.com/codero/codero/internal/session",
		"github.com/codero/codero/internal/scheduler",
		"github.com/codero/codero/internal/webhook",
		"github.com/codero/codero/internal/delivery",
		"github.com/codero/codero/internal/feedback",
	}

	// Use go/token to parse the actual source files and check imports.
	fset := token.NewFileSet()
	_ = fset // We use a simpler approach: read source and check import paths.

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, pkg := range forbidden {
			if strings.Contains(string(data), `"`+pkg+`"`) {
				t.Errorf("%s imports forbidden package %s", e.Name(), pkg)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// KATA-03 — Separate store: graph.db is distinct from daemon SQLite
// ---------------------------------------------------------------------------

func TestCert_RCv1_KATA03_SeparateStore(t *testing.T) {
	if repocontext.DefaultDBRelPath == ".codero/codero.db" {
		t.Fatal("graph.db must not be the daemon database")
	}
	if !strings.Contains(repocontext.DefaultDBRelPath, "context") {
		t.Errorf("DBRelPath %q does not include 'context' namespace", repocontext.DefaultDBRelPath)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openCertStore(t *testing.T) *repocontext.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), ".codero", "context", "graph.db")
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "--initial-branch=main")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	return dir
}

func writeGoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitAdd(t *testing.T, dir, path string) {
	t.Helper()
	run(t, dir, "git", "add", path)
}

func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	run(t, dir, "git", "commit", "-m", msg, "--allow-empty")
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(cleanGitEnvForTest(os.Environ()),
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func gitHeadForTest(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	cmd.Env = cleanGitEnvForTest(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD in %s: %v\n%s", dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func cleanGitEnvForTest(env []string) []string {
	cleaned := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		cleaned = append(cleaned, kv)
	}
	return cleaned
}
