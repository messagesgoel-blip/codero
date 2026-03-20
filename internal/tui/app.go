// Package tui provides the Bubble Tea TUI for Codero operator workflows.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui/adapters"
)

// Tab identifies the active center-pane tab.
type Tab int

const (
	TabLogs Tab = iota // INTERACTIVE LOGS & ARCHITECTURE VISUALIZATION (default)
	TabOutput
	TabEvents
	TabQueue
	tabCount
)

var tabLabels = [tabCount]string{"logs & arch", "output", "events", "queue"}

// FocusedPane identifies which pane has keyboard focus.
type FocusedPane int

const (
	PaneLeft FocusedPane = iota
	PaneCenter
	PanePipeline
	PaneRight
)

const paneCount = int(PaneRight) + 1
const maxCLIHistoryMessages = 8
const cliHistoryTruncatedNotice = "Earlier conversation truncated for brevity."

// internal Bubble Tea messages
type (
	tickMsg          struct{ t time.Time }
	gateRefreshMsg   struct{ vm adapters.GateViewModel }
	queueRefreshMsg  struct{ items []adapters.QueueItem }
	branchRefreshMsg struct{ record *state.BranchRecord }
	eventsRefreshMsg struct{ events []state.DeliveryEvent }
	outputMsg        struct{ lines []string }
	errMsg           struct{ err error }
)

// Config is provided by the command layer to configure the TUI program.
type Config struct {
	RepoPath  string
	Context   context.Context
	Interval  time.Duration
	Theme     Theme
	WatchMode bool
	InitialVM adapters.GateViewModel
	// InitialTab sets the center-pane tab that is active when the TUI starts.
	// Defaults to TabOutput when zero value.
	InitialTab Tab
}

// Model is the root Bubble Tea model for the Codero TUI.
type Model struct {
	cfg    Config
	layout Layout
	keys   KeyMap
	theme  Theme

	gatePane     GatePane
	branchPane   BranchPane
	queuePane    QueuePane
	eventsPane   EventsPane
	checksPane   ChecksPane
	logsArchPane LogsArchPane

	outputVP    viewport.Model
	outputLines []string
	outputReady bool

	rightVP    viewport.Model
	rightReady bool

	pipelinePane PipelinePane

	focused   FocusedPane
	activeTab Tab
	gateVM    adapters.GateViewModel

	paletteActive bool
	paletteInput  textinput.Model

	searchActive bool
	searchInput  textinput.Model

	cliMessages    []terminalMessage
	cliInput       textinput.Model
	cliBusy        bool
	cliHistory     []string
	cliHistoryIdx  int
	cliSuggestions []dashboard.ChatSuggestion
	cliActions     []dashboard.ChatAction

	lastUpdated time.Time
	statusMsg   string
	err         error
}

// New constructs the root TUI model from a Config.
func New(cfg Config) Model {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	theme := cfg.Theme
	keys := DefaultKeyMap()

	palette := textinput.New()
	palette.Placeholder = "Type a command or message…"
	palette.CharLimit = 64

	search := textinput.New()
	search.Placeholder = "search…"
	search.CharLimit = 64
	cli := textinput.New()
	cli.Prompt = ""
	cli.Placeholder = "type a command or message…"
	cli.CharLimit = 256
	cli.Focus()

	m := Model{
		cfg:           cfg,
		keys:          keys,
		theme:         theme,
		gatePane:      NewGatePane(theme),
		branchPane:    NewBranchPane(theme),
		queuePane:     NewQueuePane(theme),
		eventsPane:    NewEventsPane(theme),
		checksPane:    NewChecksPane(theme),
		logsArchPane:  NewLogsArchPane(theme),
		pipelinePane:  NewPipelinePane(theme),
		gateVM:        cfg.InitialVM,
		paletteInput:  palette,
		searchInput:   search,
		cliInput:      cli,
		cliHistoryIdx: -1,
		activeTab:     cfg.InitialTab,
		cliMessages: []terminalMessage{
			{Role: "system", Meta: "codero", Content: "Type help, status, gate, queue, or ask a review question."},
		},
	}
	m.gatePane.SetVM(cfg.InitialVM)
	// Pre-populate checksPane so the right pane isn't blank on first render.
	if report, err := adapters.LoadCheckReport(cfg.RepoPath); err == nil {
		vm := adapters.FromCheckReport(*report)
		m.checksPane.SetVM(vm)
		m.pipelinePane.SetVM(vm)
	}
	return m
}

