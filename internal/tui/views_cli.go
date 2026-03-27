package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui/adapters"
)

const (
	maxResponseBodyBytes = 10 << 20
	// header, separator, suggestion row, input row, plus two spare lines
	// so the assistant thread does not crowd the bottom bar edge.
	terminalReservedLines = 6
)

var dashboardChatHTTPClient = &http.Client{Timeout: 45 * time.Second}

type terminalMessage struct {
	Role    string
	Content string
	Meta    string
}

type terminalChatResultMsg struct {
	prompt   string
	response dashboard.ChatResponse
}

type terminalChatErrorMsg struct {
	prompt string
	err    error
}

func dashboardChatEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("CODERO_DASHBOARD_CHAT_URL")); v != "" {
		return v
	}
	if base := strings.TrimSpace(os.Getenv("CODERO_DASHBOARD_URL")); base != "" {
		return strings.TrimRight(base, "/") + "/chat"
	}
	return "http://127.0.0.1:8080/api/v1/dashboard/chat"
}

func dashboardChatCmd(ctx context.Context, prompt, tab string) tea.Cmd {
	return func() tea.Msg {
		if ctx == nil {
			ctx = context.Background()
		}
		reqBody := dashboard.ChatRequest{
			Prompt:  prompt,
			Tab:     tab,
			Context: "Codero TUI review shell",
			Stream:  false,
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("marshal request body: %w", err)}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, dashboardChatEndpoint(), bytes.NewReader(body))
		if err != nil {
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("create HTTP request: %w", err)}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := dashboardChatHTTPClient.Do(req)
		if err != nil {
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("send chat request: %w", err)}
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
		if err != nil {
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("read response body: %w", err)}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(raw))
			if msg == "" {
				msg = resp.Status
			}
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("dashboard chat %s: %w", resp.Status, errors.New(msg))}
		}

		var out dashboard.ChatResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return terminalChatErrorMsg{prompt: prompt, err: fmt.Errorf("unmarshal chat response: %w", err)}
		}
		return terminalChatResultMsg{prompt: prompt, response: out}
	}
}

func renderTerminalCLI(m Model) string {
	l := m.layout
	t := m.theme
	width := l.TotalW
	height := l.BottomBarH
	if width <= 0 || height <= 0 {
		return ""
	}

	lines := make([]string, 0, height)

	// Header with context and message count
	headerLeft := t.PaneHeader.Render("REVIEW ASSISTANT")
	contextLabel := " " + t.Muted.Render("·") + " " + t.Running.Render(m.chatContextTab())
	mergeLabel := " " + t.Muted.Render("·") + " " + mergeStatusLabel(m)
	headerRight := mergeLabel + " " + t.Muted.Render(fmt.Sprintf("· %d messages  ", len(m.cliMessages)))
	spacer := width - lipgloss.Width(headerLeft) - lipgloss.Width(contextLabel) - lipgloss.Width(headerRight)
	if spacer < 1 {
		spacer = 1
	}
	lines = append(lines, headerLeft+contextLabel+strings.Repeat(" ", spacer)+headerRight)

	// Separator line for visual weight
	sepLine := t.Muted.Render(strings.Repeat("─", width))
	lines = append(lines, sepLine)

	threadLines := m.renderTerminalThread(width, height-terminalReservedLines)
	lines = append(lines, threadLines...)

	suggestions := m.cliSuggestions
	if len(suggestions) == 0 {
		suggestions = []dashboard.ChatSuggestion{
			{Label: "status", Prompt: "status"},
			{Label: "help", Prompt: "help"},
			{Label: "run gate", Prompt: "run gate"},
			{Label: "queue", Prompt: "queue"},
		}
	}
	chips := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = strings.TrimSpace(s.Prompt)
		}
		chips = append(chips, commandChip(t, label))
	}
	if len(chips) > 0 {
		lines = append(lines, "  "+strings.Join(chips, " "))
	}

	// Input line with improved prompt
	input := t.Accent.Render(" ❯") + t.Base.Render(" ") + t.PaletteInput.Render(m.cliInput.View())
	inputLine := input
	if m.cliBusy {
		inputLine += " " + t.Warning.Render("● thinking…")
	}
	lines = append(lines, inputLine)

	for len(lines) < height {
		lines = append(lines, "")
	}

	return t.BottomBar.Width(width).Render(strings.Join(lines[:height], "\n"))
}

