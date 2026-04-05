// Package-level note: CODERO_TRACKING=0|false|off disables all session tracking.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"sync"
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
// Deprecated: use session.TailDir
func tailDir() string {
	return session.TailDir()
}

// tailPath returns the tail file path for a session.
// Deprecated: use session.TailPath
func tailPath(sessionID string) string {
	p, err := session.TailPath(sessionID)
	if err != nil {
		return ""
	}
	return p
}

func hookScratchKey(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:16])
}

func hookScratchDir(sessionID string) string {
	return filepath.Join(os.TempDir(), "codero-"+hookScratchKey(sessionID))
}

func seedHookScratchState(sessionID, secret string) error {
	dir := hookScratchDir(sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ensure hook scratch dir: %w", err)
	}
	rollback := func(prefix string, err error) error {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			return fmt.Errorf("%s: %w (cleanup: %v)", prefix, err, cleanupErr)
		}
		return fmt.Errorf("%s: %w", prefix, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return rollback("chmod hook scratch dir", err)
	}
	for _, entry := range []struct {
		name  string
		value string
	}{
		{name: "session-id", value: sessionID},
		{name: "secret", value: secret},
	} {
		name := entry.name
		value := entry.value
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0o600); err != nil {
			return rollback(fmt.Sprintf("write hook scratch %s", name), err)
		}
	}
	return nil
}

type activitySnapshot struct {
	RuntimeBytes int64
	OutputBytes  int64
	OutputLines  int64
	FileWrites   int64
	DiffChanges  int64
	ProcEvents   int64
}

func (s activitySnapshot) isZero() bool {
	return s.RuntimeBytes <= 0 &&
		s.OutputBytes <= 0 &&
		s.OutputLines <= 0 &&
		s.FileWrites <= 0 &&
		s.DiffChanges <= 0 &&
		s.ProcEvents <= 0
}

// activityTracker records the last time the child process wrote to stdout/stderr
// plus cumulative runtime telemetry used for dashboard activity scoring.
type activityTracker struct {
	lastActivity atomic.Int64 // unix timestamp of last I/O
	runtimeBytes atomic.Int64
	outputBytes  atomic.Int64
	outputLines  atomic.Int64
	procEvents   atomic.Int64
	worktree     *worktreeActivityTracker
}

func newActivityTracker(worktree string) *activityTracker {
	t := &activityTracker{worktree: newWorktreeActivityTracker(worktree)}
	t.lastActivity.Store(time.Now().Unix())
	if t.worktree != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = t.worktree.snapshot(ctx)
		cancel()
	}
	return t
}

// touch records output activity at the current time.
func (t *activityTracker) touch() { t.lastActivity.Store(time.Now().Unix()) }

func (t *activityTracker) recordOutput(p []byte) {
	if len(p) == 0 {
		t.touch()
		return
	}
	t.lastActivity.Store(time.Now().Unix())
	t.runtimeBytes.Add(int64(len(p)))
	t.outputBytes.Add(int64(len(p)))
	t.outputLines.Add(int64(bytes.Count(p, []byte{'\n'})))
}

func (t *activityTracker) recordProcEvent() {
	t.procEvents.Add(1)
}

// hasRecentActivity returns true if output was seen within the given window.
func (t *activityTracker) hasRecentActivity(window time.Duration) bool {
	last := time.Unix(t.lastActivity.Load(), 0)
	return time.Since(last) < window
}

func (t *activityTracker) snapshot(ctx context.Context) activitySnapshot {
	snap := activitySnapshot{
		RuntimeBytes: t.runtimeBytes.Load(),
		OutputBytes:  t.outputBytes.Load(),
		OutputLines:  t.outputLines.Load(),
		ProcEvents:   t.procEvents.Load(),
	}
	if t.worktree != nil {
		snap.FileWrites, snap.DiffChanges = t.worktree.snapshot(ctx)
	}
	return snap
}

type worktreeActivityTracker struct {
	root         string
	mu           sync.Mutex
	initialized  bool
	lastStatuses map[string]string
	lastDiff     int64
	fileWrites   int64
	diffChanges  int64
}

func newWorktreeActivityTracker(worktree string) *worktreeActivityTracker {
	root := detectGitRoot(worktree)
	if root == "" {
		return nil
	}
	return &worktreeActivityTracker{root: root}
}

func gitProcessEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "GIT_") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func gitCommand(root string, args ...string) *exec.Cmd {
	cmdArgs := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Env = gitProcessEnv()
	return cmd
}

func gitCommandContext(ctx context.Context, root string, args ...string) *exec.Cmd {
	cmdArgs := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	cmd.Env = gitProcessEnv()
	return cmd
}

func detectGitRoot(worktree string) string {
	if worktree == "" {
		return ""
	}
	out, err := gitCommand(worktree, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (t *worktreeActivityTracker) snapshot(ctx context.Context) (int64, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	statuses, diffSize, err := collectGitActivitySnapshot(ctx, t.root)
	if err != nil {
		return t.fileWrites, t.diffChanges
	}

	if !t.initialized {
		t.initialized = true
		t.lastStatuses = statuses
		t.lastDiff = diffSize
		return t.fileWrites, t.diffChanges
	}

	t.fileWrites += statusDeltaCount(t.lastStatuses, statuses)
	delta := diffSize - t.lastDiff
	if delta < 0 {
		delta = -delta
	}
	t.diffChanges += delta
	t.lastStatuses = statuses
	t.lastDiff = diffSize
	return t.fileWrites, t.diffChanges
}

func collectGitActivitySnapshot(ctx context.Context, root string) (map[string]string, int64, error) {
	if root == "" {
		return nil, 0, fmt.Errorf("git root is required")
	}

	statusCmd := gitCommandContext(ctx, root, "status", "--porcelain=v1", "--untracked-files=all")
	statusOut, err := statusCmd.Output()
	if err != nil {
		return nil, 0, err
	}
	statuses := parseGitStatusSnapshot(statusOut)

	return statuses, collectGitDiffVolume(ctx, root, statuses), nil
}

func collectGitDiffVolume(ctx context.Context, root string, statuses map[string]string) int64 {
	diffOut, err := gitDiffNumstat(ctx, root, "HEAD", "--numstat", "--")
	if err == nil {
		return parseGitDiffVolume(diffOut) + estimateUntrackedDiffVolume(root, statuses)
	}

	// Repos without a HEAD still need best-effort activity from staged, unstaged,
	// and untracked files instead of flattening to zero.
	var total int64
	for _, args := range [][]string{
		{"--cached", "--numstat", "--root", "--"},
		{"--numstat", "--"},
	} {
		out, diffErr := gitDiffNumstat(ctx, root, args...)
		if diffErr == nil {
			total += parseGitDiffVolume(out)
		}
	}
	return total + estimateUntrackedDiffVolume(root, statuses)
}

func gitDiffNumstat(ctx context.Context, root string, args ...string) ([]byte, error) {
	return gitCommandContext(ctx, root, append([]string{"diff"}, args...)...).Output()
}

func estimateUntrackedDiffVolume(root string, statuses map[string]string) int64 {
	var total int64
	for path, status := range statuses {
		if status != "??" {
			continue
		}
		total += estimateFileDiffVolume(filepath.Join(root, path))
	}
	return total
}

const (
	maxEstimatedDiffBytes = 256 * 1024
	maxEstimatedDiffLines = 10_000
)

func estimateFileDiffVolume(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 1
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return 1
	}

	file, err := os.Open(path)
	if err != nil {
		return 1
	}
	defer file.Close()

	reader := io.LimitReader(file, maxEstimatedDiffBytes)
	buf := make([]byte, 32*1024)
	var (
		lines   int64
		last    byte
		sawData bool
	)

	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			sawData = true
			if bytes.IndexByte(chunk, 0) >= 0 {
				return 1
			}
			lines += int64(bytes.Count(chunk, []byte{'\n'}))
			if lines >= maxEstimatedDiffLines {
				return maxEstimatedDiffLines
			}
			last = chunk[n-1]
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 1
		}
	}

	if !sawData {
		return 1
	}
	if last != '\n' {
		lines++
	}
	if lines <= 0 {
		return 1
	}
	if lines > maxEstimatedDiffLines {
		return maxEstimatedDiffLines
	}
	return lines
}

