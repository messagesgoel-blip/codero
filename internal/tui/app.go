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
	PaneLeft   FocusedPane = iota
	PaneCenter FocusedPane = iota
	PaneRight  FocusedPane = iota
)

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

	focused   FocusedPane
	activeTab Tab
	gateVM    adapters.GateViewModel

	paletteActive bool
	paletteInput  textinput.Model

	searchActive bool
	searchInput  textinput.Model

	lastUpdated time.Time
	statusMsg   string
	err         error
}

// New constructs the root TUI model from a Config.
func New(cfg Config) Model {
	theme := cfg.Theme
	keys := DefaultKeyMap()

	palette := textinput.New()
	palette.Placeholder = "Type a command or message…"
	palette.CharLimit = 64

	search := textinput.New()
	search.Placeholder = "search…"
	search.CharLimit = 64

	m := Model{
		cfg:          cfg,
		keys:         keys,
		theme:        theme,
		gatePane:     NewGatePane(theme),
		branchPane:   NewBranchPane(theme),
		queuePane:    NewQueuePane(theme),
		eventsPane:   NewEventsPane(theme),
		checksPane:   NewChecksPane(theme),
		logsArchPane: NewLogsArchPane(theme),
		gateVM:       cfg.InitialVM,
		paletteInput: palette,
		searchInput:  search,
		activeTab:    cfg.InitialTab,
	}
	m.gatePane.SetVM(cfg.InitialVM)
	// Pre-populate checksPane so the right pane isn't blank on first render.
	if report, err := adapters.LoadCheckReport(cfg.RepoPath); err == nil {
		m.checksPane.SetVM(adapters.FromCheckReport(*report))
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
			m.checksPane.SetVM(adapters.FromCheckReport(*report))
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

	if m.outputReady {
		m.outputVP, cmd = m.outputVP.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.paletteActive {
		return m.handlePaletteKey(msg)
	}
	if m.searchActive {
		return m.handleSearchKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Palette):
		m.paletteActive = true
		m.paletteInput.SetValue("")
		cmd := m.paletteInput.Focus()
		return m, cmd

	case key.Matches(msg, m.keys.Search):
		m.searchActive = true
		m.searchInput.SetValue("")
		cmd := m.searchInput.Focus()
		return m, cmd

	case key.Matches(msg, m.keys.NextPane):
		m.focused = (m.focused + 1) % 3

	case key.Matches(msg, m.keys.NextTab):
		m.activeTab = (m.activeTab + 1) % tabCount

	case key.Matches(msg, m.keys.PrevTab):
		m.activeTab = (m.activeTab + tabCount - 1) % tabCount

	case key.Matches(msg, m.keys.Tab1):
		m.activeTab = TabLogs
	case key.Matches(msg, m.keys.Tab2):
		m.activeTab = TabOutput
	case key.Matches(msg, m.keys.Tab3):
		m.activeTab = TabEvents
	case key.Matches(msg, m.keys.Tab4):
		m.activeTab = TabQueue

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

	return m, nil
}

func (m Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.paletteActive = false
		m.paletteInput.Blur()
		return m, nil
	case "enter":
		cmd := strings.ToLower(strings.TrimSpace(m.paletteInput.Value()))
		m.paletteActive = false
		m.paletteInput.Blur()
		return m, m.executePaletteCmd(cmd)
	}
	var teaCmd tea.Cmd
	m.paletteInput, teaCmd = m.paletteInput.Update(msg)
	return m, teaCmd
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.searchActive = false
		m.searchInput.Blur()
		return m, nil
	}
	var teaCmd tea.Cmd
	m.searchInput, teaCmd = m.searchInput.Update(msg)
	return m, teaCmd
}

