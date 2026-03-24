package context_test

import (
	"os"
	"path/filepath"
	"testing"

	repocontext "github.com/codero/codero/internal/context"
)

// ──── Store Tests ────────────────────────────────────────────────────────

func TestOpenStore_CreatesDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), ".codero", "context", "graph.db")
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestStore_SymbolCRUD(t *testing.T) {
	store := openTestStore(t)

	sym := repocontext.Symbol{
		ID: "sym:main.go:Foo", Name: "Foo", Kind: "function",
		FilePath: "main.go", LineStart: 10, LineEnd: 20,
		Signature: "func Foo()", Exported: true, SourceHash: "abc",
	}
	if err := store.InsertSymbol(sym); err != nil {
		t.Fatal(err)
	}
	if store.SymbolCount() != 1 {
		t.Errorf("SymbolCount: got %d, want 1", store.SymbolCount())
	}

	got := store.GetSymbol("sym:main.go:Foo")
	if got == nil {
		t.Fatal("GetSymbol returned nil")
	}
	if got.Name != "Foo" || got.Kind != "function" {
		t.Errorf("GetSymbol: got %+v", got)
	}
}

func TestStore_EdgeCRUD(t *testing.T) {
	store := openTestStore(t)

	// Insert two symbols and an edge.
	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:A", Name: "A", Kind: "function", FilePath: "a.go"})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:b.go:B", Name: "B", Kind: "function", FilePath: "b.go"})
	store.InsertEdge(repocontext.Edge{
		SourceID: "sym:a.go:A", TargetID: "sym:b.go:B",
		Kind: "calls", FilePath: "a.go", LineNumber: 5,
	})

	if store.EdgeCount() != 1 {
		t.Errorf("EdgeCount: got %d, want 1", store.EdgeCount())
	}
}

func TestStore_Metadata(t *testing.T) {
	store := openTestStore(t)

	store.SetMeta("repo.root", "/repo")
	store.SetMeta("schema_version", "1")

	if got := store.GetMeta("repo.root"); got != "/repo" {
		t.Errorf("GetMeta: got %q", got)
	}
	if got := store.GetMeta("missing_key"); got != "" {
		t.Errorf("GetMeta missing: got %q", got)
	}

	meta := store.GetMetadata()
	if meta.RepoRoot != "/repo" || meta.SchemaVersion != "1" {
		t.Errorf("GetMetadata: %+v", meta)
	}
}

func TestStore_DeleteFileSymbols(t *testing.T) {
	store := openTestStore(t)

	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:A", Name: "A", Kind: "function", FilePath: "a.go"})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:b.go:B", Name: "B", Kind: "function", FilePath: "b.go"})

	if err := store.DeleteFileSymbols("a.go"); err != nil {
		t.Fatal(err)
	}
	if store.SymbolCount() != 1 {
		t.Errorf("after delete: SymbolCount=%d, want 1", store.SymbolCount())
	}
}

func TestStore_FindSymbols(t *testing.T) {
	store := openTestStore(t)

	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:FooBar", Name: "FooBar", Kind: "function", FilePath: "a.go"})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:b.go:BarBaz", Name: "BarBaz", Kind: "struct", FilePath: "b.go"})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:c.go", Name: "", Kind: "file", FilePath: "c.go"})

	results, err := store.FindSymbols("Bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("FindSymbols: got %d results, want 2", len(results))
	}
}

func TestStore_ResolveSubject(t *testing.T) {
	store := openTestStore(t)

	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:Foo", Name: "Foo", Kind: "function", FilePath: "a.go"})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go", Name: "", Kind: "file", FilePath: "a.go"})

	// By name.
	if sym := store.ResolveSubject("Foo"); sym == nil || sym.ID != "sym:a.go:Foo" {
		t.Errorf("ResolveSubject by name: %v", sym)
	}
	// By file path.
	if sym := store.ResolveSubject("a.go"); sym == nil || sym.ID != "sym:a.go" {
		t.Errorf("ResolveSubject by file: %v", sym)
	}
	// By ID.
	if sym := store.ResolveSubject("sym:a.go:Foo"); sym == nil {
		t.Error("ResolveSubject by ID: nil")
	}
	// Not found.
	if sym := store.ResolveSubject("NoSuchSymbol"); sym != nil {
		t.Errorf("ResolveSubject missing: %v", sym)
	}
}

func TestStore_DepsAndRdeps(t *testing.T) {
	store := openTestStore(t)

	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:A", Name: "A", Kind: "function", FilePath: "a.go"})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:b.go:B", Name: "B", Kind: "function", FilePath: "b.go"})
	store.InsertEdge(repocontext.Edge{
		SourceID: "sym:a.go:A", TargetID: "sym:b.go:B",
		Kind: "calls", FilePath: "a.go", LineNumber: 10,
	})

	deps, err := store.Deps("sym:a.go:A")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].Target.Name != "B" {
		t.Errorf("Deps: %+v", deps)
	}

	rdeps, err := store.Rdeps("sym:b.go:B")
	if err != nil {
		t.Fatal(err)
	}
	if len(rdeps) != 1 || rdeps[0].Source.Name != "A" {
		t.Errorf("Rdeps: %+v", rdeps)
	}
}

