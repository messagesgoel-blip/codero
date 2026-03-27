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
	TabLogs     Tab = iota // INTERACTIVE LOGS & ARCHITECTURE VISUALIZATION (default)
	TabOverview            // was TabOutput
	TabEvents
	TabQueue
	TabChat // NEW
	TabSessionDrill
	TabArchives
	TabCompliance
	TabConfig // NEW
	tabCount
)

var tabLabels = [tabCount]string{"logs & arch", "overview", "events", "queue", "chat", "session", "archives", "compliance", "config"}

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

type overviewSessionRow struct {
	Session         dashboard.ActiveSession
	Checkpoint      string
	HeartbeatAgeSec int64
}

type pipelineCard struct {
	SessionID  string
	AgentID    string
	Branch     string
	Checkpoint string
	Version    int
	StageSec   int64
}

type missionControlHealth struct {
	Status       string
	UptimeSec    int64
	RedisStatus  string
	GitHubStatus string
}

// Config is provided by the command layer to configure the TUI program.
type Config struct {
	RepoPath  string
	Repo      string
	Branch    string
	Context   context.Context
	Interval  time.Duration
	Theme     Theme
	WatchMode bool
	InitialVM adapters.GateViewModel
	// InitialTab sets the center-pane tab that is active when the TUI starts.
	// Defaults to TabLogs when zero value.
	InitialTab Tab
	// StateDB is the optional state database handle used for session/archive/compliance views.
	// When nil, the DB-backed views render a "no data" placeholder.
	StateDB *state.DB
	// DaemonBaseURL is the local daemon URL used for health checks.
	DaemonBaseURL string
	// SettingsDir points at the state directory that holds dashboard-settings.json.
	SettingsDir string
}