func (m *Model) executePaletteCmd(cmd string) tea.Cmd {
	switch cmd {
	case "status":
		m.statusMsg = fmt.Sprintf("%s / %s / %s",
			strings.ToLower(m.gateVM.CopilotStatus),
			strings.ToLower(m.gateVM.LiteLLMStatus),
			strings.ToLower(m.gateVM.StatusLabel))
		return nil
	case "help":
		m.statusMsg = "commands: status, help, run gate, queue, logs, retry, quit"
		return nil
	case "run gate":
		if m.gateVM.IsFinal {
			return retryGateCmd(m.cfg.RepoPath)
		}
		m.statusMsg = "gate is already running"
		return nil
	case "queue":
		m.statusMsg = "queue view is available from the center pane controls"
		return nil
	case "retry", "r":
		if m.gateVM.IsFinal {
			return retryGateCmd(m.cfg.RepoPath)
		}
	case "logs", "l":
		return openLogsCmd(m.cfg.RepoPath)
	case "quit", "q":
		return tea.Quit
	}
	return nil
}

func (m Model) View() string {
	if m.layout.TotalW == 0 {
		return "initializing…\n"
	}

	top := m.renderTopBar()
	left := m.renderLeft()
	center := m.renderCenter()
	right := m.renderRight()
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	bottom := m.renderBottomBar()
	full := lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
	return full
}

func (m Model) renderTopBar() string {
	l := m.layout
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A9AEBF")).
		Bold(true).
		Render("COMMAND TERMINAL - CODERO")

	dots := lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F56")).Render("●"),
		" ",
		lipgloss.NewStyle().Foreground(lipgloss.Color("#FFBD2E")).Render("●"),
		" ",
		lipgloss.NewStyle().Foreground(lipgloss.Color("#27C93F")).Render("●"),
	)

	leftPad := 3
	rightPad := 3
	titlePad := l.TotalW - lipgloss.Width(dots) - lipgloss.Width(title) - leftPad - rightPad
	if titlePad < 1 {
		titlePad = 1
	}
	bar := " " + dots + strings.Repeat(" ", leftPad) + strings.Repeat(" ", titlePad/2) + title
	return lipgloss.NewStyle().Width(l.TotalW).Background(lipgloss.Color("#1E1F2E")).Render(bar)
}

func (m Model) renderLeft() string {
	l := m.layout

	// GatePane takes the full left pane height: PROCESSES & AGENTS +
	// RELAY ORCHESTRATION, matching the mockup layout.
	m.gatePane.SetSize(l.LeftW-2, l.ContentH)

	border := m.theme.PaneBorder
	if m.focused == PaneLeft {
		border = m.theme.ActiveBorder
	}
	return border.Width(l.LeftW).Height(l.ContentH).Render(m.gatePane.View())
}

func (m Model) renderCenter() string {
	l := m.layout
	m.logsArchPane.SetSize(l.CenterW-2, l.ContentH)
	content := m.logsArchPane.View()

	border := m.theme.PaneBorder
	if m.focused == PaneCenter {
		border = m.theme.ActiveBorder
	}
	return border.Width(l.CenterW).Height(l.ContentH).Render(content)
}

func (m Model) renderRight() string {
	l := m.layout

	// Right pane is the FINDINGS & ROUTING DASHBOARD, rendered by ChecksPane.
	m.checksPane.SetSize(l.RightW-2, l.ContentH)

	border := m.theme.PaneBorder
	if m.focused == PaneRight {
		border = m.theme.ActiveBorder
	}
	return border.Width(l.RightW).Height(l.ContentH).Render(m.checksPane.View())
}

