package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArgRange(cmd, args, 0, 1, jsonOut, "index [repo]"); err != nil {
				return err
			}

			repoRoot, code, err := resolveRepo(args)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}

			dbPath := repocontext.DBPath(repoRoot)
			store, err := repocontext.OpenStore(dbPath)
			if err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorDBError, err.Error(), jsonOut, 1)
			}
			defer store.Close()

			result, err := repocontext.Index(store, repoRoot, repocontext.IndexOptions{Full: full})
			if err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorDBError, err.Error(), jsonOut, 1)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArgRange(cmd, args, 0, 1, jsonOut, "status [repo]"); err != nil {
				return err
			}

			repoRoot, code, err := resolveRepo(args)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
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
				} else {
					fmt.Fprintf(os.Stderr, "warning: context status degraded: repo=%s db=%s: %v\n", repoRoot, dbPath, err)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArgRange(cmd, args, 1, 2, jsonOut, "find <query> [repo]"); err != nil {
				return err
			}

			query := args[0]
			repoArgs := args[1:]
			repoRoot, code, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}

			store, code, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}
			defer store.Close()

			matches, err := store.FindSymbols(query)
			if err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorDBError, err.Error(), jsonOut, 1)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArgRange(cmd, args, 1, 2, jsonOut, "grep <pattern> [repo]"); err != nil {
				return err
			}

			pattern := args[0]
			repoArgs := args[1:]
			repoRoot, code, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}

			if _, err := regexp.Compile(pattern); err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorUsage, err.Error(), jsonOut, 2)
			}

			matches, err := repocontext.Grep(repoRoot, pattern)
			if err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorDBError, err.Error(), jsonOut, 1)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArgRange(cmd, args, 1, 2, jsonOut, "symbols <file> [repo]"); err != nil {
				return err
			}

			filePath := args[0]
			repoArgs := args[1:]
			repoRoot, code, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}

			store, code, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}
			defer store.Close()

			syms, err := store.SymbolsByFile(filePath)
			if err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorDBError, err.Error(), jsonOut, 1)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArgRange(cmd, args, 1, 2, jsonOut, "deps <symbol> [repo]"); err != nil {
				return err
			}

			symbolQuery := args[0]
			repoArgs := args[1:]
			repoRoot, code, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}

			store, code, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}
			defer store.Close()

			subject := store.ResolveSubject(symbolQuery)
			if subject == nil {
				return contextError(cmd, repoRoot, repocontext.ErrorSubjectNotFound,
					fmt.Sprintf("symbol not found: %s", symbolQuery), jsonOut, 1)
			}

			deps, err := store.Deps(subject.ID)
			if err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorDBError, err.Error(), jsonOut, 1)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArgRange(cmd, args, 1, 2, jsonOut, "rdeps <symbol> [repo]"); err != nil {
				return err
			}

			symbolQuery := args[0]
			repoArgs := args[1:]
			repoRoot, code, err := resolveRepo(repoArgs)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}

			store, code, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}
			defer store.Close()

			subject := store.ResolveSubject(symbolQuery)
			if subject == nil {
				return contextError(cmd, repoRoot, repocontext.ErrorSubjectNotFound,
					fmt.Sprintf("symbol not found: %s", symbolQuery), jsonOut, 1)
			}

			rdeps, err := store.Rdeps(subject.ID)
			if err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorDBError, err.Error(), jsonOut, 1)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArgRange(cmd, args, 0, 1, jsonOut, "impact [repo]"); err != nil {
				return err
			}

			repoRoot, code, err := resolveRepo(args)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}

			inputMode := "staged"
			if len(files) > 0 {
				inputMode = "explicit"
			} else {
				files = repocontext.StagedFiles(repoRoot)
			}

			store, code, err := openRepoStore(repoRoot)
			if err != nil {
				return contextError(cmd, repoRoot, code, err.Error(), jsonOut, 1)
			}
			defer store.Close()

			resp, err := repocontext.Impact(store, repoRoot, files, inputMode)
			if err != nil {
				return contextError(cmd, repoRoot, repocontext.ErrorDBError, err.Error(), jsonOut, 1)
			}
			return writeJSON(resp, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "JSON output")
	cmd.Flags().StringSliceVar(&files, "files", nil, "explicit file paths (overrides staged detection)")
	return cmd
}

// ──── Helpers ────────────────────────────────────────────────────────────

func requireArgRange(cmd *cobra.Command, args []string, min, max int, jsonOut bool, usage string) error {
	if len(args) < min || len(args) > max {
		return contextError(cmd, bestEffortRepoRoot(), repocontext.ErrorUsage,
			fmt.Sprintf("usage: codero context %s", usage), jsonOut, 2)
	}
	return nil
}

func bestEffortRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	if root, err := repocontext.ResolveRepoRoot(cwd); err == nil {
		return root
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return cwd
	}
	return abs
}

func resolveRepo(args []string) (string, string, error) {
	if len(args) > 0 {
		abs, err := filepath.Abs(args[0])
		if err != nil {
			abs = args[0]
		}
		if _, err := os.Stat(args[0]); err != nil {
			if os.IsNotExist(err) {
				return abs, repocontext.ErrorRepoNotFound, fmt.Errorf("repo not found: %s", abs)
			}
			return abs, repocontext.ErrorRepoResolutionFailed, err
		}
		root, err := repocontext.ResolveRepoRoot(args[0])
		if err != nil {
			return abs, repocontext.ErrorRepoResolutionFailed, fmt.Errorf("repo root not found above %s", abs)
		}
		return root, "", nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", repocontext.ErrorRepoResolutionFailed, err
	}
	root, err := repocontext.ResolveRepoRoot(cwd)
	if err != nil {
		return bestEffortRepoRoot(), repocontext.ErrorRepoResolutionFailed, fmt.Errorf("repo root not found from current working directory")
	}
	return root, "", nil
}

func openRepoStore(repoRoot string) (*repocontext.Store, string, error) {
	switch repocontext.IndexState(repoRoot) {
	case repocontext.IndexMissing:
		return nil, repocontext.ErrorIndexMissing, fmt.Errorf("run 'codero context index' first")
	case repocontext.IndexRebuildRequired:
		return nil, repocontext.ErrorRebuildRequired, fmt.Errorf("context index requires rebuild; rerun 'codero context index --full'")
	}

	store, err := repocontext.OpenStore(repocontext.DBPath(repoRoot))
	if err != nil {
		return nil, repocontext.ErrorDBError, err
	}
	return store, "", nil
}

func contextError(_ *cobra.Command, repoRoot, code, message string, jsonOut bool, exitCode int) error {
	if jsonOut {
		resp := repocontext.ErrorResponse{
			Envelope: repocontext.NewEnvelope("error", repoRoot),
			Error:    repocontext.ErrorDetail{Code: code, Message: message},
		}
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(os.Stdout, string(data))
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
		// Non-JSON mode intentionally still emits indented JSON for now.
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