// AdapterFromPath is a convenience wrapper for the command layer.
func AdapterFromPath(repoPath string) adapters.GateViewModel {
	return adapters.FromProgressEnv(repoPath)
}

func (m Model) Init() tea.Cmd {
	if m.cfg.WatchMode {
		return tickCmd(m.cfg.Interval)
	}
	return nil
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg{t} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.layout = Compute(msg.Width, msg.Height)
		m.applyLayout()

	case tickMsg:
		vm := adapters.FromProgressEnv(m.cfg.RepoPath)
		m.gateVM = vm
		m.gatePane.SetVM(vm)
		m.lastUpdated = msg.t
		// Also refresh the gate-check report for the findings pane if available.
		if report, err := adapters.LoadCheckReport(m.cfg.RepoPath); err == nil {
			checksVM := adapters.FromCheckReport(*report)
			m.checksPane.SetVM(checksVM)
			m.pipelinePane.SetVM(checksVM)
		}
		if !vm.IsFinal || !m.cfg.WatchMode {
			cmds = append(cmds, tickCmd(m.cfg.Interval))
		} else {
			cmds = append(cmds, tea.Quit)
		}

	case gateRefreshMsg:
		m.gateVM = msg.vm
		m.gatePane.SetVM(msg.vm)
		if report, err := adapters.LoadCheckReport(m.cfg.RepoPath); err == nil {
			vm := adapters.FromCheckReport(*report)
			m.checksPane.SetVM(vm)
			m.pipelinePane.SetVM(vm)
		}

	case queueRefreshMsg:
		m.queuePane.SetItems(msg.items)

	case branchRefreshMsg:
		m.branchPane.SetRecord(msg.record)

	case eventsRefreshMsg:
		var cmd tea.Cmd
		m.eventsPane, cmd = m.eventsPane.Update(msg)
		cmds = append(cmds, cmd)

	case outputMsg:
		m.outputLines = msg.lines
		if m.outputReady {
			m.outputVP.SetContent(strings.Join(m.outputLines, "\n"))
		}

	case errMsg:
		m.err = msg.err
		m.statusMsg = "⚠  " + msg.err.Error()

	case terminalChatResultMsg:
		m.cliBusy = false
		m.cliSuggestions = msg.response.Suggestions
		m.cliActions = msg.response.Actions
		content := strings.TrimSpace(msg.response.Reply)
		if content == "" {
			content = "No assistant response was returned."
		}
		meta := assistantMeta(msg.response.Provider, msg.response.Model)
		if len(m.cliMessages) > 0 && m.cliMessages[len(m.cliMessages)-1].Role == "assistant" && m.cliMessages[len(m.cliMessages)-1].Content == "…" {
			m.cliMessages[len(m.cliMessages)-1] = terminalMessage{Role: "assistant", Meta: meta, Content: content}
		} else {
			m.cliMessages = append(m.cliMessages, terminalMessage{Role: "assistant", Meta: meta, Content: content})
		}
		m.truncateCLIHistory()

	case terminalChatErrorMsg:
		m.cliBusy = false
		m.cliSuggestions = nil
		m.cliActions = nil
		errText := "Review assistant unavailable: " + msg.err.Error()
		if len(m.cliMessages) > 0 && m.cliMessages[len(m.cliMessages)-1].Role == "assistant" && m.cliMessages[len(m.cliMessages)-1].Content == "…" {
			m.cliMessages[len(m.cliMessages)-1] = terminalMessage{Role: "error", Meta: "fallback", Content: errText}
		} else {
			m.cliMessages = append(m.cliMessages, terminalMessage{Role: "error", Meta: "fallback", Content: errText})
		}
		m.truncateCLIHistory()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// propagate to sub-panes
	var cmd tea.Cmd
	m.gatePane, cmd = m.gatePane.Update(msg)
	cmds = append(cmds, cmd)
	m.branchPane, cmd = m.branchPane.Update(msg)
	cmds = append(cmds, cmd)
	m.queuePane, cmd = m.queuePane.Update(msg)
	cmds = append(cmds, cmd)
	m.eventsPane, cmd = m.eventsPane.Update(msg)
	cmds = append(cmds, cmd)
	m.checksPane, cmd = m.checksPane.Update(msg)
	cmds = append(cmds, cmd)
	m.logsArchPane, cmd = m.logsArchPane.Update(msg)
	cmds = append(cmds, cmd)
	m.pipelinePane, cmd = m.pipelinePane.Update(msg)
	cmds = append(cmds, cmd)

	if m.outputReady {
		m.outputVP, cmd = m.outputVP.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.NextPane):
		m.focused = FocusedPane((int(m.focused) + 1) % paneCount)

	case key.Matches(msg, m.keys.PrevPane):
		m.focused = FocusedPane((int(m.focused) - 1 + paneCount) % paneCount)

	case key.Matches(msg, m.keys.Refresh):
		vm := adapters.FromProgressEnv(m.cfg.RepoPath)
		m.gateVM = vm
		m.gatePane.SetVM(vm)
		m.lastUpdated = time.Now()

	case key.Matches(msg, m.keys.Retry):
		if m.gateVM.IsFinal {
			return m, retryGateCmd(m.cfg.RepoPath)
		}

	case key.Matches(msg, m.keys.Logs):
		return m, openLogsCmd(m.cfg.RepoPath)
	}

	return m.handleTerminalKey(msg)
}