func (m Model) renderBottomBar() string {
	l := m.layout
	t := m.theme

	// Build merge-status line from gate + checks pane data (mirrors the mockup).
	mergeStatus := m.buildMergeStatus()

	// "Review Findings" button — green, right-aligned (matches mockup).
	reviewBtn := lipgloss.NewStyle().
		Background(lipgloss.Color("#50FA7B")).
		Foreground(lipgloss.Color("#1E1F2E")).
		Bold(true).
		Padding(0, 1).
		Render("Review Findings")

	// Left: merge status.  Right: Review Findings button.
	leftPart := t.Base.Render(mergeStatus + "  ")
	if m.statusMsg != "" {
		leftPart += t.Muted.Render(m.statusMsg + "  ")
	}
	// Pad between left and button.
	leftVisible := lipgloss.Width(leftPart)
	btnVisible := lipgloss.Width(reviewBtn)
	pad := l.TotalW - leftVisible - btnVisible - 2
	if pad < 1 {
		pad = 1
	}
	firstRow := leftPart + strings.Repeat(" ", pad) + reviewBtn

	m.paletteInput.Width = maxInt(24, l.TotalW-48)
	prompt := t.Accent.Render(">") + " " + t.PaletteInput.Render(m.paletteInput.View())
	chips := []string{
		commandChip("status"),
		commandChip("help"),
		commandChip("run gate"),
		commandChip("queue"),
	}
	chipLine := strings.Join(chips, " ")
	secondRow := prompt
	if chipLine != "" {
		spacer := l.TotalW - lipgloss.Width(prompt) - lipgloss.Width(chipLine) - 2
		if spacer < 1 {
			spacer = 1
		}
		secondRow = prompt + strings.Repeat(" ", spacer) + chipLine
	}

	bar := firstRow + "\n" + secondRow
	return t.BottomBar.Width(l.TotalW).Render(bar)
}

// buildMergeStatus returns a concise merge status string for the bottom bar.
// Format mirrors the mockup: "Merge Status: MERGE BLOCKED – [N Critical, N High] → Review Needed"
func (m Model) buildMergeStatus() string {
	t := m.theme
	s := m.checksPane.vm.Summary

	// Count severity buckets.
	buckets := m.checksPane.bucketChecks()
	critCount := len(buckets[0].checks)
	highCount := len(buckets[1].checks)

	switch m.gateVM.Status {
	case gate.StatusPass:
		return t.Pass.Render("Merge Status: MERGE READY — all gates passed")
	case gate.StatusFail:
		detail := ""
		if critCount > 0 || highCount > 0 {
			detail = fmt.Sprintf(" – [%d Critical, %d High Findings] → Review Needed by Security and Tech Lead",
				critCount, highCount)
		} else if s.Failed > 0 {
			detail = fmt.Sprintf(" – [%d Failed Checks] → Fix required before merge", s.Failed)
		}
		return t.Fail.Render("Merge Status: MERGE BLOCKED" + detail)
	default:
		return t.Muted.Render("Merge Status: PENDING — gate review in progress")
	}
}

func (m Model) renderPalette() string {
	t := m.theme
	title := t.Accent.Render("  Command Palette  ")
	input := t.PaletteInput.Render(m.paletteInput.View())
	help := t.Muted.Render("  retry · logs · quit  (esc to close)")
	inner := lipgloss.JoinVertical(lipgloss.Left, title, "  "+input, help)
	return t.Palette.Width(40).Render(inner)
}

func (m *Model) applyLayout() {
	l := m.layout
	m.gatePane.SetSize(l.LeftW-2, l.ContentH)
	m.logsArchPane.SetSize(l.CenterW-2, l.ContentH)
	m.paletteInput.Width = maxInt(24, l.TotalW-48)
	m.queuePane.SetSize(l.CenterW-2, l.ContentH-3)
	m.eventsPane.SetSize(l.CenterW-2, l.ContentH-3)
	m.checksPane.SetSize(l.RightW-2, l.ContentH)
	if !m.outputReady {
		m.outputVP = viewport.New(l.CenterW-2, l.ContentH-3)
		m.outputReady = true
		m.outputVP.SetContent(m.buildOutputContent())
	} else {
		m.outputVP.Width = l.CenterW - 2
		m.outputVP.Height = l.ContentH - 3
	}
	if !m.rightReady {
		m.rightVP = viewport.New(l.RightW-2, l.ContentH-1)
		m.rightReady = true
	} else {
		m.rightVP.Width = l.RightW - 2
		m.rightVP.Height = l.ContentH - 1
	}
}

func commandChip(label string) string {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#2F3648")).
		Foreground(lipgloss.Color("#A3A6B8")).
		Padding(0, 1).
		Render(label)
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
