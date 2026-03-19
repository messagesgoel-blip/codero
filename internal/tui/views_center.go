package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/state"
)

// ── LogsArchPane: INTERACTIVE LOGS & ARCHITECTURE VISUALIZATION ───────────────

// logEntry is one item in the event log stream.
type logEntry struct {
	ts    string // formatted timestamp
	msg   string // human-readable message
	level string // "normal" | "finding" | "alert"
	arrow bool   // show ← pointer
}

// LogsArchPane renders the center pane: event log on the left and a static
// architecture node diagram + code-snippet box on the right.
type LogsArchPane struct {
	events []logEntry
	theme  Theme
	width  int
	height int
}

// NewLogsArchPane creates a LogsArchPane with the given theme.
func NewLogsArchPane(theme Theme) LogsArchPane {
	return LogsArchPane{theme: theme}
}

func (p LogsArchPane) Init() tea.Cmd { return nil }

func (p LogsArchPane) Update(msg tea.Msg) (LogsArchPane, tea.Cmd) {
	if m, ok := msg.(eventsRefreshMsg); ok {
		p.events = deliveryEventsToLog(m.events)
	}
	return p, nil
}

// deliveryEventsToLog maps delivery events to styled log entries.
func deliveryEventsToLog(events []state.DeliveryEvent) []logEntry {
	out := make([]logEntry, 0, len(events))
	for _, e := range events {
		level := "normal"
		arrow := false
		msg := e.EventType
		if e.Payload != "" {
			msg = e.EventType + ": " + truncStr(e.Payload, 60)
		}
		switch e.EventType {
		case "finding_bundle", "finding":
			level = "finding"
			arrow = true
		case "error":
			level = "alert"
			arrow = true
		case "review_started", "gate_running":
			level = "alert"
		}
		out = append(out, logEntry{
			ts:    e.CreatedAt.Format("2006 at 3:04 PM"),
			msg:   msg,
			level: level,
			arrow: arrow,
		})
	}
	return out
}

// defaultLogEntries returns an empty slice; the pane renders an idle-state
// message when there are no live events to display.
func defaultLogEntries() []logEntry {
	return []logEntry{}
}

func (p LogsArchPane) View() string {
	if p.width == 0 {
		return ""
	}

	w := maxInt(0, p.width)

	// Split: 60% log, 40% arch
	archW := w * 4 / 10
	if archW < 22 {
		archW = 22
	}
	logW := w - archW
	if logW < 12 {
		// Not enough room for arch on narrow terminals — log only
		logW = w
		archW = 0
	}

	// Header row spans full width
	header := p.theme.ListHeader.Render("  INTERACTIVE LOGS & ARCHITECTURE VISUALIZATION")
	sep := p.theme.Muted.Render(strings.Repeat("─", maxInt(0, w-2)))

	// Content height below header + sep
	contentH := maxInt(0, p.height-2)

	// Render the two columns
	logCol := p.renderLogColumn(logW, contentH)
	var body string
	if archW > 0 {
		archCol := p.renderArchColumn(archW, contentH)
		body = lipgloss.JoinHorizontal(lipgloss.Top, logCol, archCol)
	} else {
		body = logCol
	}

	full := lipgloss.JoinVertical(lipgloss.Left, header, sep, body)
	return lipgloss.NewStyle().Width(w).Height(p.height).Render(full)
}

// renderLogColumn builds the scrollable event-log column.
func (p LogsArchPane) renderLogColumn(w, h int) string {
	entries := p.events
	if len(entries) == 0 {
		entries = defaultLogEntries()
	}

	lines := make([]string, 0, h)
	for _, e := range entries {
		if len(lines) >= h {
			break
		}

		ts := e.ts
		// Reserve 2 chars for optional " ←" suffix
		suffix := ""
		if e.arrow {
			suffix = " ←"
		}
		// Visible width: 2 indent + msgW + 1 space + tsLen + sufLen
		tsLen := len(ts)
		sufLen := len(suffix)
		msgW := maxInt(0, w-3-tsLen-sufLen)
		msgStr := truncStr(e.msg, msgW)

		raw := fmt.Sprintf("  %-*s %s%s", msgW, msgStr, ts, suffix)

		var line string
		switch e.level {
		case "finding":
			line = p.theme.Running.Render(raw)
		case "alert":
			line = p.theme.Warning.Render(raw)
		default:
			line = p.theme.Muted.Render(raw)
		}
		lines = append(lines, line)
		// Blank separator between entries (matching the mockup spacing)
		if len(lines) < h {
			lines = append(lines, "")
		}
	}

	for len(lines) < h {
		lines = append(lines, "")
	}
	content := strings.Join(lines[:minInt(len(lines), h)], "\n")
	return lipgloss.NewStyle().Width(w).Height(h).Render(content)
}

