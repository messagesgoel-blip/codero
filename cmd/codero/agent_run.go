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
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/codero/codero/internal/config"
	daemongrpc "github.com/codero/codero/internal/daemon/grpc"
	"github.com/codero/codero/internal/session"
	"github.com/creack/pty"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// tailDir returns the directory used for per-session output tail files.
// Override via CODERO_TAIL_DIR env var; defaults to codero-tails under os.TempDir().
func tailDir() string {
	if d := os.Getenv("CODERO_TAIL_DIR"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "codero-tails")
}

// tailPath returns the tail file path for a session.
func tailPath(sessionID string) string {
	return filepath.Join(tailDir(), sessionID+".log")
}

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
// If tail is non-nil, output is also tee'd to the tail file (capped at tailMaxBytes).
// ANSI escape sequences are stripped before writing to the tail file so that
// `codero tail` produces readable plain text.
type activityWriter struct {
	inner   io.Writer
	tracker *activityTracker
	tail    *os.File
	written int64
}

const tailMaxBytes = 4 * 1024 * 1024 // 4 MB cap per session tail file

// ansiStripper matches ANSI/VT100 escape sequences:
//   - CSI sequences:  ESC [ <params> <final>
//   - OSC sequences:  ESC ] <data> BEL  or  ESC ] <data> ST
//   - Two-byte seqs:  ESC <single char>
//   - Carriage return: \r (cursor-to-column-0 used by TUIs)
var ansiStripper = regexp.MustCompile(
	`\x1b\[[0-9;?]*[A-Za-z@]` + // CSI
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC
		`|\x1b[^[\]]` + // 2-byte (ESC x)
		`|\r`,
)

func stripANSI(b []byte) []byte {
	return ansiStripper.ReplaceAll(b, nil)
}

func (w *activityWriter) Write(p []byte) (int, error) {
	w.tracker.touch()
	if w.tail != nil && w.written < tailMaxBytes {
		plain := stripANSI(p)
		remaining := tailMaxBytes - w.written
		chunk := plain
		if int64(len(chunk)) > remaining {
			chunk = plain[:remaining]
		}
		n, _ := w.tail.Write(chunk)
		w.written += int64(n)
	}
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
		taskID  string
		repo    string
	)

	cmd := &cobra.Command{
		Use:   "run [--agent-id name] [--mode mode] [--task-id id] [--repo owner/repo] -- /path/to/binary [args...]",
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

			// CODERO_TRACKING_<AGENT>=0 disables tracking for one agent,
			// CODERO_TRACKING=0 disables tracking for all agents.
			if trackingDisabledFor(agentID) {
				return execBinary(binaryPath, binaryArgs)
			}

			daemonAddr := resolveDaemonAddr(cmd)
			if daemonAddr == "" {
				// No daemon — just exec the binary directly
				return execBinary(binaryPath, binaryArgs)
			}

			err := runAgentWithTracking(cmd.Context(), agentID, mode, taskID, repo, daemonAddr, binaryPath, binaryArgs)
			var exitErr exitCodeError
			if errors.As(err, &exitErr) {
				os.Exit(exitErr.code)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "agent identifier (defaults to binary name)")
	cmd.Flags().StringVar(&mode, "mode", "coding", "session mode label")
	cmd.Flags().StringVar(&taskID, "task-id", "", "task identifier to associate with this session (e.g. COD-123)")
	cmd.Flags().StringVar(&repo, "repo", "", "repository override in owner/repo format (defaults to git remote detection)")

	return cmd
}