func (m Model) handleTerminalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		cmd := strings.TrimSpace(m.cliInput.Value())
		if cmd == "" {
			return m, nil
		}
		m.cliHistory = append([]string{cmd}, m.cliHistory...)
		if len(m.cliHistory) > 50 {
			m.cliHistory = m.cliHistory[:50]
		}
		m.cliHistoryIdx = -1
		m.cliInput.SetValue("")
		m.cliMessages = append(m.cliMessages, terminalMessage{Role: "user", Content: cmd})
		m.cliMessages = append(m.cliMessages, terminalMessage{Role: "assistant", Meta: "streaming", Content: "…"})
		m.truncateCLIHistory()
		if handled, msgs, suggestions, actions := m.localTerminalCommand(cmd); handled {
			if strings.EqualFold(cmd, "clear") {
				m.cliMessages = nil
				m.cliSuggestions = nil
				m.cliActions = nil
				return m, nil
			}
			if len(msgs) == 0 {
				if len(m.cliMessages) > 0 && m.cliMessages[len(m.cliMessages)-1].Content == "…" {
					m.cliMessages = m.cliMessages[:len(m.cliMessages)-1]
				}
				return m, nil
			}
			m.cliMessages = m.cliMessages[:len(m.cliMessages)-1]
			m.cliMessages = append(m.cliMessages, msgs...)
			m.cliSuggestions = suggestions
			m.cliActions = actions
			m.truncateCLIHistory()
			return m, nil
		}
		m.cliBusy = true
		m.cliSuggestions = nil
		m.cliActions = nil
		return m, dashboardChatCmd(m.cfg.Context, cmd, m.chatContextTab())

	case "esc":
		m.cliInput.SetValue("")
		m.cliHistoryIdx = -1
		return m, nil

	case "up", "k":
		input := m.cliInput.Value()
		if input != "" && m.cliInput.Position() > 0 {
			break
		}
		if len(m.cliHistory) == 0 {
			break
		}
		if m.cliHistoryIdx < len(m.cliHistory)-1 {
			m.cliHistoryIdx++
		}
		if m.cliHistoryIdx >= 0 && m.cliHistoryIdx < len(m.cliHistory) {
			m.cliInput.SetValue(m.cliHistory[m.cliHistoryIdx])
			m.cliInput.CursorEnd()
		}
		return m, nil

	case "down", "j":
		input := m.cliInput.Value()
		if input != "" && m.cliInput.Position() < len([]rune(input)) {
			break
		}
		if len(m.cliHistory) == 0 {
			break
		}
		if m.cliHistoryIdx > 0 {
			m.cliHistoryIdx--
			m.cliInput.SetValue(m.cliHistory[m.cliHistoryIdx])
			m.cliInput.CursorEnd()
		} else {
			m.cliHistoryIdx = -1
			m.cliInput.SetValue("")
		}
		return m, nil
	}

	var teaCmd tea.Cmd
	m.cliInput, teaCmd = m.cliInput.Update(msg)
	return m, teaCmd
}

