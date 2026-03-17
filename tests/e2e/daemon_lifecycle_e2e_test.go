//go:build e2e

// tests/e2e/daemon_lifecycle_e2e_test.go
//
// E2E release-gate: full daemon lifecycle
//
// Exercises, in order:
//   internal/config   — env-driven config load
//   internal/daemon   — PID write, signal handling, Redis health check
//   internal/redis    — client construction and PING
//   internal/state    — DB init + migration on daemon start
//   cmd/codero        — `daemon`, `status`, and `version` subcommands
//                       under real process semantics
//
// Test inventory (release-gate surface):
//   TestVersion_NoEnv_E2E       — fast-mode: version exits 0 with no env
//   TestStatusStalePID_E2E      — stale PID detection surfaces clear error
//   TestDaemonLifecycle_E2E     — full start/ready/health/stop cycle
//   TestDaemonRestart_E2E       — graceful stop followed by clean restart
//
// Run with:
//   go test -tags=e2e -count=1 ./tests/e2e/ -v -timeout 60s
//   go test -tags=e2e -race   ./tests/e2e/ -v -timeout 60s
//
// Environment contract:
//   CODERO_SKIP_GITHUB_SCOPE_CHECK=true is set automatically by buildTestEnv.
//   A placeholder GITHUB_TOKEN and CODERO_REPOS are injected; no live GitHub
//   API calls are made during any test. miniredis provides an in-process Redis.
//
// SKIP semantics:
//   Tests skip automatically when the e2e build tag is absent (tag-gated).
//   Individual tests that require the daemon log a Fatal if the daemon binary
//   cannot be built — a build failure is a release blocker, not a skip.

package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

const (
	startupTimeout  = 10 * time.Second
	shutdownTimeout = 5 * time.Second
	pollInterval    = 100 * time.Millisecond
)

// buildBinary compiles the codero binary into a test-owned temp dir.
// Pre-building mirrors production and keeps polling timing predictable.
func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codero")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/codero")
	cmd.Dir = "." // tests/e2e/
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

// pollUntil calls f every pollInterval until it returns true or deadline fires.
func pollUntil(t *testing.T, deadline time.Duration, f func() bool) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	for {
		if f() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollInterval):
		}
	}
}

// runStatus executes `codero status` with the provided environment and returns
// (combined stdout+stderr, exit code).
func runStatus(t *testing.T, bin string, env []string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, "status") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("status exec error: %v", err)
		}
	}
	return string(out), code
}

// freePort allocates and immediately releases a TCP port on the loopback
// interface. The port is free at the moment of return.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// buildTestEnv constructs a complete, isolated environment for daemon tests.
// It starts from os.Environ(), strips existing values for all controlled keys
// (preventing duplicate-key ambiguity), then sets deterministic test values:
//   - placeholder GITHUB_TOKEN + CODERO_REPOS satisfying config.Validate
//   - CODERO_SKIP_GITHUB_SCOPE_CHECK=true to bypass the live GitHub API call
//   - the provided miniredis address, PID file path, and DB path
//   - loopback observability host + the caller's ephemeral port
func buildTestEnv(redisAddr, pidFile, dbFile string, obsPort int) []string {
	controlled := map[string]bool{
		"GITHUB_TOKEN":                   true,
		"CODERO_REPOS":                   true,
		"CODERO_SKIP_GITHUB_SCOPE_CHECK": true,
		"CODERO_REDIS_ADDR":              true,
		"CODERO_REDIS_PASS":              true,
		"CODERO_PID_FILE":                true,
		"CODERO_DB_PATH":                 true,
		"CODERO_LOG_LEVEL":               true,
		"CODERO_OBSERVABILITY_HOST":      true,
		"CODERO_OBSERVABILITY_PORT":      true,
	}
	var base []string
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if !controlled[key] {
			base = append(base, e)
		}
	}
	// Placeholder satisfies config.Validate (non-empty); CODERO_SKIP_GITHUB_SCOPE_CHECK
	// prevents any live API call with this value.
	const fakeToken = "e2e-placeholder-no-api-call"
	return append(base,
		"GITHUB_TOKEN="+fakeToken,
		"CODERO_REPOS=example/e2e-test",
		"CODERO_SKIP_GITHUB_SCOPE_CHECK=true",
		"CODERO_REDIS_ADDR="+redisAddr,
		"CODERO_REDIS_PASS=",
		"CODERO_PID_FILE="+pidFile,
		"CODERO_DB_PATH="+dbFile,
		"CODERO_LOG_LEVEL=debug",
		"CODERO_OBSERVABILITY_HOST=127.0.0.1",
		fmt.Sprintf("CODERO_OBSERVABILITY_PORT=%d", obsPort),
	)
}