func parseGitStatusSnapshot(out []byte) map[string]string {
	statuses := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		status := line[:2]
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		statuses[path] = status
	}
	return statuses
}

func parseGitDiffVolume(out []byte) int64 {
	var totalLines int64
	var fileCount int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		binaryChange := false
		for _, part := range parts[:2] {
			if part == "-" {
				binaryChange = true
				continue
			}
			n, err := strconv.ParseInt(part, 10, 64)
			if err == nil && n > 0 {
				totalLines += n
			}
		}
		if binaryChange {
			fileCount++
		}
	}
	return totalLines + fileCount
}

func statusDeltaCount(prev, next map[string]string) int64 {
	var count int64
	seen := make(map[string]struct{}, len(prev)+len(next))
	for path, status := range prev {
		seen[path] = struct{}{}
		if next[path] != status {
			count++
		}
	}
	for path := range next {
		if _, ok := seen[path]; ok {
			continue
		}
		count++
	}
	return count
}

// activityWriter wraps an io.Writer and touches the tracker on every write.
// If tail is non-nil, output is also tee'd to the tail file (capped at tailMaxBytes).
// ANSI escape sequences are stripped before writing to the tail file so that
// `codero tail` produces readable plain text.
type activityWriter struct {
	inner     io.Writer
	tracker   *activityTracker
	tail      *os.File
	written   int64
	ansiCarry []byte // partial escape fragment carried over from previous Write
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

// splitTrailingANSIFragment splits b into a stable prefix safe to strip and a
// carry suffix that may be an incomplete ANSI escape started near the end. The
// carry is prepended to the next Write call so split sequences are stripped
// correctly instead of leaking raw bytes into the tail file.
func splitTrailingANSIFragment(b []byte) (stable, carry []byte) {
	i := bytes.LastIndexByte(b, 0x1b)
	if i < 0 {
		return b, nil
	}
	tail := b[i:]
	// If the trailing ESC and whatever follows is already a complete token,
	// there is nothing to defer.
	if ansiStripper.Match(tail) {
		return b, nil
	}
	return b[:i], append([]byte(nil), tail...)
}

func (w *activityWriter) Write(p []byte) (int, error) {
	w.tracker.recordOutput(p)
	if w.tail != nil && w.written < tailMaxBytes {
		joined := append(append([]byte(nil), w.ansiCarry...), p...)
		stable, carry := splitTrailingANSIFragment(joined)
		w.ansiCarry = carry
		plain := stripANSI(stable)
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
				return execBinary(binaryPath, binaryArgs, agentID)
			}

			daemonAddr := resolveDaemonAddr(cmd)
			if daemonAddr == "" {
				// No daemon — just exec the binary directly
				return execBinary(binaryPath, binaryArgs, agentID)
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
		return execBinary(binaryPath, binaryArgs, agentID)
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
		return execBinary(binaryPath, binaryArgs, agentID)
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
	if err := seedHookScratchState(sessionID, secret); err != nil {
		fmt.Fprintf(os.Stderr, "codero: hook scratch seed failed: %v\n", err)
	} else {
		scratchDir := hookScratchDir(sessionID)
		defer func() {
			if err := os.RemoveAll(scratchDir); err != nil {
				fmt.Fprintf(os.Stderr, "codero: hook scratch cleanup failed: %v\n", err)
			}
		}()
	}

	// Activity tracker — heartbeat marks progress only when the child produces output.
	tracker := newActivityTracker(resolveFallbackWorktree())

	// Background heartbeat
	hbCtx, hbCancel := context.WithCancel(context.Background())
	defer hbCancel()
	go heartbeatLoop(hbCtx, client, sessionID, secret, tracker)

	// Run child process
	exitCode := runChild(binaryPath, binaryArgs, sessionID, agentID, daemonAddr, tracker, tailFile)

	flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = sendHeartbeatSample(flushCtx, client, sessionID, secret, tracker)
	flushCancel()
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

type heartbeatClient interface {
	Heartbeat(ctx context.Context, sessionID, heartbeatSecret string, markProgress bool) error
	HeartbeatWithContext(ctx context.Context, sessionID, heartbeatSecret string, markProgress bool, hctx daemongrpc.HeartbeatContext) error
}

func sendHeartbeatSample(ctx context.Context, client heartbeatClient, sessionID, secret string, tracker *activityTracker) error {
	markProgress := tracker.hasRecentActivity(progressWindow)
	sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	snap := tracker.snapshot(sampleCtx)
	cancel()
	if snap.isZero() {
		return client.Heartbeat(ctx, sessionID, secret, markProgress)
	}
	return client.HeartbeatWithContext(ctx, sessionID, secret, markProgress, daemongrpc.HeartbeatContext{
		RuntimeBytes: snap.RuntimeBytes,
		OutputBytes:  snap.OutputBytes,
		OutputLines:  snap.OutputLines,
		FileWrites:   snap.FileWrites,
		DiffChanges:  snap.DiffChanges,
		ProcEvents:   snap.ProcEvents,
	})
}

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
			_ = sendHeartbeatSample(ctx, client, sessionID, secret, tracker)
		}
	}
}

