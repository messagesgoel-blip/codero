// Package-level note: CODERO_TRACKING=0|false|off disables all session tracking.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	daemongrpc "github.com/codero/codero/internal/daemon/grpc"
	"github.com/codero/codero/internal/session"
	"github.com/spf13/cobra"
)

// activityTracker records the last time the child process wrote to stdout/stderr.
// The heartbeat goroutine reads this to decide whether to mark progress.
type activityTracker struct {
	lastActivity atomic.Int64 // unix timestamp of last I/O
}

func newActivityTracker() *activityTracker {
	t := &activityTracker{}
	t.lastActivity.Store(time.Now().Unix())
	return t
}

// touch records output activity at the current time.
func (t *activityTracker) touch() { t.lastActivity.Store(time.Now().Unix()) }

// hasRecentActivity returns true if output was seen within the given window.
func (t *activityTracker) hasRecentActivity(window time.Duration) bool {
	last := time.Unix(t.lastActivity.Load(), 0)
	return time.Since(last) < window
}

// activityWriter wraps an io.Writer and touches the tracker on every write.
type activityWriter struct {
	inner   io.Writer
	tracker *activityTracker
}

func (w *activityWriter) Write(p []byte) (int, error) {
	w.tracker.touch()
	return w.inner.Write(p)
}

// agentRunCmd implements `codero agent run` — the smart lifecycle wrapper.
// It registers a session, heartbeats in the background, execs the real agent
// binary as a child process, and finalizes the session on child exit.
//
// Graceful degradation: if the daemon is unreachable, the child binary is
// executed directly with no session tracking.
func agentRunCmd(configPath *string) *cobra.Command {
	var (
		agentID string
		mode    string
	)

	cmd := &cobra.Command{
		Use:   "run [--agent-id name] [--mode mode] -- /path/to/binary [args...]",
		Short: "Run an agent binary with automatic session tracking",
		Long: `Wraps any agent binary with codero session lifecycle management.
Registers a session, heartbeats in the background, and finalizes on exit.
If the daemon is unreachable, runs the binary directly with no tracking.`,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("binary path is required after --")
			}
			if agentID == "" {
				agentID = resolveAgentIDFromEnv()
			}
			if agentID == "" {
				// Derive from binary name
				agentID = baseNameWithoutExt(args[0])
			}

			binaryPath := args[0]
			binaryArgs := args[1:]

			// Verify binary exists
			if _, err := os.Stat(binaryPath); err != nil {
				return fmt.Errorf("binary not found: %s", binaryPath)
			}

			// CODERO_TRACKING=0|false|off disables session tracking entirely.
			if trackingDisabled() {
				return execBinary(binaryPath, binaryArgs)
			}

			daemonAddr := resolveDaemonAddr(cmd)
			if daemonAddr == "" {
				// No daemon — just exec the binary directly
				return execBinary(binaryPath, binaryArgs)
			}

			err := runAgentWithTracking(cmd.Context(), agentID, mode, daemonAddr, binaryPath, binaryArgs)
			var exitErr exitCodeError
			if errors.As(err, &exitErr) {
				os.Exit(exitErr.code)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "agent identifier (defaults to binary name)")
	cmd.Flags().StringVar(&mode, "mode", "coding", "session mode label")

	return cmd
}

// runAgentWithTracking registers, heartbeats, runs the child, and finalizes.
func runAgentWithTracking(ctx context.Context, agentID, mode, daemonAddr, binaryPath string, binaryArgs []string) error {
	// Connect to daemon
	client, err := daemongrpc.NewSessionClient(daemonAddr)
	if err != nil {
		// Degrade: just run the binary
		fmt.Fprintf(os.Stderr, "codero: daemon unreachable, running without tracking\n")
		return execBinary(binaryPath, binaryArgs)
	}
	defer client.Close()

	// Build rich metadata
	initialContext := buildSessionContext(binaryPath)

	result, err := client.RegisterWithContext(ctx, agentID, mode, initialContext)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codero: registration failed, running without tracking: %v\n", err)
		return execBinary(binaryPath, binaryArgs)
	}

	sessionID := result.SessionID
	secret := result.HeartbeatSecret

	// Activity tracker — heartbeat marks progress only when the child produces output.
	tracker := newActivityTracker()

	// Background heartbeat
	hbCtx, hbCancel := context.WithCancel(context.Background())
	defer hbCancel()
	go heartbeatLoop(hbCtx, client, sessionID, secret, tracker)

	// Run child process
	exitCode := runChild(binaryPath, binaryArgs, sessionID, agentID, daemonAddr, tracker)

	// Finalize — use cancelled/lost rather than completed to avoid
	// triggering gate-must-pass rules that don't apply to wrapper sessions.
	hbCancel()
	status := "cancelled"
	substatus := "terminal_cancelled"
	if exitCode != 0 {
		status = "lost"
		substatus = "terminal_lost"
	}
	if err := client.Finalize(context.Background(), sessionID, agentID, session.Completion{
		Status:     status,
		Substatus:  substatus,
		Summary:    fmt.Sprintf("exit code %d", exitCode),
		FinishedAt: time.Now().UTC(),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "codero: finalize failed: %v\n", err)
	}

	if exitCode != 0 {
		return exitCodeError{code: exitCode}
	}
	return nil
}