// TestVersion_NoEnv_E2E verifies that `codero version` exits 0 and prints a
// non-empty version string without any environment variables or external
// services. This proves fast-mode invocation: the binary is usable immediately
// on install — no daemon, no Redis, and no GitHub token required.
func TestVersion_NoEnv_E2E(t *testing.T) {
	bin := buildBinary(t)

	cmd := exec.Command(bin, "version") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	// Provide only PATH; strip everything else to confirm zero-env operation.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codero version: unexpected error: %v\noutput: %s", err, out)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Error("codero version: produced no output; expected a non-empty version string")
	}
}

// TestStatusStalePID_E2E verifies that `codero status` detects a stale PID
// file — one whose PID belongs to a dead process — and reports a clear,
// actionable error. Release-critical: a stale PID must never silently report
// "running"; operators must be able to distinguish daemon absence from stale
// state to recover cleanly.
func TestStatusStalePID_E2E(t *testing.T) {
	bin := buildBinary(t)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "codero.pid")
	dbFile := filepath.Join(tmpDir, "codero.db")

	// Start a short-lived process, record its PID, kill and reap it so the
	// PID is guaranteed dead by the time runStatus executes.
	sleepCmd := exec.Command("sleep", "300")
	if err := sleepCmd.Start(); err != nil {
		t.Fatalf("start sleep helper: %v", err)
	}
	deadPID := sleepCmd.Process.Pid
	_ = sleepCmd.Process.Kill()
	_ = sleepCmd.Wait()

	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", deadPID)), 0600); err != nil {
		t.Fatalf("write stale PID file: %v", err)
	}

	env := buildTestEnv(mr.Addr(), pidFile, dbFile, freePort(t))
	out, code := runStatus(t, bin, env)

	if code == 0 {
		t.Errorf("stale PID: want non-zero exit, got 0\noutput: %q", out)
	}
	if !strings.Contains(out, "stale") {
		t.Errorf("stale PID: want 'stale' in output (PID %d), got: %q", deadPID, out)
	}
}

