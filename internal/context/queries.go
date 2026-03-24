package context

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Grep searches files in the repo for a regex pattern.
// It operates on the live filesystem, not the graph store.
func Grep(repoRoot, pattern string) ([]GrepMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	goFiles, _ := collectGoFiles(repoRoot)
	var matches []GrepMatch

	for _, relPath := range goFiles {
		absPath := filepath.Join(repoRoot, relPath)
		fileMatches, err := grepFile(absPath, relPath, re)
		if err != nil {
			continue // skip unreadable files
		}
		matches = append(matches, fileMatches...)
	}

	return matches, nil
}

func grepFile(absPath, relPath string, re *regexp.Regexp) ([]GrepMatch, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var matches []GrepMatch
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		locs := re.FindStringIndex(line)
		if locs != nil {
			matches = append(matches, GrepMatch{
				FilePath:   relPath,
				LineNumber: lineNum,
				LineText:   line,
				MatchStart: locs[0],
				MatchEnd:   locs[1],
			})
		}
	}
	return matches, scanner.Err()
}

// Impact computes an advisory impact analysis for a set of files.
func Impact(store *Store, repoRoot string, files []string, inputMode string) (*ImpactResponse, error) {
	resp := &ImpactResponse{
		Envelope:  NewEnvelope("impact", repoRoot),
		InputMode: inputMode,
		Files:     files,
	}

	if len(files) == 0 {
		resp.AnalysisState = "empty_input"
		resp.Files = []string{}
		resp.TouchedSymbols = []Symbol{}
		resp.Dependents = []Symbol{}
		return resp, nil
	}

	resp.AnalysisState = "ok"

	// Find symbols in the touched files.
	touched, err := store.SymbolsInFiles(files)
	if err != nil {
		return nil, err
	}
	if touched == nil {
		touched = []Symbol{}
	}
	resp.TouchedSymbols = touched

	// Collect IDs and find reverse deps.
	var ids []string
	for _, s := range touched {
		ids = append(ids, s.ID)
	}
	dependents, err := store.RdepsOfSymbols(ids)
	if err != nil {
		return nil, err
	}
	if dependents == nil {
		dependents = []Symbol{}
	}

	// Filter out self-references (symbols already in touched set).
	touchedSet := make(map[string]bool, len(touched))
	for _, s := range touched {
		touchedSet[s.ID] = true
	}
	var filteredDeps []Symbol
	for _, d := range dependents {
		if !touchedSet[d.ID] {
			filteredDeps = append(filteredDeps, d)
		}
	}
	if filteredDeps == nil {
		filteredDeps = []Symbol{}
	}
	resp.Dependents = filteredDeps

	resp.Summary = ImpactSummary{
		Files:          len(files),
		TouchedSymbols: len(touched),
		Dependents:     len(filteredDeps),
	}

	// Risk heuristic per spec §4.
	resp.RiskLevel, resp.Reasons = computeRisk(len(files), len(touched), len(filteredDeps))

	return resp, nil
}

// computeRisk implements the spec's minimum risk-level table.
func computeRisk(nFiles, nSymbols, nDeps int) (string, []string) {
	switch {
	case nFiles > 5 || nSymbols > 10 || nDeps > 5:
		return RiskHigh, []string{
			fmt.Sprintf("Large change touching %d files, %d symbols with %d direct dependents", nFiles, nSymbols, nDeps),
		}
	case nFiles >= 3 || nSymbols >= 3 || nDeps >= 1:
		return RiskMedium, []string{
			fmt.Sprintf("Change touches %d files, %d symbols with %d direct dependents", nFiles, nSymbols, nDeps),
		}
	default:
		return RiskLow, []string{
			fmt.Sprintf("Change is isolated to %d files with %d symbols and no external dependents", nFiles, nSymbols),
		}
	}
}

// ResolveRepoRoot walks up from startDir looking for a .git directory.
func ResolveRepoRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo_resolution_failed: no .git found above %s", startDir)
		}
		dir = parent
	}
}

// DBPath returns the graph.db path for a repo root.
func DBPath(repoRoot string) string {
	return filepath.Join(repoRoot, DefaultDBRelPath)
}

// DBExists checks whether the graph.db file exists.
func DBExists(repoRoot string) bool {
	_, err := os.Stat(DBPath(repoRoot))
	return err == nil
}

// DBSize returns the size of graph.db in bytes, or 0 if missing.
func DBSize(repoRoot string) int64 {
	info, err := os.Stat(DBPath(repoRoot))
	if err != nil {
		return 0
	}
	return info.Size()
}

// IndexState determines the index state for a repo.
func IndexState(repoRoot string) string {
	path := DBPath(repoRoot)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return IndexMissing
	}
	store, err := OpenStore(path)
	if err != nil {
		return IndexRebuildRequired
	}
	defer store.Close()

	meta := store.GetMetadata()
	if meta.SchemaVersion != SchemaVersion {
		return IndexRebuildRequired
	}
	if meta.LastIndexedAt == "" {
		return IndexRebuildRequired
	}
	return IndexReady
}

// StagedFiles returns the list of staged file paths for a repo, used by impact.
func StagedFiles(repoRoot string) []string {
	files, err := gitStagedFiles(repoRoot)
	if err != nil {
		return nil
	}
	// Filter to files that exist.
	var result []string
	for _, f := range files {
		if !strings.HasPrefix(f, ".") || strings.Contains(f, "/") {
			result = append(result, f)
		}
	}
	return result
}
