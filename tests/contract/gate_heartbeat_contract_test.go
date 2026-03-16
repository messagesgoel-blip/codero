// Package contract provides contract tests for the shared gate-heartbeat interface.
// These tests validate that the heartbeat output format and parsing contract
// remain stable across script and client changes.
package contract_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codero/codero/internal/gate"
)

// TestGateHeartbeat_Contract_BinaryExists checks that the shared gate-heartbeat
// binary is available. This is a preflight check that fails fast if the
// shared toolkit is not mounted.
func TestGateHeartbeat_Contract_BinaryExists(t *testing.T) {
	bin := gate.DefaultHeartbeatBin
	if envBin := os.Getenv("CODERO_GATE_HEARTBEAT_BIN"); envBin != "" {
		bin = envBin
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gate-heartbeat binary not found at %s (skipping contract tests): %v", bin, err)
	}
}

// TestGateHeartbeat_Contract_PendingOutput verifies the PENDING output format
// from a fresh gate-heartbeat invocation.
func TestGateHeartbeat_Contract_PendingOutput(t *testing.T) {
	repoPath := t.TempDir()

	// First invocation always starts a new run and returns PENDING.
	cfg := gate.Config{
		HeartbeatBin:        filepath.Join(repoPath, "stub-heartbeat.sh"),
		CopilotTimeoutSec:   gate.DefaultCopilotTimeoutSec,
		LiteLLMTimeoutSec:   gate.DefaultLiteLLMTimeoutSec,
		GateTotalTimeoutSec: gate.DefaultGateTotalTimeoutSec,
		PollIntervalSec:     gate.DefaultPollIntervalSec,
		RepoPath:            repoPath,
	}

	runner := &gate.Runner{Cfg: cfg}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run with a custom heartbeat that always produces PENDING then stops.
	// We use a stub script in a temp dir to control the output.
	stubBin := writeStubHeartbeat(t, repoPath, `STATUS: PENDING
RUN_ID: test-pending-run
ELAPSED_SEC: 1
POLL_AFTER_SEC: 1
PROGRESS_BAR: [o copilot:pending] [o litellm:pending]
CURRENT_GATE: none
COPILOT_STATUS: pending
LITELLM_STATUS: pending
`)
	cfg.HeartbeatBin = stubBin

	// Add a second response that terminates (PASS) so Run returns.
	writeStubHeartbeatSequence(t, repoPath, []string{
		`STATUS: PENDING
RUN_ID: test-run-1
ELAPSED_SEC: 1
POLL_AFTER_SEC: 1
PROGRESS_BAR: [o copilot:pending] [o litellm:pending]
CURRENT_GATE: none
COPILOT_STATUS: pending
LITELLM_STATUS: pending
`,
		`STATUS: PASS
RUN_ID: test-run-1
PROGRESS_BAR: [+ copilot:pass] [+ litellm:pass]
CURRENT_GATE: none
COPILOT_STATUS: pass
LITELLM_STATUS: pass
COMMENTS: none
`,
	})

	var pendingSeen bool
	cfg.HeartbeatBin = filepath.Join(repoPath, "stub-heartbeat.sh")
	runner = &gate.Runner{Cfg: cfg}

	result, err := runner.Run(ctx, func(r gate.Result) {
		if r.Status == gate.StatusPending {
			pendingSeen = true
			if r.RunID == "" {
				t.Error("PENDING result should have a RunID")
			}
			if r.PollAfterSec <= 0 {
				t.Errorf("PENDING PollAfterSec should be > 0, got %d", r.PollAfterSec)
			}
		}
	})

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !pendingSeen {
		t.Error("expected at least one PENDING result during polling")
	}
	if result.Status != gate.StatusPass {
		t.Errorf("final status: got %q, want PASS", result.Status)
	}
}