// ChatUIState encapsulates slash-popup state for the chat tab.
type ChatUIState struct {
	SlashPopupActive bool
	SlashPopupIdx    int
	SlashPopupFilter string
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

	sessionDrillPane SessionDrillPane
	archivesPane     ArchivesPane
	compliancePane   CompliancePane
	configPane       ConfigPane

	outputVP    viewport.Model
	outputLines []string
	outputReady bool

	rightVP    viewport.Model
	rightReady bool

	pipelinePane  PipelinePane
	pipelineCards []pipelineCard

	focused   FocusedPane
	activeTab Tab
	prevTab   Tab
	gateVM    adapters.GateViewModel
	checksVM  adapters.CheckReportViewModel

	paletteActive bool
	paletteInput  textinput.Model

	searchActive bool
	searchInput  textinput.Model

	cliMessages        []terminalMessage
	cliInput           textinput.Model
	cliBusy            bool
	cliHistory         []string
	cliHistoryIdx      int
	cliSuggestions     []dashboard.ChatSuggestion
	cliActions         []dashboard.ChatAction
	chatState          ChatUIState
	chatConversationID string

	lastUpdated time.Time
	statusMsg   string
	err         error

	branchRecord              *state.BranchRecord
	activeSessions            []dashboard.ActiveSession
	overviewRows              []overviewSessionRow
	overviewSelected          int
	overviewSelectedSessionID string
	missionHealth             missionControlHealth
	queueDepth                int
	blockReasons              []dashboard.BlockReason
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
		cfg:              cfg,
		keys:             keys,
		theme:            theme,
		gatePane:         NewGatePane(theme),
		branchPane:       NewBranchPane(theme),
		queuePane:        NewQueuePane(theme),
		eventsPane:       NewEventsPane(theme),
		checksPane:       NewChecksPane(theme),
		logsArchPane:     NewLogsArchPane(theme),
		pipelinePane:     NewPipelinePane(theme),
		sessionDrillPane: NewSessionDrillPane(theme),
		archivesPane:     NewArchivesPane(theme),
		compliancePane:   NewCompliancePane(theme),
		configPane:       NewConfigPane(theme),
		gateVM:           cfg.InitialVM,
		paletteInput:     palette,
		searchInput:      search,
		cliInput:         cli,
		cliHistoryIdx:    -1,
		activeTab:        cfg.InitialTab,
		cliMessages: []terminalMessage{
			{Role: "system", Meta: "codero", Content: "Type help, status, gate, queue, or ask a review question."},
		},
	}
	m.gatePane.SetVM(cfg.InitialVM)
	// Pre-populate checksPane so the right pane isn't blank on first render.
	if report, _, err := dashboard.LoadGateCheckReport(cfg.RepoPath); err == nil && report != nil {
		vm := adapters.FromCheckReport(*report)
		m.checksVM = vm
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
		return tea.Batch(m.loadLiveShellCmds(), tickCmd(m.cfg.Interval))
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
		cmds = append(cmds, m.loadLiveShellCmds())

	case tickMsg:
		vm := adapters.FromProgressEnv(m.cfg.RepoPath)
		m.gateVM = vm
		m.gatePane.SetVM(vm)
		m.lastUpdated = msg.t
		m.outputLines = nil
		cmds = append(cmds, m.loadLiveShellCmds())
		// Refresh DB-backed views when their tab is active.
		if m.cfg.StateDB != nil {
			switch m.activeTab {
			case TabArchives:
				cmds = append(cmds, m.loadArchivesCmd())
			case TabCompliance:
				cmds = append(cmds, m.loadComplianceCmd())
			}
		}
		if !vm.IsFinal || !m.cfg.WatchMode {
			cmds = append(cmds, tickCmd(m.cfg.Interval))
		} else {
			cmds = append(cmds, tea.Quit)
		}

	case gateRefreshMsg:
		m.gateVM = msg.vm
		m.gatePane.SetVM(msg.vm)

	case queueRefreshMsg:
		m.queuePane.SetItems(msg.items)
		m.queueDepth = len(msg.items)
		m.outputLines = nil
		m.refreshOverviewViewport()

	case branchRefreshMsg:
		m.branchRecord = msg.record
		m.branchPane.SetRecord(msg.record)
		m.outputLines = nil

	case eventsRefreshMsg:
		var cmd tea.Cmd
		m.eventsPane, cmd = m.eventsPane.Update(msg)
		cmds = append(cmds, cmd)
		m.outputLines = nil

	case checksRefreshMsg:
		m.checksVM = msg.vm
		m.checksPane.SetVM(msg.vm)
		m.pipelinePane.SetVM(msg.vm)
		m.outputLines = nil

	case activeSessionsRefreshMsg:
		m.activeSessions = msg.sessions
		m.outputLines = nil
		if len(msg.overviewRows) > 0 || len(msg.pipelineCards) > 0 || msg.health.Status != "" {
			m.overviewRows = msg.overviewRows
			m.pipelineCards = msg.pipelineCards
			m.missionHealth = msg.health
			m.pipelinePane.SetCards(msg.pipelineCards)
			m.syncOverviewSelection()
			m.refreshOverviewViewport()
		}

	case blockReasonsRefreshMsg:
		m.blockReasons = msg.reasons
		m.outputLines = nil

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
		if strings.TrimSpace(msg.response.ConversationID) != "" {
			m.chatConversationID = strings.TrimSpace(msg.response.ConversationID)
		}
		content := strings.TrimSpace(msg.response.Reply)
		if content == "" {
			content = "No assistant response was returned."
		}
		meta := assistantMeta(msg.response.Provider, msg.response.Model)
		if len(m.cliMessages) > 0 && m.cliMessages[len(m.cliMessages)-1].Role == "assistant" && m.cliMessages[len(m.cliMessages)-1].Meta == "streaming" {
			m.cliMessages[len(m.cliMessages)-1] = terminalMessage{Role: "assistant", Meta: meta, Content: content}
		} else if len(m.cliMessages) > 0 && m.cliMessages[len(m.cliMessages)-1].Role == "assistant" && m.cliMessages[len(m.cliMessages)-1].Content != "" {
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
		if len(m.cliMessages) > 0 && m.cliMessages[len(m.cliMessages)-1].Role == "assistant" && m.cliMessages[len(m.cliMessages)-1].Meta == "streaming" {
			m.cliMessages[len(m.cliMessages)-1] = terminalMessage{Role: "error", Meta: "fallback", Content: errText}
		} else if len(m.cliMessages) > 0 && m.cliMessages[len(m.cliMessages)-1].Role == "assistant" && m.cliMessages[len(m.cliMessages)-1].Content != "" {
			m.cliMessages[len(m.cliMessages)-1] = terminalMessage{Role: "error", Meta: "fallback", Content: errText}
		} else {
			m.cliMessages = append(m.cliMessages, terminalMessage{Role: "error", Meta: "fallback", Content: errText})
		}
		m.truncateCLIHistory()

	case terminalChatStreamStartMsg:
		m.cliBusy = true
		if msg.stream == nil {
			return m, nil
		}
		return m, readTerminalChatStreamCmd(msg.stream)

	case terminalChatStreamDeltaMsg:
		m.cliBusy = true
		if msg.delta != "" {
			m.applyChatStreamingDelta(msg.delta)
		}
		if msg.stream != nil {
			return m, readTerminalChatStreamCmd(msg.stream)
		}
		return m, nil

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
	m.sessionDrillPane, cmd = m.sessionDrillPane.Update(msg)
	cmds = append(cmds, cmd)
	m.archivesPane, cmd = m.archivesPane.Update(msg)
	cmds = append(cmds, cmd)
	m.compliancePane, cmd = m.compliancePane.Update(msg)
	cmds = append(cmds, cmd)
	m.configPane, cmd = m.configPane.Update(msg)
	cmds = append(cmds, cmd)

	if m.outputReady {
		m.outputVP, cmd = m.outputVP.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.chatState.SlashPopupActive:
		return m.handleSlashPopupKey(msg)
	case m.activeTab == TabChat:
		return m.handleChatTabKey(msg)
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Chat):
		m.prevTab = m.activeTab
		m.activeTab = TabChat
		m.focused = PaneCenter
		m.cliInput.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Overview):
		m.activeTab = TabOverview
		m.focused = PaneCenter
		return m, nil

	case key.Matches(msg, m.keys.Session):
		if row, ok := m.selectedOverviewRow(); ok {
			m.activeTab = TabSessionDrill
			m.focused = PaneCenter
			return m, m.loadSelectedSessionDetailCmd(row)
		}
		m.activeTab = TabSessionDrill
		m.focused = PaneCenter
		return m, nil

	case key.Matches(msg, m.keys.Pipeline):
		m.focused = PanePipeline
		return m, nil

	case key.Matches(msg, m.keys.Archives):
		m.activeTab = TabArchives
		m.focused = PaneCenter
		return m, nil

	case key.Matches(msg, m.keys.NextPane):
		m.focused = FocusedPane((int(m.focused) + 1) % paneCount)

	case key.Matches(msg, m.keys.PrevPane):
		m.focused = FocusedPane((int(m.focused) - 1 + paneCount) % paneCount)

	case msg.String() == "esc" && m.activeTab == TabSessionDrill:
		m.activeTab = TabOverview
		m.focused = PaneCenter
		return m, nil

	case m.activeTab == TabOverview && m.focused == PaneCenter && key.Matches(msg, m.keys.Up):
		m.moveOverviewSelection(-1)
		return m, nil

	case m.activeTab == TabOverview && m.focused == PaneCenter && key.Matches(msg, m.keys.Down):
		m.moveOverviewSelection(1)
		return m, nil

	case key.Matches(msg, m.keys.Refresh):
		vm := adapters.FromProgressEnv(m.cfg.RepoPath)
		m.gateVM = vm
		m.gatePane.SetVM(vm)
		m.lastUpdated = time.Now()
		return m, m.loadLiveShellCmds()

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
				m.chatConversationID = ""
				m.cliBusy = false
				return m, nil
			}
			if len(msgs) == 0 {
				if len(m.cliMessages) > 0 && m.cliMessages[len(m.cliMessages)-1].Content == "…" {
					m.cliMessages = m.cliMessages[:len(m.cliMessages)-1]
				}
				m.cliBusy = false
				return m, nil
			}
			m.cliMessages = m.cliMessages[:len(m.cliMessages)-1]
			m.cliMessages = append(m.cliMessages, msgs...)
			m.cliSuggestions = suggestions
			m.cliActions = actions
			m.truncateCLIHistory()
			m.cliBusy = false
			return m, nil
		}
		m.cliBusy = true
		m.cliSuggestions = nil
		m.cliActions = nil
		if len(m.cliMessages) == 0 || m.cliMessages[len(m.cliMessages)-1].Role != "assistant" || m.cliMessages[len(m.cliMessages)-1].Meta != "streaming" {
			m.cliMessages = append(m.cliMessages, terminalMessage{Role: "assistant", Meta: "streaming", Content: "…"})
		}
		return m, dashboardChatStreamCmd(m.cfg.Context, cmd, m.chatContextTab(), m.chatConversationID)

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

