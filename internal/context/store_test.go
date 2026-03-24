package context_test

import (
	"path/filepath"
	"testing"

	repocontext "github.com/codero/codero/internal/context"
)

// MI-006 requires dedicated store parity skeleton coverage alongside the
// broader repo-context contract tests.
func TestMI006StoreSkeleton_OpenStoreAndMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), ".codero", "context", "graph.db")
	repoRoot := filepath.Join(t.TempDir(), "repo-root")
	store, err := repocontext.OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	if err := store.SetMeta("schema_version", repocontext.SchemaVersion); err != nil {
		t.Fatalf("SetMeta schema_version: %v", err)
	}
	if err := store.SetMeta("repo.root", repoRoot); err != nil {
		t.Fatalf("SetMeta repo.root: %v", err)
	}

	meta := store.GetMetadata()
	if meta.SchemaVersion != repocontext.SchemaVersion {
		t.Fatalf("schema_version = %q, want %q", meta.SchemaVersion, repocontext.SchemaVersion)
	}
	if meta.RepoRoot != repoRoot {
		t.Fatalf("repo.root = %q, want %q", meta.RepoRoot, repoRoot)
	}
}

func TestMI006StoreSkeleton_SymbolAndEdgeRoundTrip(t *testing.T) {
	store := openTestStore(t)

	if err := store.InsertSymbol(repocontext.Symbol{
		ID:       "sym:a.go:Alpha",
		Name:     "Alpha",
		Kind:     repocontext.KindFunction,
		FilePath: "a.go",
	}); err != nil {
		t.Fatalf("InsertSymbol Alpha: %v", err)
	}
	if err := store.InsertSymbol(repocontext.Symbol{
		ID:       "sym:b.go:Beta",
		Name:     "Beta",
		Kind:     repocontext.KindFunction,
		FilePath: "b.go",
	}); err != nil {
		t.Fatalf("InsertSymbol Beta: %v", err)
	}
	if err := store.InsertEdge(repocontext.Edge{
		SourceID:   "sym:b.go:Beta",
		TargetID:   "sym:a.go:Alpha",
		Kind:       repocontext.EdgeCalls,
		FilePath:   "b.go",
		LineNumber: 8,
	}); err != nil {
		t.Fatalf("InsertEdge: %v", err)
	}

	deps, err := store.Deps("sym:b.go:Beta")
	if err != nil {
		t.Fatalf("Deps: %v", err)
	}
	if len(deps) != 1 || deps[0].Target.Name != "Alpha" {
		t.Fatalf("Deps = %+v, want Alpha dependency", deps)
	}
}