func TestDaemonLifecycle_E2E(t *testing.T) {
	// ── 0. Build binary ──────────────────────────────────────────────────────
	bin := buildBinary(t)

	// ── 1. Spin up miniredis ─────────────────────────────────────────────────
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis start: %v", err)
	}
	t.Cleanup(mr.Close)

	// ── 2. Scratch paths and ephemeral observability port ────────────────────
	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "codero.pid")
	dbFile := filepath.Join(tmpDir, "codero.db")
	obsPort := freePort(t)

	// ── 3. Build environment ─────────────────────────────────────────────────
	// buildTestEnv injects placeholder credentials and
	// CODERO_SKIP_GITHUB_SCOPE_CHECK=true so the daemon starts without a live
	// GitHub API call. The observability server binds to 127.0.0.1:<obsPort>.
	env := buildTestEnv(mr.Addr(), pidFile, dbFile, obsPort)

	// ── 4. `codero status` before daemon — must report not-running ────────────
	t.Run("status_before_start", func(t *testing.T) {
		out, code := runStatus(t, bin, env)
		if code != 0 {
			t.Errorf("expected exit 0 for not-running, got %d", code)
		}
		if !strings.Contains(out, "not running") {
			t.Errorf("expected 'not running' in output, got: %q", out)
		}
	})

	// ── 5. Start daemon ───────────────────────────────────────────────────────
	daemonCmd := exec.Command(bin, "daemon") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	daemonCmd.Env = env
	// Capture daemon logs for failure diagnostics.
	daemonOut := &strings.Builder{}
	daemonCmd.Stdout = daemonOut
	daemonCmd.Stderr = daemonOut

	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("daemon start: %v", err)
	}
	// Always reap the child, even on early test failure.
	t.Cleanup(func() {
		if daemonCmd.Process != nil {
			_ = daemonCmd.Process.Signal(syscall.SIGKILL)
			_ = daemonCmd.Wait()
		}
	})

	// ── 6. Wait for PID file ─────────────────────────────────────────────────
	pidAppeared := pollUntil(t, startupTimeout, func() bool {
		_, err := os.Stat(pidFile)
		return err == nil
	})
	if !pidAppeared {
		t.Fatalf("PID file never appeared after %s\ndaemon output:\n%s", startupTimeout, daemonOut)
	}

	// ── 7. `codero status` while running — must report running + redis ok ─────
	t.Run("status_while_running", func(t *testing.T) {
		statusOK := pollUntil(t, startupTimeout, func() bool {
			out, code := runStatus(t, bin, env)
			return code == 0 &&
				strings.Contains(out, "running") &&
				strings.Contains(out, "redis: ok")
		})
		if !statusOK {
			out, code := runStatus(t, bin, env)
			t.Errorf("status never became running+redis:ok (last exit %d, output: %q)\ndaemon:\n%s",
				code, out, daemonOut)
		}
	})

	// ── 8. Verify miniredis received at least one Redis command ──────────────
	// The daemon health check and Lua script loading must each issue commands.
	// CommandCount() is the total across all commands received by miniredis.
	t.Run("redis_commands_received", func(t *testing.T) {
		if n := mr.CommandCount(); n == 0 {
			t.Error("expected daemon to issue at least one Redis command, none recorded")
		}
	})

	// ── 9. Verify DB file created and migrated ────────────────────────────────
	t.Run("db_initialized", func(t *testing.T) {
		info, err := os.Stat(dbFile)
		if err != nil {
			t.Fatalf("db file not created: %v", err)
		}
		if info.Size() == 0 {
			t.Error("db file is empty — migrations did not run")
		}
	})

	// ── 10. Observability /health ─────────────────────────────────────────────
	// The HTTP server may take a moment to bind after the PID file appears.
	// Poll with the same bounded timeout to stay flake-free.
	// Expected: HTTP 200 with JSON body containing "status":"ok".
	t.Run("observability_health", func(t *testing.T) {
		healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", obsPort)
		healthOK := pollUntil(t, startupTimeout, func() bool {
			resp, err := http.Get(healthURL) //nolint:noctx
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			_, _ = io.ReadAll(resp.Body)
			return resp.StatusCode == http.StatusOK
		})
		if !healthOK {
			t.Errorf("GET /health: want HTTP 200 within %s, never received\ndaemon output:\n%s",
				startupTimeout, daemonOut)
		}
	})

	// ── 11. Observability /ready ──────────────────────────────────────────────
	// /ready is the Kubernetes readiness probe surface; must return 200 when
	// Redis is reachable. miniredis is always reachable in this test.
	t.Run("observability_ready", func(t *testing.T) {
		readyURL := fmt.Sprintf("http://127.0.0.1:%d/ready", obsPort)
		resp, err := http.Get(readyURL) //nolint:noctx
		if err != nil {
			t.Fatalf("GET /ready: %v\ndaemon output:\n%s", err, daemonOut)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /ready: want 200, got %d (body: %q)\ndaemon output:\n%s",
				resp.StatusCode, body, daemonOut)
		}
	})

	// ── 12. Observability /gate ───────────────────────────────────────────────
	// /gate must remain available and return HTTP 200 while daemon is running.
	t.Run("observability_gate", func(t *testing.T) {
		gateURL := fmt.Sprintf("http://127.0.0.1:%d/gate", obsPort)
		gateOK := pollUntil(t, startupTimeout, func() bool {
			resp, err := http.Get(gateURL) //nolint:noctx
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			_, _ = io.ReadAll(resp.Body)
			return resp.StatusCode == http.StatusOK
		})
		if !gateOK {
			t.Errorf("GET /gate: want HTTP 200 within %s\ndaemon output:\n%s",
				startupTimeout, daemonOut)
		}
	})

	// ── 13. SIGTERM — clean shutdown ──────────────────────────────────────────
	if err := daemonCmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- daemonCmd.Wait() }()

	select {
	case waitErr := <-done:
		if waitErr != nil {
			exitErr, ok := waitErr.(*exec.ExitError)
			if !ok {
				t.Fatalf("daemon wait: %v", waitErr)
			}
			if exitErr.ExitCode() != 0 && !strings.Contains(waitErr.Error(), "signal") {
				t.Errorf("daemon exited non-zero after SIGTERM: %v\noutput:\n%s",
					waitErr, daemonOut)
			}
		}
	case <-time.After(shutdownTimeout):
		t.Fatalf("daemon did not exit within %s after SIGTERM\noutput:\n%s",
			shutdownTimeout, daemonOut)
	}

	// ── 14. PID file must be removed on clean shutdown ────────────────────────
	t.Run("pid_file_removed", func(t *testing.T) {
		if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
			t.Errorf("PID file still present after clean shutdown: %v", err)
		}
	})

	// ── 15. `codero status` after shutdown — must report not-running again ────
	t.Run("status_after_shutdown", func(t *testing.T) {
		out, code := runStatus(t, bin, env)
		if code != 0 {
			t.Errorf("expected exit 0, got %d", code)
		}
		if !strings.Contains(out, "not running") {
			t.Errorf("expected 'not running' after shutdown, got: %q", out)
		}
	})
}