// renderArchColumn builds the architecture diagram + code-snippet panel.
func (p LogsArchPane) renderArchColumn(w, h int) string {
	t := p.theme
	lines := archDiagramLines(w, t)

	for len(lines) < h {
		lines = append(lines, "")
	}
	content := strings.Join(lines[:minInt(len(lines), h)], "\n")
	return lipgloss.NewStyle().Width(w).Height(h).Render(content)
}

// archDiagramLines returns the architecture diagram + code-snippet box lines.
// The diagram mirrors the mockup: Auth Service → Token Flow → Auth Middleware
// → {config.yaml | Active Node} with a code-snippet inset below.
func archDiagramLines(w int, t Theme) []string {
	// Box inner width: fit within w with 2-char left margin + 2 border chars
	bw := minInt(w-4, 17)
	if bw < 8 {
		bw = 8
	}

	accent := t.Accent
	muted := t.Muted
	fail := t.Fail
	pass := t.Pass

	hline := func(n int) string { return strings.Repeat("─", maxInt(0, n)) }
	spaces := func(n int) string { return strings.Repeat(" ", maxInt(0, n)) }

	mid := bw / 2 // connector offset from left of box

	lines := []string{
		// Auth Service box
		muted.Render(fmt.Sprintf("  ╭%s╮", hline(bw))),
		accent.Render(fmt.Sprintf("  │ %-*s│", bw-1, "Auth Service")),
		muted.Render(fmt.Sprintf("  ╰%s┬%s╯", hline(mid), hline(bw-mid-1))),
		// connector
		muted.Render(fmt.Sprintf("  %s│", spaces(mid+1))),
		// Token Flow box
		muted.Render(fmt.Sprintf("  ╭%s▼%s╮", hline(mid), hline(bw-mid-1))),
		accent.Render(fmt.Sprintf("  │ %-*s│", bw-1, "Token Flow")),
		muted.Render(fmt.Sprintf("  ╰%s┬%s╯", hline(mid), hline(bw-mid-1))),
		// connector
		muted.Render(fmt.Sprintf("  %s│", spaces(mid+1))),
		// Auth Middleware box (same width)
		muted.Render(fmt.Sprintf("  ╭%s▼%s╮", hline(mid), hline(bw-mid-1))),
		accent.Render(fmt.Sprintf("  │ %-*s│", bw-1, "Auth Middleware")),
		muted.Render(fmt.Sprintf("  ╰%s┬%s┬%s╯", hline(mid/2), hline(mid-mid/2), hline(bw-mid-1))),
	}

	// Two-branch connectors and leaf nodes
	lOff := mid / 2 // left branch column offset
	rOff := mid     // right branch column offset
	lines = append(lines,
		muted.Render(fmt.Sprintf("  %s│%s│", spaces(lOff+1), spaces(rOff-lOff-1))),
	)

	// Leaf node widths
	hw := minInt((bw-3)/2, 10)
	if hw < 4 {
		hw = 4
	}
	gap := rOff - lOff - hw - 1
	if gap < 1 {
		gap = 1
	}

	lines = append(lines,
		fail.Render(fmt.Sprintf("  ╭%s╮%s╭%s╮", hline(hw), spaces(gap), hline(hw))),
		fail.Render(fmt.Sprintf("  │%-*s│%s│%-*s│", hw, truncStr("config.yaml", hw), spaces(gap), hw, truncStr("Active Node", hw))),
		fail.Render(fmt.Sprintf("  ╰%s╯%s╰%s╯", hline(hw), spaces(gap), hline(hw))),
		"",
	)
	_ = pass

	// Code-snippet box
	sw := minInt(w-4, 22)
	if sw < 10 {
		sw = 10
	}
	lines = append(lines,
		muted.Render(fmt.Sprintf("  ┌%s┐", hline(sw))),
		muted.Render(fmt.Sprintf("  │ %-*s│", sw-2, "config.yaml:")),
		fail.Render(fmt.Sprintf("  │  secret_loc: %-*s│", sw-16, "████")),
		muted.Render(fmt.Sprintf("  │  private: %-*s│", sw-13, "null")),
		muted.Render(fmt.Sprintf("  │  pair_names: %-*s│", sw-16, truncStr("$accoevpit", sw-16))),
		muted.Render(fmt.Sprintf("  └%s┘", hline(sw))),
	)

	return lines
}

func (p *LogsArchPane) SetSize(w, h int) { p.width = w; p.height = h }
