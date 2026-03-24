package context_test

import (
	"testing"

	repocontext "github.com/codero/codero/internal/context"
)

// MI-006 requires dedicated query parity skeleton coverage alongside the
// broader repo-context contract tests.
func TestMI006QueriesSkeleton_IndexStateMissing(t *testing.T) {
	repoDir := t.TempDir()
	if got := repocontext.IndexState(repoDir); got != repocontext.IndexMissing {
		t.Fatalf("IndexState = %q, want %q", got, repocontext.IndexMissing)
	}
}

func TestMI006QueriesSkeleton_ImpactEmptyInput(t *testing.T) {
	store := openTestStore(t)

	resp, err := repocontext.Impact(store, "/repo", nil, "staged")
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if resp.AnalysisState != "empty_input" {
		t.Fatalf("analysis_state = %q, want empty_input", resp.AnalysisState)
	}
	if len(resp.TouchedSymbols) != 0 || len(resp.Dependents) != 0 {
		t.Fatalf("expected empty impact payload, got touched=%d dependents=%d", len(resp.TouchedSymbols), len(resp.Dependents))
	}
}