func (m *Model) truncateCLIHistory() {
	if len(m.cliMessages) > maxCLIHistoryMessages {
		retained := append([]terminalMessage(nil), m.cliMessages[len(m.cliMessages)-maxCLIHistoryMessages:]...)
		notice := terminalMessage{Role: "system", Meta: "codero", Content: cliHistoryTruncatedNotice}
		m.cliMessages = append([]terminalMessage{notice}, retained...)
	}
}

func assistantMeta(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	switch {
	case provider != "" && model != "":
		return provider + " / " + model
	case provider != "":
		return provider
	case model != "":
		return model
	default:
		return "assistant"
	}
}

func (m Model) chatContextTab() string {
	switch m.activeTab {
	case TabEvents:
		return "events"
	case TabQueue:
		return "queue"
	case TabOutput:
		return "overview"
	default:
		return "review"
	}
}

func (m Model) View() string {
	if m.layout.TotalW == 0 {
		return "initializing…\n"
	}

	top := m.renderTopBar()
	left := m.renderLeft()
	center := m.renderCenter()
	pipeline := m.renderPipeline()
	right := m.renderRight()
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, center, pipeline, right)
	bottom := renderTerminalCLI(m)
	return lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
}

func (m Model) renderTopBar() string {
	l := m.layout
	title := m.theme.Title.Render(" COMMAND TERMINAL — CODERO")
	dots := lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F56")).Render("●"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#FFBD2E")).Render("●"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#27C93F")).Render("●"),
	)

	currentTime := time.Now().Format("15:04:05")
	right := m.theme.Muted.Render(currentTime + " ")

	spacerW := l.TotalW - lipgloss.Width(dots) - lipgloss.Width(title) - lipgloss.Width(right) - 2
	if spacerW < 0 {
		spacerW = 0
	}
	spacer := strings.Repeat(" ", spacerW)

	bar := lipgloss.JoinHorizontal(lipgloss.Left, " ", dots, title, spacer, right)
	return lipgloss.NewStyle().
		Width(l.TotalW).
		Background(lipgloss.Color("#1E1F2E")).
		Render(bar)
}

func (m Model) renderLeft() string {
	l := m.layout

	// GatePane takes the full left pane height: PROCESSES & AGENTS +
	// RELAY ORCHESTRATION, matching the mockup layout.
	m.gatePane.SetSize(l.LeftW-2, l.ContentH-2)

	border := m.theme.PaneBorder
	if m.focused == PaneLeft {
		border = m.theme.ActiveBorder
	}
	return border.Width(l.LeftW).Height(l.ContentH).Render(m.gatePane.View())
}

func (m Model) renderCenter() string {
	l := m.layout
	m.logsArchPane.SetSize(l.CenterW-2, l.ContentH-2)
	content := m.logsArchPane.View()

	border := m.theme.PaneBorder
	if m.focused == PaneCenter {
		border = m.theme.ActiveBorder
	}
	return border.Width(l.CenterW).Height(l.ContentH).Render(content)
}

func (m Model) renderPipeline() string {
	l := m.layout
	m.pipelinePane.SetSize(l.PipelineW-2, l.ContentH-2)

	border := m.theme.PaneBorder
	if m.focused == PanePipeline {
		border = m.theme.ActiveBorder
	}
	return border.Width(l.PipelineW).Height(l.ContentH).Render(m.pipelinePane.View())
}

func (m Model) renderRight() string {
	l := m.layout

	// Right pane is the FINDINGS & ROUTING DASHBOARD, rendered by ChecksPane.
	m.checksPane.SetSize(l.RightW-2, l.ContentH-2)

	border := m.theme.PaneBorder
	if m.focused == PaneRight {
		border = m.theme.ActiveBorder
	}
	return border.Width(l.RightW).Height(l.ContentH).Render(m.checksPane.View())
}