func renderStatusBar(m Model) string {
	l := m.layout
	t := m.theme
	width := l.TotalW
	height := l.BottomBarH
	if width <= 0 || height <= 0 {
		return ""
	}

	lines := make([]string, 0, height)

	// Line 1: Merge status with severity counts
	mergeLabel := mergeStatusLabel(m)
	sevCounts := ""
	if s := m.checksVM.Summary; s.Failed > 0 {
		sevCounts = t.Fail.Render(fmt.Sprintf(" · %d failed", s.Failed))
	}
	line1 := " " + t.Muted.Render("Merge:") + " " + mergeLabel + sevCounts
	lines = append(lines, lipgloss.NewStyle().Width(width).Render(line1))

	// Line 2: Key hints
	hints := t.Muted.Render(" o overview · s session · p pipeline · a archives · c chat · Tab pane · C-r refresh · q quit")
	lines = append(lines, lipgloss.NewStyle().Width(width).Render(hints))

	// Line 3: Status line
	parts := make([]string, 0, 3)
	if !m.lastUpdated.IsZero() {
		parts = append(parts, fmt.Sprintf("updated %ds ago", int(time.Since(m.lastUpdated).Seconds())))
	}
	parts = append(parts, fmt.Sprintf("interval %s", m.cfg.Interval.Truncate(time.Millisecond)))
	status := t.Muted.Render(" " + strings.Join(parts, " · "))
	lines = append(lines, lipgloss.NewStyle().Width(width).Render(status))

	for len(lines) < height {
		lines = append(lines, "")
	}

	return t.BottomBar.Width(width).Render(strings.Join(lines[:height], "\n"))
}

func commandChip(t Theme, label string) string {
	bg := t.ChipBackground
	if bg == "" {
		bg = lipgloss.Color("#31384A")
	}
	fg := t.ChipForeground
	if fg == "" {
		fg = lipgloss.Color("#A3A6B8")
	}
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Padding(0, 1).
		Render(label)
}

func mergeStatusLabel(m Model) string {
	switch {
	case len(m.blockReasons) > 0:
		top := m.blockReasons[0]
		return m.theme.Fail.Render(fmt.Sprintf("merge blocked: %s (%d)", top.Source, top.Count))
	case m.checksVM.Summary.Failed > 0:
		return m.theme.Warning.Render(fmt.Sprintf("merge blocked: %d failing checks", m.checksVM.Summary.Failed))
	case m.branchRecord != nil && m.branchRecord.State == state.StateMergeReady:
		return m.theme.Pass.Render("merge ready")
	case m.branchRecord != nil:
		return m.theme.Muted.Render("branch: " + string(m.branchRecord.State))
	default:
		return m.theme.Muted.Render("merge status pending")
	}
}

func (m Model) renderTerminalThread(width, height int) []string {
	if height < 1 {
		return nil
	}
	lines := make([]string, 0, height)
	if len(m.cliMessages) == 0 {
		empty := "Ask Codero about the review process: queue, gate checks, findings, active sessions, activity, or merge readiness."
		lines = append(lines, "")
		lines = append(lines, m.theme.Muted.Render("  "+empty))
		for len(lines) < height {
			lines = append(lines, "")
		}
		return lines[:height]
	}

	for i := len(m.cliMessages) - 1; i >= 0 && len(lines) < height; i-- {
		renderedMsg := renderTerminalMsg(m.theme, m.cliMessages[i])
		msgLines := strings.Split(renderedMsg, "\n")
		if len(msgLines) > height {
			msgLines = clipTerminalMessageLines(msgLines, height)
		} else if remaining := height - len(lines); len(msgLines) > remaining {
			msgLines = clipTerminalMessageLines(msgLines, remaining)
		}
		lines = append(msgLines, lines...)
	}
	for len(lines) < height {
		lines = append([]string{""}, lines...)
	}
	return lines
}

