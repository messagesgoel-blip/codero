package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/session"
)

// SessionDetail holds the display model for a single session drill-down (§2.3).
type SessionDetail struct {
	SessionID     string
	AgentID       string
	Mode          string
	Status        string
	TmuxName      string
	Checkpoint    string
	StartedAt     time.Time
	LastSeenAt    time.Time
	EndedAt       *time.Time
	EndReason     string
	Assignments   []SessionAssignmentRow
	GateSummary   string
	FeedbackCount int
	PRNumber      int
}

// SessionAssignmentRow is the display model for an assignment inside a session.
type SessionAssignmentRow struct {
	AssignmentID string
	Repo         string
	Branch       string
	TaskID       string
	State        string
	Substatus    string
	StartedAt    time.Time
	EndedAt      *time.Time
}

// sessionDrillMsg delivers a SessionDetail to the TUI.
type sessionDrillMsg struct{ detail SessionDetail }

// SessionDrillPane renders a single session's lifecycle (§2.3).
type SessionDrillPane struct {
	detail   *SessionDetail
	viewport viewport.Model
	theme    Theme
	width    int
	height   int
	ready    bool
}

// NewSessionDrillPane creates a session drill-down pane.
func NewSessionDrillPane(theme Theme) SessionDrillPane {
	return SessionDrillPane{theme: theme}
}

func (p SessionDrillPane) Init() tea.Cmd { return nil }

func (p SessionDrillPane) Update(msg tea.Msg) (SessionDrillPane, tea.Cmd) {
	if m, ok := msg.(sessionDrillMsg); ok {
		p.detail = &m.detail
		p.refreshViewport()
	}
	if p.ready {
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p *SessionDrillPane) refreshViewport() {
	if !p.ready || p.detail == nil {
		return
	}
	d := p.detail
	var sb strings.Builder

	// Header
	sb.WriteString(p.theme.Accent.Render("  SESSION DRILL-DOWN") + "\n")
	sb.WriteString(p.theme.Muted.Render("  ────────────────────────────────") + "\n")

	// Identity
	sb.WriteString(fmt.Sprintf("  Session:    %s\n", p.theme.Base.Render(d.SessionID)))
	sb.WriteString(fmt.Sprintf("  Agent:      %s\n", p.theme.Base.Render(d.AgentID)))
	sb.WriteString(fmt.Sprintf("  Mode:       %s\n", p.theme.Base.Render(d.Mode)))
	sb.WriteString(fmt.Sprintf("  Tmux:       %s\n", p.theme.Base.Render(d.TmuxName)))

	// Lifecycle
	sb.WriteString(p.theme.Muted.Render("\n  ── Lifecycle ──") + "\n")
	sb.WriteString(fmt.Sprintf("  Status:     %s\n", p.statusStyle(d.Status).Render(d.Status)))
	sb.WriteString(fmt.Sprintf("  Checkpoint: %s\n", p.checkpointStyle(d.Checkpoint).Render(d.Checkpoint)))
	sb.WriteString(fmt.Sprintf("  Started:    %s\n", p.theme.Muted.Render(d.StartedAt.Format(time.RFC3339))))
	sb.WriteString(fmt.Sprintf("  Last Seen:  %s\n", p.theme.Muted.Render(d.LastSeenAt.Format(time.RFC3339))))
	if d.EndedAt != nil {
		sb.WriteString(fmt.Sprintf("  Ended:      %s\n", p.theme.Muted.Render(d.EndedAt.Format(time.RFC3339))))
		sb.WriteString(fmt.Sprintf("  Reason:     %s\n", p.theme.Base.Render(d.EndReason)))
	}

	// Assignments
	sb.WriteString(p.theme.Muted.Render("\n  ── Assignments ──") + "\n")
	if len(d.Assignments) == 0 {
		sb.WriteString(p.theme.Muted.Render("  (none)") + "\n")
	}
	for i, a := range d.Assignments {
		sb.WriteString(fmt.Sprintf("  [%d] %s  %s/%s\n", i+1,
			p.theme.Accent.Render(a.AssignmentID),
			p.theme.Base.Render(a.Repo),
			p.theme.Base.Render(a.Branch)))
		sb.WriteString(fmt.Sprintf("      Task: %s  State: %s  Substatus: %s\n",
			a.TaskID, p.statusStyle(a.State).Render(a.State), a.Substatus))
	}

	// Gate / PR summary
	if d.GateSummary != "" {
		sb.WriteString(p.theme.Muted.Render("\n  ── Gate ──") + "\n")
		sb.WriteString(fmt.Sprintf("  %s\n", p.theme.Base.Render(d.GateSummary)))
	}
	if d.PRNumber > 0 {
		sb.WriteString(fmt.Sprintf("  PR: #%d\n", d.PRNumber))
	}

	p.viewport.SetContent(sb.String())
}

func (p SessionDrillPane) statusStyle(s string) lipgloss.Style {
	switch s {
	case "active":
		return p.theme.Running
	case "completed", "done":
		return p.theme.Pass
	case "lost", "expired", "cancelled":
		return p.theme.Fail
	default:
		return p.theme.Base
	}
}

func (p SessionDrillPane) checkpointStyle(cp string) lipgloss.Style {
	ck := session.Checkpoint(cp)
	if ck.IsTerminal() {
		return p.theme.Fail
	}
	if cp == "" || cp == string(session.CheckpointLaunched) {
		return p.theme.Muted
	}
	return p.theme.Running
}

func (p SessionDrillPane) View() string {
	if !p.ready {
		return p.theme.Muted.Render("  Loading session…")
	}
	if p.detail == nil {
		return p.theme.Muted.Render("  Select a session to drill down (press Enter on queue)")
	}
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(p.viewport.View())
}

func (p *SessionDrillPane) SetSize(w, h int) {
	p.width = w
	p.height = h
	if !p.ready {
		p.viewport = viewport.New(w, h)
		p.ready = true
	} else {
		p.viewport.Width = w
		p.viewport.Height = h
	}
	p.refreshViewport()
}