func (m *Model) applyLayout() {
	l := m.layout
	m.gatePane.SetSize(l.LeftW-2, l.ContentH-2)
	m.logsArchPane.SetSize(l.CenterW-2, l.ContentH-2)
	m.pipelinePane.SetSize(l.PipelineW-2, l.ContentH-2)
	m.paletteInput.Width = maxInt(24, l.TotalW-48)
	m.cliInput.Width = maxInt(24, l.TotalW-48)
	m.queuePane.SetSize(l.CenterW-2, l.ContentH-5)
	m.eventsPane.SetSize(l.CenterW-2, l.ContentH-5)
	m.checksPane.SetSize(l.RightW-2, l.ContentH-2)
	if !m.outputReady {
		m.outputVP = viewport.New(l.CenterW-2, l.ContentH-5)
		m.outputReady = true
		m.outputVP.SetContent(m.buildOutputContent())
	} else {
		m.outputVP.Width = l.CenterW - 2
		m.outputVP.Height = l.ContentH - 5
	}
	if !m.rightReady {
		m.rightVP = viewport.New(l.RightW-2, l.ContentH-3)
		m.rightReady = true
	} else {
		m.rightVP.Width = l.RightW - 2
		m.rightVP.Height = l.ContentH - 3
	}
}

func (m Model) buildOutputContent() string {
	vm := m.gateVM
	var sb strings.Builder
	sb.WriteString(m.theme.PaneTitle.Render("Gate Summary") + "\n\n")
	sb.WriteString(m.theme.Base.Render("  Status:  ") + renderStatusInline(m.theme, vm) + "\n")
	sb.WriteString(m.theme.Muted.Render(fmt.Sprintf("  RunID:   %s", vm.RunID)) + "\n")
	sb.WriteString(m.theme.Muted.Render(fmt.Sprintf("  Elapsed: %s", adapters.ElapsedLabel(vm.ElapsedSec))) + "\n")
	sb.WriteString(m.theme.Base.Render(fmt.Sprintf("  Bar:     %s", vm.ProgressBar)) + "\n")
	if len(vm.Comments) > 0 {
		sb.WriteString("\n" + m.theme.Fail.Render("  Blockers:") + "\n")
		for _, c := range vm.Comments {
			sb.WriteString(m.theme.Warning.Render("    • "+c) + "\n")
		}
	}
	return sb.String()
}

// retryGateCmd re-invokes the current codero binary with commit-gate.
// Safe: bin is os.Executable() (self), args are static + validated repoPath.
//
//nolint:gosec
func retryGateCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		bin, err := os.Executable()
		if err != nil {
			return errMsg{err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
		cmd := exec.CommandContext(ctx, bin, "commit-gate", "--repo-path", repoPath)
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errMsg{fmt.Errorf("commit-gate failed: %w", err)}
		}
		return outputMsg{lines: strings.Split(string(out), "\n")}
	}
}

// openLogsCmd reads the gate log directory.
func openLogsCmd(repoPath string) tea.Cmd {
	return func() tea.Msg {
		logDir := filepath.Join(repoPath, ".codero", "gate-heartbeat")
		entries, err := os.ReadDir(logDir)
		if err != nil {
			return outputMsg{lines: []string{"log dir not found: " + logDir}}
		}
		lines := []string{"Gate logs: " + logDir, ""}
		for _, e := range entries {
			lines = append(lines, "  "+e.Name())
		}
		return outputMsg{lines: lines}
	}
}

func renderStatusInline(t Theme, vm adapters.GateViewModel) string {
	switch vm.Status {
	case gate.StatusPass:
		return t.Pass.Render(vm.StatusIcon + " " + vm.StatusLabel)
	case gate.StatusFail:
		return t.Fail.Render(vm.StatusIcon + " " + vm.StatusLabel)
	default:
		return t.Running.Render(vm.StatusIcon + " " + vm.StatusLabel)
	}
}