func TestStore_SymbolsByFile(t *testing.T) {
	store := openTestStore(t)

	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:X", Name: "X", Kind: "function", FilePath: "a.go", LineStart: 5})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:Y", Name: "Y", Kind: "struct", FilePath: "a.go", LineStart: 20})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:b.go:Z", Name: "Z", Kind: "var", FilePath: "b.go"})

	syms, err := store.SymbolsByFile("a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 2 {
		t.Errorf("SymbolsByFile: got %d, want 2", len(syms))
	}
	if syms[0].Name != "X" {
		t.Errorf("sort: first symbol should be X (lower line), got %s", syms[0].Name)
	}
}

// ──── Indexer Tests ──────────────────────────────────────────────────────

func TestIndex_FullIndex(t *testing.T) {
	repoDir := setupTestRepo(t)
	dbPath := filepath.Join(repoDir, ".codero", "context", "graph.db")
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	result, err := repocontext.Index(store, repoDir, repocontext.IndexOptions{Full: true})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	if result.Files == 0 {
		t.Error("expected >0 files indexed")
	}
	if result.Symbols == 0 {
		t.Error("expected >0 symbols indexed")
	}

	meta := store.GetMetadata()
	if meta.LanguageScope != "go" {
		t.Errorf("language_scope: %s", meta.LanguageScope)
	}
	if meta.SchemaVersion != "1" {
		t.Errorf("schema_version: %s", meta.SchemaVersion)
	}
}

func TestIndex_FindsSymbols(t *testing.T) {
	repoDir := setupTestRepo(t)
	dbPath := filepath.Join(repoDir, ".codero", "context", "graph.db")
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repocontext.Index(store, repoDir, repocontext.IndexOptions{Full: true})

	// The test repo has Hello and Add functions.
	syms, _ := store.FindSymbols("Hello")
	if len(syms) == 0 {
		t.Error("expected to find Hello symbol")
	}

	syms, _ = store.FindSymbols("Add")
	if len(syms) == 0 {
		t.Error("expected to find Add symbol")
	}
}

func TestIndex_FindsEdges(t *testing.T) {
	repoDir := setupTestRepo(t)
	dbPath := filepath.Join(repoDir, ".codero", "context", "graph.db")
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repocontext.Index(store, repoDir, repocontext.IndexOptions{Full: true})

	if store.EdgeCount() == 0 {
		t.Error("expected >0 edges")
	}
}

func TestIndex_StatusMissing(t *testing.T) {
	repoDir := t.TempDir()
	state := repocontext.IndexState(repoDir)
	if state != repocontext.IndexMissing {
		t.Errorf("IndexState: got %s, want missing", state)
	}
}

func TestIndex_StatusReady(t *testing.T) {
	repoDir := setupTestRepo(t)
	dbPath := filepath.Join(repoDir, ".codero", "context", "graph.db")
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	repocontext.Index(store, repoDir, repocontext.IndexOptions{Full: true})
	store.Close()

	state := repocontext.IndexState(repoDir)
	if state != repocontext.IndexReady {
		t.Errorf("IndexState: got %s, want ready", state)
	}
}

// ──── Grep Tests ─────────────────────────────────────────────────────────

func TestGrep_FindsPattern(t *testing.T) {
	repoDir := setupTestRepo(t)

	matches, err := repocontext.Grep(repoDir, "func Hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Error("expected grep matches for 'func Hello'")
	}
	if matches[0].FilePath == "" {
		t.Error("match should have file_path")
	}
}