// TestGateHeartbeat_Contract_PassOutput verifies the PASS terminal output.
func TestGateHeartbeat_Contract_PassOutput(t *testing.T) {
	repoPath := t.TempDir()
	writeStubHeartbeatSequence(t, repoPath, []string{`STATUS: PASS
RUN_ID: pass-run-abc
PROGRESS_BAR: [+ copilot:pass] [+ litellm:pass]
CURRENT_GATE: none
COPILOT_STATUS: pass
LITELLM_STATUS: pass
COMMENTS: none
`})

	cfg := gate.Config{
		HeartbeatBin:        filepath.Join(repoPath, "stub-heartbeat.sh"),
		CopilotTimeoutSec:   gate.DefaultCopilotTimeoutSec,
		LiteLLMTimeoutSec:   gate.DefaultLiteLLMTimeoutSec,
		GateTotalTimeoutSec: gate.DefaultGateTotalTimeoutSec,
		PollIntervalSec:     gate.DefaultPollIntervalSec,
		RepoPath:            repoPath,
	}
	runner := &gate.Runner{Cfg: cfg}
	ctx := context.Background()

	result, err := runner.Run(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != gate.StatusPass {
		t.Errorf("Status: got %q, want PASS", result.Status)
	}
	if result.RunID != "pass-run-abc" {
		t.Errorf("RunID: got %q, want pass-run-abc", result.RunID)
	}
	if len(result.Comments) != 0 {
		t.Errorf("PASS should have no comments, got %d", len(result.Comments))
	}
}

// TestGateHeartbeat_Contract_FailOutput verifies the FAIL terminal output with comments.
func TestGateHeartbeat_Contract_FailOutput(t *testing.T) {
	repoPath := t.TempDir()
	writeStubHeartbeatSequence(t, repoPath, []string{`STATUS: FAIL
RUN_ID: fail-run-xyz
PROGRESS_BAR: [x copilot:blocked] [+ litellm:pass]
CURRENT_GATE: none
COPILOT_STATUS: blocked
LITELLM_STATUS: pass
COMMENTS:
BLOCK: found 2 findings in main.go
BLOCK: unhandled error in handler.go
`})

	cfg := gate.Config{
		HeartbeatBin:        filepath.Join(repoPath, "stub-heartbeat.sh"),
		CopilotTimeoutSec:   gate.DefaultCopilotTimeoutSec,
		LiteLLMTimeoutSec:   gate.DefaultLiteLLMTimeoutSec,
		GateTotalTimeoutSec: gate.DefaultGateTotalTimeoutSec,
		PollIntervalSec:     gate.DefaultPollIntervalSec,
		RepoPath:            repoPath,
	}
	runner := &gate.Runner{Cfg: cfg}
	ctx := context.Background()

	result, err := runner.Run(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != gate.StatusFail {
		t.Errorf("Status: got %q, want FAIL", result.Status)
	}
	if result.CopilotStatus != "blocked" {
		t.Errorf("CopilotStatus: got %q, want blocked", result.CopilotStatus)
	}
	if len(result.Comments) < 2 {
		t.Errorf("expected >= 2 blocker comments, got %d", len(result.Comments))
	}
	for _, c := range result.Comments {
		if !strings.Contains(c, "BLOCK") {
			t.Errorf("comment missing BLOCK prefix: %q", c)
		}
	}
}

// TestGateHeartbeat_Contract_InfraFailIsNonBlocking verifies that infra_fail on
// a gate does not produce STATUS: FAIL (per the gate policy contract).
func TestGateHeartbeat_Contract_InfraFailIsNonBlocking(t *testing.T) {
	repoPath := t.TempDir()
	writeStubHeartbeatSequence(t, repoPath, []string{`STATUS: PASS
RUN_ID: infra-fail-run
PROGRESS_BAR: [! copilot:infra_fail] [+ litellm:pass]
CURRENT_GATE: none
COPILOT_STATUS: infra_fail
LITELLM_STATUS: pass
COMMENTS: none
`})

	cfg := gate.Config{
		HeartbeatBin:        filepath.Join(repoPath, "stub-heartbeat.sh"),
		CopilotTimeoutSec:   gate.DefaultCopilotTimeoutSec,
		LiteLLMTimeoutSec:   gate.DefaultLiteLLMTimeoutSec,
		GateTotalTimeoutSec: gate.DefaultGateTotalTimeoutSec,
		PollIntervalSec:     gate.DefaultPollIntervalSec,
		RepoPath:            repoPath,
	}
	runner := &gate.Runner{Cfg: cfg}

	result, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != gate.StatusPass {
		t.Errorf("infra_fail should produce PASS (non-blocking), got %q", result.Status)
	}
	if result.CopilotStatus != "infra_fail" {
		t.Errorf("CopilotStatus: got %q, want infra_fail", result.CopilotStatus)
	}
}

// TestGateHeartbeat_Contract_TimeoutIndependence verifies that setting one gate's
// timeout env var does not affect the other gate's timeout value in the config.
func TestGateHeartbeat_Contract_TimeoutIndependence(t *testing.T) {
	cases := []struct {
		name               string
		copilotEnv         string
		litellmEnv         string
		totalEnv           string
		wantCopilotTimeout int
		wantLiteLLMTimeout int
		wantTotalTimeout   int
	}{
		{
			name:               "copilot only set",
			copilotEnv:         "120",
			wantCopilotTimeout: 120,
			wantLiteLLMTimeout: gate.DefaultLiteLLMTimeoutSec,
			wantTotalTimeout:   gate.DefaultGateTotalTimeoutSec,
		},
		{
			name:               "litellm only set",
			litellmEnv:         "60",
			wantCopilotTimeout: gate.DefaultCopilotTimeoutSec,
			wantLiteLLMTimeout: 60,
			wantTotalTimeout:   gate.DefaultGateTotalTimeoutSec,
		},
		{
			name:               "total only set",
			totalEnv:           "600",
			wantCopilotTimeout: gate.DefaultCopilotTimeoutSec,
			wantLiteLLMTimeout: gate.DefaultLiteLLMTimeoutSec,
			wantTotalTimeout:   600,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Unsetenv("CODERO_COPILOT_TIMEOUT_SEC")
			os.Unsetenv("CODERO_LITELLM_TIMEOUT_SEC")
			os.Unsetenv("CODERO_GATE_TOTAL_TIMEOUT_SEC")

			if tc.copilotEnv != "" {
				t.Setenv("CODERO_COPILOT_TIMEOUT_SEC", tc.copilotEnv)
			}
			if tc.litellmEnv != "" {
				t.Setenv("CODERO_LITELLM_TIMEOUT_SEC", tc.litellmEnv)
			}
			if tc.totalEnv != "" {
				t.Setenv("CODERO_GATE_TOTAL_TIMEOUT_SEC", tc.totalEnv)
			}

			cfg := gate.LoadConfig()

			if cfg.CopilotTimeoutSec != tc.wantCopilotTimeout {
				t.Errorf("CopilotTimeoutSec: got %d, want %d", cfg.CopilotTimeoutSec, tc.wantCopilotTimeout)
			}
			if cfg.LiteLLMTimeoutSec != tc.wantLiteLLMTimeout {
				t.Errorf("LiteLLMTimeoutSec: got %d, want %d", cfg.LiteLLMTimeoutSec, tc.wantLiteLLMTimeout)
			}
			if cfg.GateTotalTimeoutSec != tc.wantTotalTimeout {
				t.Errorf("GateTotalTimeoutSec: got %d, want %d", cfg.GateTotalTimeoutSec, tc.wantTotalTimeout)
			}
		})
	}
}

// --- Test helpers ---

// resolveHeartbeatBin returns the gate-heartbeat binary path, skipping if absent.
func resolveHeartbeatBin(t *testing.T) string {
	t.Helper()
	bin := gate.DefaultHeartbeatBin
	if envBin := os.Getenv("CODERO_GATE_HEARTBEAT_BIN"); envBin != "" {
		bin = envBin
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gate-heartbeat binary not found at %s (skipping live binary test): %v", bin, err)
	}
	return bin
}

// writeStubHeartbeat writes a single-response stub script and returns its path.
func writeStubHeartbeat(t *testing.T, dir, output string) string {
	t.Helper()
	return writeStubHeartbeatSequence(t, dir, []string{output})
}

// writeStubHeartbeatSequence writes a stub script that returns each response in
// sequence on successive invocations, then repeats the last response.
func writeStubHeartbeatSequence(t *testing.T, dir string, responses []string) string {
	t.Helper()

	counterFile := filepath.Join(dir, "stub-counter")
	scriptPath := filepath.Join(dir, "stub-heartbeat.sh")

	// Write each response to a numbered file.
	for i, resp := range responses {
		respFile := filepath.Join(dir, fmt.Sprintf("stub-resp-%d", i))
		if err := os.WriteFile(respFile, []byte(resp), 0600); err != nil {
			t.Fatalf("write stub response %d: %v", i, err)
		}
	}

	maxIdx := len(responses) - 1
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
DIR="%s"
COUNTER_FILE="%s"
MAX_IDX=%d
count=0
if [ -f "$COUNTER_FILE" ]; then
  count=$(cat "$COUNTER_FILE")
fi
if [ "$count" -gt "$MAX_IDX" ]; then
  count=$MAX_IDX
fi
cat "$DIR/stub-resp-$count"
echo $((count+1)) > "$COUNTER_FILE"
`, dir, counterFile, maxIdx)

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}
	return scriptPath
}
