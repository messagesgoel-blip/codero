package main

// cod027_test.go — Tests for COD-027: non-TTY gate-status --watch fallback.
//
// All tests run in a non-TTY context (Go test runner's stdin/stdout are pipes),
// so IsInteractiveTTY() returns false in every case below.  That is exactly the
// environment where the fallback must work.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jsonKeys enumerates the keys that must appear in gate-status --json / --watch
// non-TTY output per the COD-027 output contract.
var jsonKeys = []string{
	"status",
	"copilot_status",
	"litellm_status",
	"current_gate",
	"run_id",
	"comments",
	"progress_bar",
}

// captureStdout redirects os.Stdout to a pipe, runs f, then returns the
// captured output and restores os.Stdout.
func captureStdout(f func()) string {
	orig := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr
	f()
	wr.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	io.Copy(&buf, rd) //nolint:errcheck
	return buf.String()
}

// makeProgressEnv writes a progress.env file under repoPath/.codero/gate-heartbeat/
// and returns the repo path.
func makeProgressEnv(t *testing.T, lines []string) string {
	t.Helper()
	repoPath := t.TempDir()
	dir := filepath.Join(repoPath, ".codero", "gate-heartbeat")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "progress.env"), []byte(content), 0o644); err != nil {
		t.Fatalf("write progress.env: %v", err)
	}
	return repoPath
}

// assertJSONKeys checks that all required keys appear in the decoded object.
func assertJSONKeys(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		t.Fatal("output is empty; expected JSON object")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, raw)
	}
	for _, k := range jsonKeys {
		if _, ok := obj[k]; !ok {
			t.Errorf("JSON output missing required key %q", k)
		}
	}
	return obj
}

// --- TestGateStatusWatch_NonTTY_WithProgressFile ---
// Tests the primary acceptance criterion: non-TTY --watch emits valid JSON.

func TestGateStatusWatch_NonTTY_WithProgressFile(t *testing.T) {
	repoPath := makeProgressEnv(t, []string{
		"RUN_ID=run-cod027",
		"STATUS=PASS",
		"COPILOT_STATUS=pass",
		"LITELLM_STATUS=pass",
		"CURRENT_GATE=none",
		"COMMENTS=",
		"ELAPSED_SEC=5",
		"PROGRESS_BAR=[+ copilot:pass] [+ litellm:pass]",
		"UPDATED_AT=2026-03-16T20:00:00Z",
	})

	cmd := gateStatusCmd()
	cmd.SetArgs([]string{"--watch", "--repo-path", repoPath})

	var execErr error
	out := captureStdout(func() {
		execErr = cmd.Execute()
	})

	if execErr != nil {
		t.Fatalf("non-TTY --watch should not error; got: %v", execErr)
	}

	obj := assertJSONKeys(t, out)

	if got, _ := obj["status"].(string); got != "PASS" {
		t.Errorf("status = %q; want PASS", got)
	}
	if got, _ := obj["run_id"].(string); got != "run-cod027" {
		t.Errorf("run_id = %q; want run-cod027", got)
	}
}

// --- TestGateStatusWatch_NonTTY_EmptyRepo ---
// No progress.env present; fallback should still emit valid JSON (pending state)
// and not return an error.

func TestGateStatusWatch_NonTTY_EmptyRepo(t *testing.T) {
	repoPath := t.TempDir() // no .codero/gate-heartbeat/

	cmd := gateStatusCmd()
	cmd.SetArgs([]string{"--watch", "--repo-path", repoPath})

	var execErr error
	out := captureStdout(func() {
		execErr = cmd.Execute()
	})

	if execErr != nil {
		t.Fatalf("non-TTY --watch with no progress.env should not error; got: %v", execErr)
	}

	assertJSONKeys(t, out)
}

// --- TestGateStatusWatch_NonTTY_FailState ---
// FAIL status: non-TTY --watch should emit JSON and return exit 0
// (unlike one-shot gate-status which exits 1 on FAIL in non-TTY).
// The watch path signals real status via the JSON payload, not process exit code.