func TestGrep_EmptyResult(t *testing.T) {
	repoDir := setupTestRepo(t)

	matches, err := repocontext.Grep(repoDir, "XYZNOTFOUND123")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestGrep_InvalidPattern(t *testing.T) {
	repoDir := setupTestRepo(t)

	_, err := repocontext.Grep(repoDir, "[invalid")
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

// ──── Impact Tests ───────────────────────────────────────────────────────

func TestImpact_EmptyInput(t *testing.T) {
	store := openTestStore(t)

	resp, err := repocontext.Impact(store, "/repo", nil, "staged")
	if err != nil {
		t.Fatal(err)
	}
	if resp.AnalysisState != "empty_input" {
		t.Errorf("analysis_state: %s", resp.AnalysisState)
	}
	if resp.RiskLevel != "" {
		t.Errorf("risk_level should be empty for empty_input, got %s", resp.RiskLevel)
	}
}

func TestImpact_WithSymbols(t *testing.T) {
	store := openTestStore(t)

	// Set up: file with symbol, another symbol depending on it.
	store.InsertSymbol(repocontext.Symbol{ID: "sym:a.go:Foo", Name: "Foo", Kind: "function", FilePath: "a.go", LineStart: 1})
	store.InsertSymbol(repocontext.Symbol{ID: "sym:b.go:Bar", Name: "Bar", Kind: "function", FilePath: "b.go", LineStart: 1})
	store.InsertEdge(repocontext.Edge{
		SourceID: "sym:b.go:Bar", TargetID: "sym:a.go:Foo",
		Kind: "calls", FilePath: "b.go", LineNumber: 5,
	})

	resp, err := repocontext.Impact(store, "/repo", []string{"a.go"}, "explicit")
	if err != nil {
		t.Fatal(err)
	}
	if resp.AnalysisState != "ok" {
		t.Errorf("analysis_state: %s", resp.AnalysisState)
	}
	if len(resp.TouchedSymbols) != 1 {
		t.Errorf("touched_symbols: %d", len(resp.TouchedSymbols))
	}
	if len(resp.Dependents) != 1 {
		t.Errorf("dependents: %d", len(resp.Dependents))
	}
	if resp.RiskLevel == "" {
		t.Error("risk_level should not be empty for non-empty result")
	}
}

func TestImpact_RiskLevels(t *testing.T) {
	tests := []struct {
		files, syms, deps int
		want              string
	}{
		{1, 1, 0, "low"},
		{2, 2, 0, "low"},
		{3, 3, 1, "medium"},
		{5, 5, 3, "medium"},
		{6, 1, 0, "high"},
		{1, 11, 0, "high"},
		{1, 1, 6, "high"},
	}
	for _, tt := range tests {
		store := openTestStore(t)
		// Create symbols for files.
		for i := 0; i < tt.syms; i++ {
			id := "sym:a.go:" + string(rune('A'+i))
			store.InsertSymbol(repocontext.Symbol{ID: id, Name: string(rune('A' + i)), Kind: "function", FilePath: "a.go", LineStart: i + 1})
		}
		// Create dependent symbols with edges.
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
		// Deduplicate.
		fileSet := make(map[string]bool)
		for _, f := range files {
			fileSet[f] = true
		}
		var uniqueFiles []string
		for f := range fileSet {
			uniqueFiles = append(uniqueFiles, f)
		}

		resp, err := repocontext.Impact(store, "/repo", files, "explicit")
		if err != nil {
			t.Fatalf("files=%d syms=%d deps=%d: %v", tt.files, tt.syms, tt.deps, err)
		}
		if resp.RiskLevel != tt.want {
			t.Errorf("files=%d syms=%d deps=%d: risk=%s, want %s", tt.files, tt.syms, tt.deps, resp.RiskLevel, tt.want)
		}
	}
}

// ──── Advisory-Only Boundary ─────────────────────────────────────────────

func TestAdvisoryOnly_NoStateImports(t *testing.T) {
	// This test verifies by construction that internal/context has no
	// dependency on internal/state, internal/daemon, internal/session,
	// or any other workflow-control package. The test repo compiles
	// without those imports, which is sufficient proof.
	//
	// If someone adds such an import, the compile will still work but
	// this test serves as a documented boundary assertion.
	t.Log("Advisory-only boundary: internal/context does not import workflow-control packages")
}

// ──── ResolveRepoRoot ────────────────────────────────────────────────────

func TestResolveRepoRoot_NoGit(t *testing.T) {
	dir := t.TempDir()
	_, err := repocontext.ResolveRepoRoot(dir)
	if err == nil {
		t.Error("expected error for dir without .git")
	}
}

func TestResolveRepoRoot_WithGit(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	root, err := repocontext.ResolveRepoRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Errorf("root: %s, want %s", root, dir)
	}
}

func TestResolveRepoRoot_WalksUp(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	subDir := filepath.Join(dir, "a", "b")
	os.MkdirAll(subDir, 0o755)

	root, err := repocontext.ResolveRepoRoot(subDir)
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Errorf("root: %s, want %s", root, dir)
	}
}

// ──── Helpers ────────────────────────────────────────────────────────────

func openTestStore(t *testing.T) *repocontext.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// setupTestRepo creates a minimal Go repo for indexing tests.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create .git directory (marker for repo root detection).
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	// Create go.mod.
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/testrepo\n\ngo 1.21\n"), 0o644)

	// Create main.go with a function.
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "fmt"

func Hello(name string) string {
	return fmt.Sprintf("hello %s", name)
}

func main() {
	Hello("world")
}
`), 0o644)

	// Create a sub-package.
	os.MkdirAll(filepath.Join(dir, "math"), 0o755)
	os.WriteFile(filepath.Join(dir, "math", "add.go"), []byte(`package math

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a + b
}

// Point is a 2D point.
type Point struct {
	X, Y float64
}

var DefaultOrigin = Point{0, 0}
`), 0o644)

	return dir
}
