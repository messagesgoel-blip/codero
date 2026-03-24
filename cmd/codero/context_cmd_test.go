package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	repocontext "github.com/codero/codero/internal/context"
)

func runContextCmd(t *testing.T, cwd string, args ...string) (string, string, error) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	defer func() {
		if stdoutW != nil {
			_ = stdoutW.Close()
		}
		if stderrW != nil {
			_ = stderrW.Close()
		}
		os.Stdout = origStdout
		os.Stderr = origStderr
		if chdirErr := os.Chdir(origWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	}()

	os.Stdout = stdoutW
	os.Stderr = stderrW
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("Chdir(%s): %v", cwd, err)
	}

	cmd := contextCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	execErr := cmd.ExecuteContext(context.Background())

	_ = stdoutW.Close()
	stdoutW = nil
	_ = stderrW.Close()
	stderrW = nil

	var stdoutBuf bytes.Buffer
	if _, err := io.Copy(&stdoutBuf, stdoutR); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	var stderrBuf bytes.Buffer
	if _, err := io.Copy(&stderrBuf, stderrR); err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	return stdoutBuf.String(), stderrBuf.String(), execErr
}

func setupContextCLIRepo(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module example.com/contextcli\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "a.go"), []byte(`package sample

func Alpha() {}

func Beta() {
	Alpha()
}
`), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "b.go"), []byte(`package sample

func Gamma() {
	Beta()
}
`), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}
	return repoDir
}

func decodeJSON[T any](t *testing.T, raw string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("unmarshal JSON: %v\nraw:\n%s", err, raw)
	}
	return v
}

func requireNoStderr(t *testing.T, stderr string) {
	t.Helper()
	if stderr != "" {
		t.Fatalf("expected empty stderr, got:\n%s", stderr)
	}
}

func TestContextStatusCmd_JSONMissingIndex(t *testing.T) {
	repoDir := setupContextCLIRepo(t)

	stdout, stderr, err := runContextCmd(t, repoDir, "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireNoStderr(t, stderr)

	resp := decodeJSON[repocontext.StatusResponse](t, stdout)
	if resp.SchemaVersion != repocontext.SchemaVersion {
		t.Fatalf("schema_version = %q, want %q", resp.SchemaVersion, repocontext.SchemaVersion)
	}
	if resp.IndexState != repocontext.IndexMissing {
		t.Fatalf("index_state = %q, want %q", resp.IndexState, repocontext.IndexMissing)
	}
	if resp.DBPresent {
		t.Fatal("expected db_present=false for missing index")
	}
}

func TestContextFindCmd_UsageErrorJSONExit2(t *testing.T) {
	repoDir := setupContextCLIRepo(t)

	stdout, stderr, err := runContextCmd(t, repoDir, "find", "--json")
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
	requireNoStderr(t, stderr)

	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected UsageError, got %T: %v", err, err)
	}

	resp := decodeJSON[repocontext.ErrorResponse](t, stdout)
	if resp.Error.Code != repocontext.ErrorUsage {
		t.Fatalf("error.code = %q, want %q", resp.Error.Code, repocontext.ErrorUsage)
	}
}