// TestDaemonRestart_E2E verifies the graceful stop → clean restart cycle.
// After a SIGTERM-induced shutdown (PID file removed), a second daemon start
// must succeed and reach the ready state without leftover state from the first
// run. This exercises the PID file ownership handoff and DB re-open path.
func TestDaemonRestart_E2E(t *testing.T) {
	bin := buildBinary(t)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "codero.pid")
	dbFile := filepath.Join(tmpDir, "codero.db")

	// startDaemon launches a new daemon process with env and log buffer.
	startDaemon := func(t *testing.T, env []string, logBuf *strings.Builder) *exec.Cmd {
		t.Helper()
		cmd := exec.Command(bin, "daemon") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		cmd.Env = env
		cmd.Stdout = logBuf
		cmd.Stderr = logBuf
		if err := cmd.Start(); err != nil {
			t.Fatalf("daemon start: %v", err)
		}
		return cmd
	}

	// stopDaemon sends SIGTERM and waits for clean exit within shutdownTimeout.
	stopDaemon := func(t *testing.T, cmd *exec.Cmd, logBuf *strings.Builder) {
		t.Helper()
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("SIGTERM: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case waitErr := <-done:
			if waitErr != nil {
				if exitErr, ok := waitErr.(*exec.ExitError); ok {
					if exitErr.ExitCode() != 0 && !strings.Contains(waitErr.Error(), "signal") {
						t.Errorf("daemon exited non-zero after SIGTERM: %v\noutput:\n%s",
							waitErr, logBuf)
					}
				} else {
					t.Fatalf("daemon wait: %v", waitErr)
				}
			}
		case <-time.After(shutdownTimeout):
			t.Fatalf("daemon did not exit within %s after SIGTERM\noutput:\n%s",
				shutdownTimeout, logBuf)
		}
	}

	// ── First run ─────────────────────────────────────────────────────────────
	obsPort1 := freePort(t)
	env1 := buildTestEnv(mr.Addr(), pidFile, dbFile, obsPort1)
	log1 := &strings.Builder{}
	daemon1 := startDaemon(t, env1, log1)
	t.Cleanup(func() {
		if daemon1.Process != nil {
			_ = daemon1.Process.Signal(syscall.SIGKILL)
			_ = daemon1.Wait()
		}
	})

	if !pollUntil(t, startupTimeout, func() bool {
		_, err := os.Stat(pidFile)
		return err == nil
	}) {
		t.Fatalf("run1: PID file never appeared after %s\noutput:\n%s", startupTimeout, log1)
	}

	t.Run("run1_ready", func(t *testing.T) {
		ok := pollUntil(t, startupTimeout, func() bool {
			out, code := runStatus(t, bin, env1)
			return code == 0 &&
				strings.Contains(out, "running") &&
				strings.Contains(out, "redis: ok")
		})
		if !ok {
			out, code := runStatus(t, bin, env1)
			t.Errorf("run1: status never became running+redis:ok (exit %d, output: %q)\n%s",
				code, out, log1)
		}
	})

	stopDaemon(t, daemon1, log1)
	daemon1.Process = nil // disarm SIGKILL cleanup

	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("run1: PID file not removed after clean shutdown")
	}

	// ── Second run ────────────────────────────────────────────────────────────
	obsPort2 := freePort(t)
	env2 := buildTestEnv(mr.Addr(), pidFile, dbFile, obsPort2)
	log2 := &strings.Builder{}
	daemon2 := startDaemon(t, env2, log2)
	t.Cleanup(func() {
		if daemon2.Process != nil {
			_ = daemon2.Process.Signal(syscall.SIGKILL)
			_ = daemon2.Wait()
		}
	})

	if !pollUntil(t, startupTimeout, func() bool {
		_, err := os.Stat(pidFile)
		return err == nil
	}) {
		t.Fatalf("run2: PID file never appeared after %s\noutput:\n%s", startupTimeout, log2)
	}

	t.Run("run2_ready", func(t *testing.T) {
		ok := pollUntil(t, startupTimeout, func() bool {
			out, code := runStatus(t, bin, env2)
			return code == 0 &&
				strings.Contains(out, "running") &&
				strings.Contains(out, "redis: ok")
		})
		if !ok {
			out, code := runStatus(t, bin, env2)
			t.Errorf("run2: status never became running+redis:ok (exit %d, output: %q)\n%s",
				code, out, log2)
		}
	})

	stopDaemon(t, daemon2, log2)
	daemon2.Process = nil // disarm SIGKILL cleanup

	t.Run("run2_pid_removed", func(t *testing.T) {
		if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
			t.Errorf("run2: PID file still present after clean shutdown: %v", err)
		}
	})
}