func (m *Model) applyChatStreamingDelta(delta string) {
	if len(m.cliMessages) == 0 {
		m.cliMessages = append(m.cliMessages, terminalMessage{Role: "assistant", Meta: "streaming", Content: delta})
		return
	}
	last := &m.cliMessages[len(m.cliMessages)-1]
	if last.Role == "assistant" && last.Meta == "streaming" {
		if last.Content == "…" || last.Content == "" {
			last.Content = delta
			return
		}
		last.Content += delta
		return
	}
	m.cliMessages = append(m.cliMessages, terminalMessage{Role: "assistant", Meta: "streaming", Content: delta})
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
	case TabOverview:
		return "overview"
	case TabChat:
		return "chat"
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
	bottom := renderStatusBar(m)
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
	innerW := l.CenterW - 2
	innerH := l.ContentH - 2

	var content string
	switch m.activeTab {
	case TabOverview:
		m.refreshOverviewViewport()
		content = m.outputVP.View()
	case TabSessionDrill:
		m.sessionDrillPane.SetSize(innerW, innerH)
		content = m.sessionDrillPane.View()
	case TabArchives:
		m.archivesPane.SetSize(innerW, innerH)
		content = m.archivesPane.View()
	case TabCompliance:
		m.compliancePane.SetSize(innerW, innerH)
		content = m.compliancePane.View()
	case TabConfig:
		m.configPane.SetSize(innerW, innerH)
		content = m.configPane.View()
	case TabChat:
		content = m.renderChatTab(innerW, innerH)
	default:
		m.logsArchPane.SetSize(innerW, innerH)
		content = m.logsArchPane.View()
	}

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
	m.sessionDrillPane.SetSize(l.CenterW-2, l.ContentH-5)
	m.archivesPane.SetSize(l.CenterW-2, l.ContentH-5)
	m.compliancePane.SetSize(l.CenterW-2, l.ContentH-5)
	m.configPane.SetSize(l.CenterW-2, l.ContentH-5)
	if !m.outputReady {
		m.outputVP = viewport.New(l.CenterW-2, l.ContentH-5)
		m.outputReady = true
	} else {
		m.outputVP.Width = l.CenterW - 2
		m.outputVP.Height = l.ContentH - 5
	}
	m.refreshOverviewViewport()
	if !m.rightReady {
		m.rightVP = viewport.New(l.RightW-2, l.ContentH-3)
		m.rightReady = true
	} else {
		m.rightVP.Width = l.RightW - 2
		m.rightVP.Height = l.ContentH - 3
	}
}