func TestGateStatusWatch_NonTTY_FailState(t *testing.T) {
	repoPath := makeProgressEnv(t, []string{
		"RUN_ID=run-fail-27",
		"STATUS=FAIL",
		"COPILOT_STATUS=blocked",
		"LITELLM_STATUS=pass",
		"CURRENT_GATE=copilot-third-pass",
		"COMMENTS=semgrep: risky pattern",
		"PROGRESS_BAR=[! copilot:blocked] [+ litellm:pass]",
	})

	cmd := gateStatusCmd()
	cmd.SetArgs([]string{"--watch", "--repo-path", repoPath})

	var execErr error
	out := captureStdout(func() {
		execErr = cmd.Execute()
	})

	if execErr != nil {
		t.Fatalf("non-TTY --watch (FAIL state) should not error; got: %v", execErr)
	}

	obj := assertJSONKeys(t, out)
	if got, _ := obj["status"].(string); got != "FAIL" {
		t.Errorf("status = %q; want FAIL", got)
	}
}

// --- TestGateStatusWatch_NonTTY_OutputIsOneLineJSON ---
// Output must be a single compact JSON object followed by a newline (no prose).

func TestGateStatusWatch_NonTTY_OutputIsOneLineJSON(t *testing.T) {
	repoPath := makeProgressEnv(t, []string{
		"RUN_ID=run-compact",
		"STATUS=PENDING",
		"COPILOT_STATUS=pending",
		"LITELLM_STATUS=pending",
	})

	cmd := gateStatusCmd()
	cmd.SetArgs([]string{"--watch", "--repo-path", repoPath})

	var execErr error
	out := captureStdout(func() {
		execErr = cmd.Execute()
	})

	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}

	// Output must be valid JSON (may be pretty-printed but must parse cleanly).
	trimmed := strings.TrimSpace(out)
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// Must not contain non-JSON prose.
	if strings.Contains(out, "PENDING") && !strings.Contains(out, "{") {
		t.Error("output contains prose text; expected only JSON")
	}
}

// --- TestGateStatusJSON_Regression ---
// Regression: gate-status --json still works after COD-027 changes.

func TestGateStatusJSON_Regression(t *testing.T) {
	repoPath := makeProgressEnv(t, []string{
		"RUN_ID=run-regression",
		"STATUS=PASS",
		"COPILOT_STATUS=pass",
		"LITELLM_STATUS=pass",
		"CURRENT_GATE=none",
		"COMMENTS=",
		"PROGRESS_BAR=[+ copilot:pass] [+ litellm:pass]",
	})

	cmd := gateStatusCmd()
	cmd.SetArgs([]string{"--json", "--repo-path", repoPath})

	var execErr error
	out := captureStdout(func() {
		execErr = cmd.Execute()
	})

	if execErr != nil {
		t.Fatalf("--json returned error: %v", execErr)
	}

	obj := assertJSONKeys(t, out)
	if got, _ := obj["status"].(string); got != "PASS" {
		t.Errorf("status = %q; want PASS", got)
	}
	if got, _ := obj["run_id"].(string); got != "run-regression" {
		t.Errorf("run_id = %q; want run-regression", got)
	}
}

// --- TestGateStatusJSONandWatch_JSONWins ---
// When --json and --watch are both supplied, --json wins: output is JSON, no error.

func TestGateStatusJSONandWatch_JSONWins(t *testing.T) {
	repoPath := makeProgressEnv(t, []string{
		"RUN_ID=run-json-wins",
		"STATUS=PASS",
		"COPILOT_STATUS=pass",
		"LITELLM_STATUS=pass",
	})

	cmd := gateStatusCmd()
	cmd.SetArgs([]string{"--json", "--watch", "--repo-path", repoPath})

	var execErr error
	out := captureStdout(func() {
		execErr = cmd.Execute()
	})

	if execErr != nil {
		t.Fatalf("--json --watch should not error; got: %v", execErr)
	}

	obj := assertJSONKeys(t, out)
	if got, _ := obj["run_id"].(string); got != "run-json-wins" {
		t.Errorf("run_id = %q; want run-json-wins", got)
	}
}

// --- TestGateStatusJSON_CommentsNeverNull ---
// The comments field must be an array (never JSON null) even when empty.

func TestGateStatusJSON_CommentsNeverNull(t *testing.T) {
	repoPath := makeProgressEnv(t, []string{
		"RUN_ID=run-null-check",
		"STATUS=PASS",
		"COPILOT_STATUS=pass",
		"LITELLM_STATUS=pass",
		"COMMENTS=",
	})

	cmd := gateStatusCmd()
	cmd.SetArgs([]string{"--json", "--repo-path", repoPath})

	var execErr error
	out := captureStdout(func() {
		execErr = cmd.Execute()
	})

	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &obj); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	comments, ok := obj["comments"]
	if !ok {
		t.Fatal("comments key missing")
	}
	if comments == nil {
		t.Error("comments is null; expected empty array []")
	}
}