// runAgentWithTracking registers, heartbeats, runs the child, and finalizes.
func runAgentWithTracking(ctx context.Context, agentID, mode, taskID, repoOverride, daemonAddr, binaryPath string, binaryArgs []string) error {
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
	if taskID != "" {
		initialContext["task_id"] = taskID
	}
	if repoOverride != "" {
		parts := strings.SplitN(repoOverride, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("--repo must be in owner/repo format, got %q", repoOverride)
		}
		initialContext["repo"] = repoOverride
	}

	result, err := client.RegisterWithContext(ctx, agentID, mode, initialContext)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codero: registration failed, running without tracking: %v\n", err)
		return execBinary(binaryPath, binaryArgs)
	}

	sessionID := result.SessionID
	secret := result.HeartbeatSecret

	// Open per-session tail file for output capture.
	// 0o700/0o600: agent output may contain secrets; restrict to owner only.
	var tailFile *os.File
	if err := os.MkdirAll(tailDir(), 0o700); err == nil {
		if f, err := os.OpenFile(tailPath(sessionID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
			tailFile = f
		}
	}
	if tailFile != nil {
		defer tailFile.Close()
	}

	// Activity tracker — heartbeat marks progress only when the child produces output.
	tracker := newActivityTracker()

	// Background heartbeat
	hbCtx, hbCancel := context.WithCancel(context.Background())
	defer hbCancel()
	go heartbeatLoop(hbCtx, client, sessionID, secret, tracker)

	// Run child process
	exitCode := runChild(binaryPath, binaryArgs, sessionID, agentID, daemonAddr, tracker, tailFile)

	hbCancel()
	status := "completed"
	substatus := "terminal_finished"
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
// When stdout is a real TTY (e.g. inside tmux), the child is started inside a PTY so that
// isatty() checks in the child return true and TUI agents render correctly.
// Activity is tracked by reading the PTY master output stream.
func runChild(binaryPath string, args []string, sessionID, agentID, daemonAddr string, tracker *activityTracker, tailFile *os.File) int {
	child := exec.Command(binaryPath, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	child.Env = append(os.Environ(),
		"CODERO_SESSION_ID="+sessionID,
		"CODERO_AGENT_ID="+agentID,
		"CODERO_DAEMON_ADDR="+daemonAddr,
	)

	stdoutIsTTY := term.IsTerminal(int(os.Stdout.Fd()))

	if stdoutIsTTY {
		return runChildPTY(child, tracker, tailFile)
	}
	return runChildPiped(child, tracker, tailFile)
}

// runChildPTY starts the child inside a pseudo-terminal so TUI agents see a real TTY.
// It copies PTY master → os.Stdout while tracking write activity.
func runChildPTY(child *exec.Cmd, tracker *activityTracker, tailFile *os.File) int {
	ptmx, err := pty.Start(child)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codero: pty start failed: %v\n", err)
		return 1
	}
	defer ptmx.Close()

	// Resize PTY to match the outer terminal.
	if sz, err := pty.GetsizeFull(os.Stdout); err == nil {
		_ = pty.Setsize(ptmx, sz)
	}

	// Forward SIGWINCH so the child gets window-resize events.
	winchCh := make(chan os.Signal, 1)
	signal.Notify(winchCh, syscall.SIGWINCH)
	go func() {
		for range winchCh {
			if sz, err := pty.GetsizeFull(os.Stdout); err == nil {
				_ = pty.Setsize(ptmx, sz)
			}
		}
	}()

	// Forward INT/TERM to child process group.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			if child.Process != nil {
				_ = child.Process.Signal(sig)
			}
		}
	}()

	// Put the outer terminal into raw mode so keystrokes pass through unmodified.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err == nil {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// stdin → PTY master (user keystrokes reach the child).
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()

	// PTY master → stdout with activity tracking (also tee'd to tail file).
	_, _ = io.Copy(&activityWriter{inner: os.Stdout, tracker: tracker, tail: tailFile}, ptmx)

	signal.Stop(sigCh)
	close(sigCh)
	signal.Stop(winchCh)
	close(winchCh)

	if err := child.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		// PTY EOF before Wait is normal; ignore.
	}
	return 0
}

// runChildPiped starts the child with plain pipes (non-TTY fallback).
func runChildPiped(child *exec.Cmd, tracker *activityTracker, tailFile *os.File) int {
	child.Stdin = os.Stdin
	child.Stdout = &activityWriter{inner: os.Stdout, tracker: tracker, tail: tailFile}
	child.Stderr = &activityWriter{inner: os.Stderr, tracker: tracker}

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
		fmt.Fprintf(os.Stderr, "codero: failed to run child: %v\n", err)
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

// trackingDisabledFor returns true when tracking is disabled for a specific agent.
// Priority: env CODERO_TRACKING_<AGENT> > env CODERO_TRACKING > config disabled_agents.
func trackingDisabledFor(agentID string) bool {
	// Per-agent env override (highest priority).
	key := "CODERO_TRACKING_" + strings.ToUpper(strings.ReplaceAll(agentID, "-", "_"))
	if envFalsy(os.Getenv(key)) {
		return true
	}
	// Global env override.
	if envFalsy(os.Getenv("CODERO_TRACKING")) {
		return true
	}
	// Config file disabled_agents list.
	if uc, err := config.LoadUserConfig(); err == nil {
		return uc.IsTrackingDisabled(agentID)
	}
	return false
}

func envFalsy(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "0" || v == "false" || v == "off"
}
