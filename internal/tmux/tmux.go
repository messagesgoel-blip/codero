// Package tmux manages tmux sessions for Codero agent lifecycle.
//
// The tmux session IS the Codero session (SL-9). Its existence proves
// the agent is alive, and its termination triggers session cleanup.
package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// SessionNamePrefix is prepended to all Codero-managed tmux sessions.
const SessionNamePrefix = "codero-"

// SessionName returns the canonical tmux session name for a given agent
// and session UUID. Format: codero-{agentID}-{uuidShort} (SL-11).
func SessionName(agentID, sessionID string) string {
	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("codero-%s-%s", agentID, short)
}

// ParseSessionName extracts (agentID, uuidShort) from a tmux session name.
// Returns ("", "", false) if the name does not follow the convention.
func ParseSessionName(name string) (agentID, uuidShort string, ok bool) {
	if !strings.HasPrefix(name, SessionNamePrefix) {
		return "", "", false
	}
	rest := name[len(SessionNamePrefix):]
	idx := strings.LastIndex(rest, "-")
	if idx <= 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// HasSession checks whether a tmux session with the given name exists.
// Returns true if tmux has-session exits 0 (SL-10).
func HasSession(ctx context.Context, name string) bool {
	cmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// NewSession creates a detached tmux session with the given name and
// working directory (SL-9, SL-13 step 5).
func NewSession(ctx context.Context, name, workdir string) error {
	args := []string{"new-session", "-d", "-s", name}
	if workdir != "" {
		args = append(args, "-c", workdir)
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	return cmd.Run()
}

// SendKeys sends a command string to the tmux session followed by Enter
// (SL-13 step 10).
func SendKeys(ctx context.Context, name, command string) error {
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", name, command, "Enter")
	return cmd.Run()
}

// KillSession destroys the tmux session (SL-13 step 14).
func KillSession(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", name)
	return cmd.Run()
}

// CapturePane captures the visible pane content of a tmux session
// for optional session log archival (SL-15).
func CapturePane(ctx context.Context, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", name, "-p")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane: %w", err)
	}
	return string(out), nil
}

// ListCoderoSessions returns all active tmux sessions matching the
// codero-* prefix. Each entry is the raw session name.
func ListCoderoSessions(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		// tmux exits non-zero if no server is running
		if strings.Contains(err.Error(), "exit status") {
			return nil, nil
		}
		return nil, fmt.Errorf("list-sessions: %w", err)
	}
	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, SessionNamePrefix) {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// Executor abstracts tmux operations for testing. Production code uses
// the package-level functions; tests can substitute a mock.
type Executor interface {
	HasSession(ctx context.Context, name string) bool
	NewSession(ctx context.Context, name, workdir string) error
	RespawnWindow(ctx context.Context, name string, command []string) error
	SendKeys(ctx context.Context, name, command string) error
	KillSession(ctx context.Context, name string) error
	CapturePane(ctx context.Context, name string) (string, error)
	ListCoderoSessions(ctx context.Context) ([]string, error)
}

// RealExecutor delegates to the real tmux binary.
type RealExecutor struct{}

func (RealExecutor) HasSession(ctx context.Context, name string) bool {
	return HasSession(ctx, name)
}
func (RealExecutor) NewSession(ctx context.Context, name, workdir string) error {
	return NewSession(ctx, name, workdir)
}
func (RealExecutor) RespawnWindow(ctx context.Context, name string, command []string) error {
	args := append([]string{"respawn-window", "-k", "-t", name}, command...)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("respawn-window: %w", err)
	}
	return nil
}
func (RealExecutor) SendKeys(ctx context.Context, name, command string) error {
	return SendKeys(ctx, name, command)
}
func (RealExecutor) KillSession(ctx context.Context, name string) error {
	return KillSession(ctx, name)
}
func (RealExecutor) CapturePane(ctx context.Context, name string) (string, error) {
	return CapturePane(ctx, name)
}
func (RealExecutor) ListCoderoSessions(ctx context.Context) ([]string, error) {
	return ListCoderoSessions(ctx)
}

// MockExecutor records calls and returns configurable results for testing.
type MockExecutor struct {
	Sessions      map[string]bool   // name → alive
	PaneContent   map[string]string // name → captured content
	CreatedNames  []string
	Respawned     []SentRespawn
	KilledNames   []string
	SentKeys      []SentKey
	NewSessionErr error
	RespawnErr    error
	SendKeysErr   error
	KillErr       error
	CaptureErr    error
}

// SentKey records a SendKeys call.
type SentKey struct {
	Name    string
	Command string
}

// SentRespawn records a RespawnWindow call.
type SentRespawn struct {
	Name    string
	Command []string
}

// NewMockExecutor creates a MockExecutor with empty state.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		Sessions:    make(map[string]bool),
		PaneContent: make(map[string]string),
	}
}

func (m *MockExecutor) HasSession(_ context.Context, name string) bool {
	return m.Sessions[name]
}

func (m *MockExecutor) NewSession(_ context.Context, name, _ string) error {
	if m.NewSessionErr != nil {
		return m.NewSessionErr
	}
	m.Sessions[name] = true
	m.CreatedNames = append(m.CreatedNames, name)
	return nil
}

func (m *MockExecutor) RespawnWindow(_ context.Context, name string, command []string) error {
	if m.RespawnErr != nil {
		return m.RespawnErr
	}
	m.Respawned = append(m.Respawned, SentRespawn{Name: name, Command: append([]string{}, command...)})
	return nil
}

func (m *MockExecutor) SendKeys(_ context.Context, name, command string) error {
	if m.SendKeysErr != nil {
		return m.SendKeysErr
	}
	m.SentKeys = append(m.SentKeys, SentKey{Name: name, Command: command})
	return nil
}

func (m *MockExecutor) KillSession(_ context.Context, name string) error {
	if m.KillErr != nil {
		return m.KillErr
	}
	delete(m.Sessions, name)
	m.KilledNames = append(m.KilledNames, name)
	return nil
}

func (m *MockExecutor) CapturePane(_ context.Context, name string) (string, error) {
	if m.CaptureErr != nil {
		return "", m.CaptureErr
	}
	return m.PaneContent[name], nil
}

func (m *MockExecutor) ListCoderoSessions(_ context.Context) ([]string, error) {
	var out []string
	for name, alive := range m.Sessions {
		if alive && strings.HasPrefix(name, SessionNamePrefix) {
			out = append(out, name)
		}
	}
	return out, nil
}