func (m Model) buildOutputContent() string {
	rows := m.overviewRows
	if len(rows) == 0 && len(m.activeSessions) > 0 {
		rows = make([]overviewSessionRow, 0, len(m.activeSessions))
		for _, session := range m.activeSessions {
			rows = append(rows, overviewSessionRow{
				Session:         session,
				Checkpoint:      normalizeStageName(session.ActivityState),
				HeartbeatAgeSec: int64(time.Since(session.LastHeartbeatAt).Seconds()),
			})
		}
	}
	contentW := m.outputVP.Width
	if contentW <= 0 {
		contentW = maxInt(60, m.layout.CenterW-2)
	}

	var sb strings.Builder
	sb.WriteString(m.theme.PaneTitle.Render("MISSION CONTROL OVERVIEW") + "\n")
	sb.WriteString(m.renderHealthBar(contentW) + "\n\n")
	sb.WriteString(m.theme.Muted.Render(fmt.Sprintf("  Active sessions: %d  Queue depth: %d", len(rows), m.queueDepth)) + "\n\n")
	sb.WriteString(m.renderOverviewHeader(contentW) + "\n")
	if len(rows) == 0 {
		sb.WriteString(m.theme.Muted.Render("  No active sessions\n"))
		return sb.String()
	}
	for i, row := range rows {
		sb.WriteString(m.renderOverviewRow(row, contentW, i == m.overviewSelected))
		if i < len(rows)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func (m Model) renderHealthBar(width int) string {
	chip := func(style lipgloss.Style, label, value string) string {
		return lipgloss.NewStyle().Background(m.theme.ChipBackground).Foreground(m.theme.ChipForeground).Padding(0, 1).Render(
			style.Render(fmt.Sprintf("%s %s", label, value)),
		)
	}

	daemonStatus := chip(m.statusStyle(m.missionHealth.Status), "daemon", fmt.Sprintf("%s %s", m.missionHealth.Status, formatUptime(m.missionHealth.UptimeSec)))
	redisStatus := chip(m.statusStyle(m.missionHealth.RedisStatus), "redis", m.missionHealth.RedisStatus)
	ghStatus := chip(m.statusStyle(m.missionHealth.GitHubStatus), "github", m.missionHealth.GitHubStatus)
	queueStatus := chip(m.theme.Warning, "queue", fmt.Sprintf("%d", m.queueDepth))

	bar := lipgloss.JoinHorizontal(lipgloss.Left, daemonStatus, " ", redisStatus, " ", ghStatus, " ", queueStatus)
	return lipgloss.NewStyle().Width(width).Render(bar)
}

func (m Model) renderOverviewHeader(width int) string {
	_ = width
	return m.theme.ListHeader.Render("  AGENT_ID      SESSION   CHECKPOINT   DURATION   REPO/BRANCH                    HEARTBEAT")
}

func (m Model) renderOverviewRow(row overviewSessionRow, width int, selected bool) string {
	agent := truncStr(row.Session.AgentID, 12)
	sessionID := truncStr(row.Session.SessionID, 8)
	checkpoint := truncStr(row.Checkpoint, 11)
	duration := adapters.ElapsedLabel(int(row.Session.ElapsedSec))
	repoBranch := row.Session.Repo
	if row.Session.Branch != "" {
		if repoBranch != "" {
			repoBranch += "/"
		}
		repoBranch += row.Session.Branch
	}
	repoBranch = truncStr(repoBranch, maxInt(20, width-48))
	heartbeat := fmt.Sprintf("%ds", maxInt(0, int(row.HeartbeatAgeSec)))
	heartbeatStyled := m.heartbeatAgeStyle(row.HeartbeatAgeSec).Render(heartbeat)

	raw := fmt.Sprintf("  %-12s %-8s %-11s %-9s %-*s %s",
		agent, sessionID, checkpoint, duration, maxInt(20, width-48), repoBranch, heartbeatStyled)
	if selected {
		return m.theme.ListSelected.Render(raw)
	}
	return m.theme.ListNormal.Render(raw)
}

func (m Model) heartbeatAgeStyle(ageSec int64) lipgloss.Style {
	switch {
	case ageSec < 30:
		return m.theme.Pass
	case ageSec < 60:
		return m.theme.Warning
	default:
		return m.theme.Fail
	}
}

func (m Model) statusStyle(status string) lipgloss.Style {
	status = strings.ToLower(strings.TrimSpace(status))
	switch {
	case status == "ok" || status == "connected":
		return m.theme.Pass
	case status == "degraded" || status == "disconnected":
		return m.theme.Warning
	case strings.HasPrefix(status, "error") || status == "failed":
		return m.theme.Fail
	default:
		return m.theme.Muted
	}
}

func (m Model) syncOverviewSelection() {
	if len(m.overviewRows) == 0 {
		m.overviewSelected = 0
		m.overviewSelectedSessionID = ""
		return
	}
	if m.overviewSelectedSessionID != "" {
		for i, row := range m.overviewRows {
			if row.Session.SessionID == m.overviewSelectedSessionID {
				m.overviewSelected = i
				return
			}
		}
	}
	if m.overviewSelected < 0 {
		m.overviewSelected = 0
	}
	if m.overviewSelected >= len(m.overviewRows) {
		m.overviewSelected = len(m.overviewRows) - 1
	}
	m.overviewSelectedSessionID = m.overviewRows[m.overviewSelected].Session.SessionID
}

func (m *Model) moveOverviewSelection(delta int) {
	if len(m.overviewRows) == 0 {
		return
	}
	m.overviewSelected += delta
	if m.overviewSelected < 0 {
		m.overviewSelected = 0
	}
	if m.overviewSelected >= len(m.overviewRows) {
		m.overviewSelected = len(m.overviewRows) - 1
	}
	m.overviewSelectedSessionID = m.overviewRows[m.overviewSelected].Session.SessionID
	m.refreshOverviewViewport()
}

func (m Model) selectedOverviewRow() (overviewSessionRow, bool) {
	if len(m.overviewRows) == 0 {
		return overviewSessionRow{}, false
	}
	if m.overviewSelected < 0 {
		return overviewSessionRow{}, false
	}
	if m.overviewSelected >= len(m.overviewRows) {
		return overviewSessionRow{}, false
	}
	return m.overviewRows[m.overviewSelected], true
}

func (m *Model) refreshOverviewViewport() {
	if !m.outputReady {
		return
	}
	if len(m.outputLines) > 0 {
		m.outputVP.SetContent(strings.Join(m.outputLines, "\n"))
		return
	}
	m.outputVP.SetContent(m.buildOutputContent())
}

func formatUptime(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	return (time.Duration(seconds) * time.Second).Truncate(time.Second).String()
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

// ─── DB-backed view refresh commands (RV-1 parity) ──────────────────────

func (m Model) loadArchivesCmd() tea.Cmd {
	return func() tea.Msg {
		rows, err := queryTUIArchives(m.cfg.Context, m.cfg.StateDB)
		if err != nil {
			return errMsg{err}
		}
		return archivesRefreshMsg{archives: rows}
	}
}

func (m Model) loadComplianceCmd() tea.Cmd {
	return func() tea.Msg {
		vm, err := queryTUICompliance(m.cfg.Context, m.cfg.StateDB)
		if err != nil {
			return errMsg{err}
		}
		return complianceRefreshMsg{vm: vm}
	}
}

// queryTUIArchives reads session_archives for the TUI archives view.
func queryTUIArchives(ctx context.Context, db *state.DB) ([]ArchiveRow, error) {
	if db == nil {
		return nil, nil
	}
	sqlDB := db.Unwrap()
	var hasTable bool
	err := sqlDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='session_archives'`).Scan(&hasTable)
	if err != nil || !hasTable {
		return nil, err
	}

	rows, err := sqlDB.QueryContext(ctx, `
		SELECT archive_id, session_id, agent_id, result,
		       COALESCE(repo, ''), COALESCE(branch, ''),
		       COALESCE(task_id, ''), COALESCE(task_source, ''),
		       started_at, ended_at,
		       COALESCE(duration_seconds, 0), COALESCE(commit_count, 0),
		       COALESCE(merge_sha, '')
		FROM session_archives
		ORDER BY ended_at DESC
		LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArchiveRow
	for rows.Next() {
		var a ArchiveRow
		if err := rows.Scan(&a.ArchiveID, &a.SessionID, &a.AgentID, &a.Result,
			&a.Repo, &a.Branch, &a.TaskID, &a.TaskSource,
			&a.StartedAt, &a.EndedAt,
			&a.DurationSeconds, &a.CommitCount, &a.MergeSHA); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// queryTUICompliance reads gate compliance state for the TUI compliance view.
func queryTUICompliance(ctx context.Context, db *state.DB) (ComplianceViewModel, error) {
	var vm ComplianceViewModel
	if db == nil {
		return vm, nil
	}
	sqlDB := db.Unwrap()

	// Read gate rules from compliance_rules table.
	var hasRules bool
	_ = sqlDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='compliance_rules'`).Scan(&hasRules)
	if hasRules {
		rows, err := sqlDB.QueryContext(ctx,
			`SELECT rule_id, COALESCE(rule_version, 0), COALESCE(description, ''), COALESCE(enforcement, 'blocking')
			 FROM compliance_rules ORDER BY rule_id LIMIT 100`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var r ComplianceRuleRow
				_ = rows.Scan(&r.RuleID, &r.RuleVersion, &r.Description, &r.Enforcement)
				vm.Rules = append(vm.Rules, r)
			}
		}
	}

	// Read recent compliance checks.
	var hasChecks bool
	_ = sqlDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='compliance_checks'`).Scan(&hasChecks)
	if hasChecks {
		rows, err := sqlDB.QueryContext(ctx,
			`SELECT check_id, COALESCE(assignment_id, ''), COALESCE(session_id, ''),
			        rule_id, result, violation, checked_at, COALESCE(resolved_by, '')
			 FROM compliance_checks ORDER BY checked_at DESC LIMIT 50`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var c ComplianceCheckRow
				_ = rows.Scan(&c.CheckID, &c.AssignmentID, &c.SessionID,
					&c.RuleID, &c.Result, &c.Violation, &c.CheckedAt, &c.ResolvedBy)
				if c.Violation {
					vm.Violations++
				}
				vm.Checks = append(vm.Checks, c)
			}
		}
	}

	return vm, nil
}