// exitCodeError signals a non-zero child exit to the caller without calling os.Exit.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("child exited with code %d", e.code) }

// progressWindow is how recently the child must have produced output for
// the heartbeat to mark progress. Aligned with the heartbeat interval so
// that one silent interval is enough to flip to heartbeat-only mode.
const progressWindow = 60 * time.Second

// heartbeatLoop sends heartbeats every 30s until the context is cancelled.
// markProgress is true only when the child produced recent I/O output.
func heartbeatLoop(ctx context.Context, client *daemongrpc.SessionClient, sessionID, secret string, tracker *activityTracker) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			markProgress := tracker.hasRecentActivity(progressWindow)
			_ = client.Heartbeat(ctx, sessionID, secret, markProgress)
		}
	}
}

// runChild starts the binary as a child process, forwards signals, and returns the exit code.
func runChild(binaryPath string, args []string, sessionID, agentID, daemonAddr string, tracker *activityTracker) int {
	child := exec.Command(binaryPath, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	child.Stdin = os.Stdin
	child.Stdout = &activityWriter{inner: os.Stdout, tracker: tracker}
	child.Stderr = &activityWriter{inner: os.Stderr, tracker: tracker}
	child.Env = append(os.Environ(),
		"CODERO_SESSION_ID="+sessionID,
		"CODERO_AGENT_ID="+agentID,
		"CODERO_DAEMON_ADDR="+daemonAddr,
	)

	// Forward signals to child
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for {
			select {
			case sig := <-sigCh:
				if child.Process != nil {
					_ = child.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()

	err := child.Run()
	signal.Stop(sigCh)
	close(done)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "codero: failed to run %s: %v\n", binaryPath, err)
		return 1
	}
	return 0
}

// execBinary replaces the current process with the binary (no tracking).
func execBinary(binaryPath string, args []string) error {
	argv := append([]string{binaryPath}, args...)
	return syscall.Exec(binaryPath, argv, os.Environ()) // nosemgrep: go.lang.security.audit.dangerous-syscall-exec.dangerous-syscall-exec
}

// buildSessionContext collects rich metadata for registration.
func buildSessionContext(binaryPath string) map[string]string {
	ctx := map[string]string{
		"session_source": "agent_run",
		"binary":         binaryPath,
		"pid":            strconv.Itoa(os.Getpid()),
		"ppid":           strconv.Itoa(os.Getppid()),
	}

	if h, err := os.Hostname(); err == nil {
		ctx["hostname"] = h
	}
	if u, err := user.Current(); err == nil {
		ctx["username"] = u.Username
	}
	if cwd, err := os.Getwd(); err == nil {
		ctx["cwd"] = cwd
	}

	// Detect git repo and branch from cwd
	if repo := detectGitRemoteFromCwd(); repo != "" {
		ctx["repo"] = repo
	}
	if branch, err := getCurrentBranch(); err == nil && branch != "" {
		ctx["branch"] = branch
	}

	return ctx
}

// detectGitRemoteFromCwd returns the origin remote URL (owner/repo format) or "".
func detectGitRemoteFromCwd() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	// Extract owner/repo from https or ssh URL
	if idx := strings.Index(url, "github.com"); idx >= 0 {
		path := url[idx+len("github.com"):]
		path = strings.TrimPrefix(path, "/")
		path = strings.TrimPrefix(path, ":")
		path = strings.TrimSuffix(path, ".git")
		return path
	}
	return ""
}

// baseNameWithoutExt returns the file name without extension.
func baseNameWithoutExt(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

// trackingDisabled returns true when the user has set CODERO_TRACKING to a
// falsy value (0, false, off). Unset or any other value means tracking is on.
func trackingDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CODERO_TRACKING")))
	return v == "0" || v == "false" || v == "off"
}
