package main

import (
	"encoding/json"
	"fmt"
	"os"

	repocontext "github.com/codero/codero/internal/context"
	"github.com/spf13/cobra"
)

// contextCmd returns the `codero context` command group.
func contextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Repo-local code intelligence (advisory-only)",
	}

	cmd.AddCommand(
		contextIndexCmd(),
		contextStatusCmd(),
		contextFindCmd(),
		contextGrepCmd(),
		contextSymbolsCmd(),
		contextDepsCmd(),
		contextRdepsCmd(),
		contextImpactCmd(),
	)

	return cmd
}

// ──── index ──────────────────────────────────────────────────────────────

func contextIndexCmd() *cobra.Command {
	var full bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "index [repo]",
		Short: "Build or refresh the code-intelligence index",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepo(args)
			if err != nil {
				return contextError(cmd, repoRoot, "repo_resolution_failed", err.Error(), jsonOut, 2)
			}

			dbPath := repocontext.DBPath(repoRoot)
			store, err := repocontext.OpenStore(dbPath)
			if err != nil {
				return contextError(cmd, repoRoot, "store_open_failed", err.Error(), jsonOut, 1)
			}
			defer store.Close()

			result, err := repocontext.Index(store, repoRoot, repocontext.IndexOptions{Full: full})
			if err != nil {
				return contextError(cmd, repoRoot, "index_failed", err.Error(), jsonOut, 1)
			}

			meta := store.GetMetadata()
			resp := repocontext.IndexResponse{
				Envelope:      repocontext.NewEnvelope("index", repoRoot),
				Warnings:      result.Warnings,
				IndexState:    repocontext.IndexReady,
				DBPresent:     true,
				DBPath:        dbPath,
				FileCount:     result.Files,
				SymbolCount:   result.Symbols,
				EdgeCount:     result.Edges,
				LastIndexedAt: meta.LastIndexedAt,
			}
			if resp.Warnings == nil {
				resp.Warnings = []string{}
			}

			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "force full rebuild")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

// ──── status ─────────────────────────────────────────────────────────────

func contextStatusCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "status [repo]",
		Short: "Show index status and metadata",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepo(args)
			if err != nil {
				return contextError(cmd, repoRoot, "repo_resolution_failed", err.Error(), jsonOut, 2)
			}

			state := repocontext.IndexState(repoRoot)
			dbPath := repocontext.DBPath(repoRoot)
			dbPresent := repocontext.DBExists(repoRoot)

			resp := repocontext.StatusResponse{
				Envelope:   repocontext.NewEnvelope("status", repoRoot),
				IndexState: state,
				DBPresent:  dbPresent,
				DBPath:     dbPath,
			}

			if dbPresent {
				resp.DBSizeBytes = repocontext.DBSize(repoRoot)
				store, err := repocontext.OpenStore(dbPath)
				if err == nil {
					defer store.Close()
					meta := store.GetMetadata()
					resp.FileCount = store.FileCount()
					resp.SymbolCount = store.SymbolCount()
					resp.EdgeCount = store.EdgeCount()
					resp.LastIndexedAt = meta.LastIndexedAt
					resp.LastIndexedSHA = meta.LastIndexedSHA
					resp.LanguageScope = meta.LanguageScope
				}
			}

			// status always exits 0 per spec.
			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

// ──── find ───────────────────────────────────────────────────────────────

func contextFindCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "find <query> [repo]",
		Short: "Search for symbols by name",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			repoArgs := args[1:]
			repoRoot, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, "repo_resolution_failed", err.Error(), jsonOut, 2)
			}

			store, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, "index_not_built", "run 'codero context index' first", jsonOut, 1)
			}
			defer store.Close()

			matches, err := store.FindSymbols(query)
			if err != nil {
				return contextError(cmd, repoRoot, "query_failed", err.Error(), jsonOut, 1)
			}
			if matches == nil {
				matches = []repocontext.Symbol{}
			}

			resp := repocontext.FindResponse{
				Envelope: repocontext.NewEnvelope("find", repoRoot),
				Query:    query,
				Count:    len(matches),
				Matches:  matches,
			}
			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

// ──── grep ───────────────────────────────────────────────────────────────

func contextGrepCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "grep <pattern> [repo]",
		Short: "Search file contents for a regex pattern",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern := args[0]
			repoArgs := args[1:]
			repoRoot, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, "repo_resolution_failed", err.Error(), jsonOut, 2)
			}

			matches, err := repocontext.Grep(repoRoot, pattern)
			if err != nil {
				return contextError(cmd, repoRoot, "grep_failed", err.Error(), jsonOut, 1)
			}
			if matches == nil {
				matches = []repocontext.GrepMatch{}
			}

			resp := repocontext.GrepResponse{
				Envelope: repocontext.NewEnvelope("grep", repoRoot),
				Pattern:  pattern,
				Count:    len(matches),
				Matches:  matches,
			}
			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

// ──── symbols ────────────────────────────────────────────────────────────

func contextSymbolsCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "symbols <file> [repo]",
		Short: "List symbols declared in a file",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			repoArgs := args[1:]
			repoRoot, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, "repo_resolution_failed", err.Error(), jsonOut, 2)
			}

			store, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, "index_not_built", "run 'codero context index' first", jsonOut, 1)
			}
			defer store.Close()

			syms, err := store.SymbolsByFile(filePath)
			if err != nil {
				return contextError(cmd, repoRoot, "query_failed", err.Error(), jsonOut, 1)
			}
			if syms == nil {
				syms = []repocontext.Symbol{}
			}

			resp := repocontext.SymbolsResponse{
				Envelope: repocontext.NewEnvelope("symbols", repoRoot),
				FilePath: filePath,
				Count:    len(syms),
				Symbols:  syms,
			}
			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

// ──── deps ───────────────────────────────────────────────────────────────

func contextDepsCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "deps <symbol> [repo]",
		Short: "List direct dependencies of a symbol",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbolQuery := args[0]
			repoArgs := args[1:]
			repoRoot, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, "repo_resolution_failed", err.Error(), jsonOut, 2)
			}

			store, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, "index_not_built", "run 'codero context index' first", jsonOut, 1)
			}
			defer store.Close()

			subject := store.ResolveSubject(symbolQuery)
			if subject == nil {
				return contextError(cmd, repoRoot, "subject_not_found",
					fmt.Sprintf("symbol not found: %s", symbolQuery), jsonOut, 1)
			}

			deps, err := store.Deps(subject.ID)
			if err != nil {
				return contextError(cmd, repoRoot, "query_failed", err.Error(), jsonOut, 1)
			}
			if deps == nil {
				deps = []repocontext.DepEdge{}
			}

			resp := repocontext.DepsResponse{
				Envelope:     repocontext.NewEnvelope("deps", repoRoot),
				Subject:      *subject,
				Count:        len(deps),
				Dependencies: deps,
			}
			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

// ──── rdeps ──────────────────────────────────────────────────────────────

func contextRdepsCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "rdeps <symbol> [repo]",
		Short: "List reverse dependencies (what depends on a symbol)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbolQuery := args[0]
			repoArgs := args[1:]
			repoRoot, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, "repo_resolution_failed", err.Error(), jsonOut, 2)
			}

			store, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, "index_not_built", "run 'codero context index' first", jsonOut, 1)
			}
			defer store.Close()

			subject := store.ResolveSubject(symbolQuery)
			if subject == nil {
				return contextError(cmd, repoRoot, "subject_not_found",
					fmt.Sprintf("symbol not found: %s", symbolQuery), jsonOut, 1)
			}

			rdeps, err := store.Rdeps(subject.ID)
			if err != nil {
				return contextError(cmd, repoRoot, "query_failed", err.Error(), jsonOut, 1)
			}
			if rdeps == nil {
				rdeps = []repocontext.RdepEdge{}
			}

			resp := repocontext.RdepsResponse{
				Envelope:   repocontext.NewEnvelope("rdeps", repoRoot),
				Subject:    *subject,
				Count:      len(rdeps),
				Dependents: rdeps,
			}
			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	return cmd
}

// ──── impact ─────────────────────────────────────────────────────────────

func contextImpactCmd() *cobra.Command {
	var jsonOut bool
	var files []string

	cmd := &cobra.Command{
		Use:   "impact [repo]",
		Short: "Advisory impact analysis for changed files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepo(args)
			if err != nil {
				return contextError(cmd, repoRoot, "repo_resolution_failed", err.Error(), jsonOut, 2)
			}

			inputMode := "staged"
			if len(files) > 0 {
				inputMode = "explicit"
			} else {
				files = repocontext.StagedFiles(repoRoot)
			}

			store, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, "index_not_built", "run 'codero context index' first", jsonOut, 1)
			}
			defer store.Close()

			resp, err := repocontext.Impact(store, repoRoot, files, inputMode)
			if err != nil {
				return contextError(cmd, repoRoot, "impact_failed", err.Error(), jsonOut, 1)
			}
			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	cmd.Flags().StringSliceVar(&files, "files", nil, "explicit file paths (overrides staged detection)")
	return cmd
}

// ──── Helpers ────────────────────────────────────────────────────────────

func resolveRepo(args []string) (string, error) {
	if len(args) > 0 {
		return repocontext.ResolveRepoRoot(args[0])
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return repocontext.ResolveRepoRoot(cwd)
}

func openRepoStore(repoRoot string) (*repocontext.Store, error) {
	dbPath := repocontext.DBPath(repoRoot)
	if !repocontext.DBExists(repoRoot) {
		return nil, fmt.Errorf("index not built")
	}
	return repocontext.OpenStore(dbPath)
}

type contextErrorResult struct {
	exitCode int
}

func (e *contextErrorResult) Error() string { return fmt.Sprintf("exit %d", e.exitCode) }

func contextError(_ *cobra.Command, repoRoot, code, message string, jsonOut bool, exitCode int) error {
	if jsonOut {
		resp := repocontext.ErrorResponse{
			Envelope: repocontext.NewEnvelope("error", repoRoot),
			Error:    repocontext.ErrorDetail{Code: code, Message: message},
		}
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(os.Stderr, string(data))
	} else {
		fmt.Fprintf(os.Stderr, "error: %s: %s\n", code, message)
	}
	if exitCode == 2 {
		return &UsageError{msg: message}
	}
	return fmt.Errorf("%s: %s", code, message)
}

func writeJSON(v interface{}, jsonOut bool) error {
	if !jsonOut {
		// Simple text output for non-JSON mode.
		data, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