// runChild starts the binary as a child process, forwards signals, and returns the exit code.
// When stdout is a real TTY (e.g. inside tmux), the child is started inside a PTY so that
// isatty() checks in the child return true and TUI agents render correctly.
// Activity is tracked by reading the PTY master output stream.
//
// BND-002: Environment is filtered to prevent secret leakage. Agent processes
// do not receive CODERO_DB_*, CODERO_REDIS_*, GITHUB_TOKEN, or other Codero
// control-plane credentials.
func runChild(binaryPath string, args []string, sessionID, agentID, daemonAddr string, tracker *activityTracker, tailFile *os.File) int {
	child := exec.Command(binaryPath, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command

	// BND-002: Filter env to prevent control-plane secrets from leaking to agent.
	// Uses strict allowlist: only system vars + allowed agent vars pass through.
	env := session.FilterEnv(session.LayerAgent)

	// Add user-configured wrapper env vars if present (also filtered for safety)
	if uc, err := config.LoadUserConfig(); err == nil && uc != nil {
		if w, ok := uc.Wrappers[agentID]; ok && w.EnvVars != nil {
			// BND-002: Filter wrapper env vars to prevent config from re-introducing forbidden vars
			filtered := session.FilterWrapperEnvVars(w.EnvVars, session.LayerAgent)
			for k, v := range filtered {
				env = append(env, k+"="+v)
			}
		}
	}
	child.Env = append(env,
		"CODERO_SESSION_ID="+sessionID,
		"CODERO_AGENT_ID="+agentID,
		"CODERO_DAEMON_ADDR="+daemonAddr,
		"CODERO_HOOK_SCRATCH_DIR="+hookScratchDir(sessionID),
		"CODERO_WORKTREE="+resolveFallbackWorktree(),
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
	tracker.recordProcEvent()
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

	err = child.Wait()
	tracker.recordProcEvent()
	if err != nil {
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

	tracker.recordProcEvent()
	err := child.Run()
	tracker.recordProcEvent()
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
// BND-002: Uses filtered env to prevent secret leakage even in degraded path.
// The resolved agent identity is appended after filtering so fallback launches
// do not lose the current agent label.
func execBinary(binaryPath string, args []string, agentID string) error {
	argv := append([]string{binaryPath}, args...)
	env := buildFallbackEnv(agentID)
	return syscall.Exec(binaryPath, argv, env) // nosemgrep: go.lang.security.audit.dangerous-syscall-exec.dangerous-syscall-exec
}

// buildFallbackEnv filters the current environment for degraded agent execution
// and re-applies the resolved agent identity last so it wins over inherited state.
func buildFallbackEnv(agentID string) []string {
	env := session.FilterEnv(session.LayerAgent)
	if agentID != "" {
		env = append(env, "CODERO_AGENT_ID="+agentID)
	}
	if worktree := resolveFallbackWorktree(); worktree != "" {
		env = append(env, "CODERO_WORKTREE="+worktree)
	}
	return env
}

// resolveFallbackWorktree preserves an existing CODERO_WORKTREE when present.
// If the environment does not already carry one, fall back to the current
// process working directory so degraded launches still have a usable worktree.
func resolveFallbackWorktree() string {
	if worktree := os.Getenv("CODERO_WORKTREE"); worktree != "" {
		return worktree
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		return cwd
	}
	return ""
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