func TestContextIndexFindGrepAndSymbols_JSONContracts(t *testing.T) {
	repoDir := setupContextCLIRepo(t)

	stdout, stderr, err := runContextCmd(t, repoDir, "index", "--json")
	if err != nil {
		t.Fatalf("index --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireNoStderr(t, stderr)

	indexResp := decodeJSON[repocontext.IndexResponse](t, stdout)
	if indexResp.IndexState != repocontext.IndexReady {
		t.Fatalf("index_state = %q, want %q", indexResp.IndexState, repocontext.IndexReady)
	}
	if !indexResp.DBPresent || indexResp.DBPath == "" {
		t.Fatalf("unexpected index response: %+v", indexResp)
	}

	stdout, stderr, err = runContextCmd(t, repoDir, "find", "a", "--json")
	if err != nil {
		t.Fatalf("find --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireNoStderr(t, stderr)
	findResp := decodeJSON[repocontext.FindResponse](t, stdout)
	if findResp.Count < 3 {
		t.Fatalf("expected at least 3 matches, got %d", findResp.Count)
	}
	names := make([]string, 0, len(findResp.Matches))
	for _, match := range findResp.Matches {
		names = append(names, match.Name)
	}
	if !slices.Contains(names, "Alpha") || !slices.Contains(names, "Beta") || !slices.Contains(names, "Gamma") {
		t.Fatalf("find result set missing expected symbols: %+v", names)
	}

	stdout, stderr, err = runContextCmd(t, repoDir, "find", "NoSuchSymbol", "--json")
	if err != nil {
		t.Fatalf("find empty --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireNoStderr(t, stderr)
	emptyFind := decodeJSON[repocontext.FindResponse](t, stdout)
	if emptyFind.Count != 0 || len(emptyFind.Matches) != 0 {
		t.Fatalf("expected empty find result, got %+v", emptyFind)
	}

	stdout, stderr, err = runContextCmd(t, repoDir, "grep", "func", "--json")
	if err != nil {
		t.Fatalf("grep --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireNoStderr(t, stderr)
	grepResp := decodeJSON[repocontext.GrepResponse](t, stdout)
	if grepResp.Count < 3 {
		t.Fatalf("expected grep matches, got %d", grepResp.Count)
	}
	if grepResp.Matches[0].FilePath != "a.go" || grepResp.Matches[0].LineNumber != 3 {
		t.Fatalf("unexpected first grep match: %+v", grepResp.Matches[0])
	}
	if grepResp.Matches[1].FilePath != "a.go" || grepResp.Matches[1].LineNumber != 5 {
		t.Fatalf("unexpected second grep match: %+v", grepResp.Matches[1])
	}
	if grepResp.Matches[2].FilePath != "b.go" || grepResp.Matches[2].LineNumber != 3 {
		t.Fatalf("unexpected third grep match: %+v", grepResp.Matches[2])
	}

	stdout, stderr, err = runContextCmd(t, repoDir, "symbols", "a.go", "--json")
	if err != nil {
		t.Fatalf("symbols --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireNoStderr(t, stderr)
	symbolsResp := decodeJSON[repocontext.SymbolsResponse](t, stdout)
	if symbolsResp.Count != 2 {
		t.Fatalf("expected 2 symbols in a.go, got %d", symbolsResp.Count)
	}
	if symbolsResp.Symbols[0].Name != "Alpha" || symbolsResp.Symbols[1].Name != "Beta" {
		t.Fatalf("symbols order mismatch: %+v", symbolsResp.Symbols)
	}
}

func TestContextDepsAndRdeps_SubjectNotFoundJSONError(t *testing.T) {
	repoDir := setupContextCLIRepo(t)
	if stdout, stderr, err := runContextCmd(t, repoDir, "index", "--json"); err != nil {
		t.Fatalf("index --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	for _, args := range [][]string{
		{"deps", "Missing", "--json"},
		{"rdeps", "Missing", "--json"},
	} {
		stdout, stderr, err := runContextCmd(t, repoDir, args...)
		if err == nil {
			t.Fatalf("%v: expected error, got nil", args)
		}
		requireNoStderr(t, stderr)

		var usageErr *UsageError
		if errors.As(err, &usageErr) {
			t.Fatalf("%v: expected exit-1 style error, got UsageError: %v", args, err)
		}

		resp := decodeJSON[repocontext.ErrorResponse](t, stdout)
		if resp.Error.Code != repocontext.ErrorSubjectNotFound {
			t.Fatalf("%v: error.code = %q, want %q", args, resp.Error.Code, repocontext.ErrorSubjectNotFound)
		}
	}
}

func TestContextImpactCmd_EmptyInputJSONExit0(t *testing.T) {
	repoDir := setupContextCLIRepo(t)
	if stdout, stderr, err := runContextCmd(t, repoDir, "index", "--json"); err != nil {
		t.Fatalf("index --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	stdout, stderr, err := runContextCmd(t, repoDir, "impact", "--json")
	if err != nil {
		t.Fatalf("impact --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireNoStderr(t, stderr)

	resp := decodeJSON[repocontext.ImpactResponse](t, stdout)
	if resp.AnalysisState != "empty_input" {
		t.Fatalf("analysis_state = %q, want empty_input", resp.AnalysisState)
	}
	if resp.InputMode != "staged" {
		t.Fatalf("input_mode = %q, want staged", resp.InputMode)
	}
}