func clipTerminalMessageLines(msgLines []string, maxLines int) []string {
	if maxLines <= 0 || len(msgLines) == 0 {
		return nil
	}
	if len(msgLines) <= maxLines {
		return msgLines
	}
	if maxLines == 1 {
		return msgLines[:1]
	}

	header := msgLines[:1]
	tail := msgLines[1:]
	if len(tail) >= maxLines-1 {
		tail = tail[len(tail)-(maxLines-1):]
	}
	return append(header, tail...)
}

func renderTerminalMsg(t Theme, msg terminalMessage) string {
	roleStyle := t.Base
	switch msg.Role {
	case "user":
		roleStyle = t.Accent
	case "assistant":
		roleStyle = t.Pass
	case "error":
		roleStyle = t.Fail
	case "system":
		roleStyle = t.Bold
	}

	role := roleStyle.Render(strings.ToUpper(msg.Role))
	meta := ""
	if strings.TrimSpace(msg.Meta) != "" {
		meta = " " + t.Muted.Render("("+msg.Meta+")")
	}

	prefix := "  " + role + meta + " "
	content := t.Base.Render(msg.Content)

	return prefix + content
}

func (m Model) localTerminalCommand(cmd string) (handled bool, messages []terminalMessage, suggestions []dashboard.ChatSuggestion, actions []dashboard.ChatAction) {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "":
		return true, nil, nil, nil
	case "help":
		return true,
			[]terminalMessage{{Role: "assistant", Meta: "local", Content: strings.Join([]string{
				"status  - show gate and security summary",
				"help    - list commands",
				"gate    - show gate-check summary",
				"queue   - show pipeline/queue information",
				"clear   - clear the thread",
				"anything else is sent to the review assistant backend",
			}, "\n")}},
			nil,
			nil
	case "status":
		status := fmt.Sprintf("Gate: %s  |  Current gate: %s  |  Elapsed: %s",
			m.gateVM.StatusLabel,
			emptyFallback(m.gateVM.CurrentGate, "pending"),
			adapters.ElapsedLabel(m.gateVM.ElapsedSec),
		)
		security := ""
		if s := m.checksPane.vm.Summary; s.Total > 0 {
			security = fmt.Sprintf("Findings: %d failed, %d passed, %d total", s.Failed, s.Passed, s.Total)
		}
		text := status
		if security != "" {
			text += "\n" + security
		}
		return true, []terminalMessage{{Role: "assistant", Meta: "local", Content: text}}, nil, nil
	case "gate":
		s := m.checksPane.vm.Summary
		text := fmt.Sprintf("Overall: %s\nPassed: %d  Failed: %d  Skipped: %d  Disabled: %d",
			emptyFallback(s.Overall, "pending"), s.Passed, s.Failed, s.Skipped, s.Disabled)
		if s.RequiredFailed > 0 {
			text += fmt.Sprintf("\nRequired failed: %d", s.RequiredFailed)
		}
		if s.RequiredDisabled > 0 {
			text += fmt.Sprintf("\nRequired disabled: %d", s.RequiredDisabled)
		}
		return true, []terminalMessage{{Role: "assistant", Meta: "local", Content: text}}, nil, nil
	case "queue":
		if len(m.gateVM.PipelineRows) == 0 {
			return true, []terminalMessage{{Role: "assistant", Meta: "local", Content: "No pipeline rows available."}}, nil, nil
		}
		var lines []string
		for _, row := range m.gateVM.PipelineRows {
			lines = append(lines, fmt.Sprintf("%s [%s] %s", row.Name, row.Status, row.Note))
		}
		return true, []terminalMessage{{Role: "assistant", Meta: "local", Content: strings.Join(lines, "\n")}}, nil, nil
	case "clear":
		return true, nil, nil, nil
	case "agents":
		var lines []string
		lines = append(lines, fmt.Sprintf("copilot: %s", m.gateVM.CopilotStatus))
		lines = append(lines, fmt.Sprintf("litellm: %s", m.gateVM.LiteLLMStatus))
		lines = append(lines, fmt.Sprintf("current gate: %s", emptyFallback(m.gateVM.CurrentGate, "pending")))
		return true, []terminalMessage{{Role: "assistant", Meta: "local", Content: strings.Join(lines, "\n")}}, nil, nil
	default:
		return false, nil, nil, nil
	}
}

func emptyFallback(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
