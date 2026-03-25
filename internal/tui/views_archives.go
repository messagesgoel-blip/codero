package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ArchiveRow holds the display model for a session archive (§2.8).
type ArchiveRow struct {
	ArchiveID       string
	SessionID       string
	AgentID         string
	Result          string
	Repo            string
	Branch          string
	TaskID          string
	TaskSource      string
	StartedAt       time.Time
	EndedAt         time.Time
	DurationSeconds int
	CommitCount     int
	MergeSHA        string
}

// archivesRefreshMsg delivers archive data to the pane.
type archivesRefreshMsg struct{ archives []ArchiveRow }

// ArchivesPane renders a paginated table of session_archives (§2.8).
type ArchivesPane struct {
	archives []ArchiveRow
	viewport viewport.Model
	theme    Theme
	width    int
	height   int
	ready    bool
}

// NewArchivesPane creates an archives pane.
func NewArchivesPane(theme Theme) ArchivesPane {
	return ArchivesPane{theme: theme}
}

func (p ArchivesPane) Init() tea.Cmd { return nil }

func (p ArchivesPane) Update(msg tea.Msg) (ArchivesPane, tea.Cmd) {
	if m, ok := msg.(archivesRefreshMsg); ok {
		p.archives = m.archives
		p.refreshViewport()
	}
	if p.ready {
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p *ArchivesPane) refreshViewport() {
	if !p.ready {
		return
	}
	var sb strings.Builder

	sb.WriteString(p.theme.Accent.Render("  SESSION ARCHIVES") + "\n")
	sb.WriteString(p.theme.Muted.Render("  ────────────────────────────────────────────────────────────") + "\n")

	if len(p.archives) == 0 {
		sb.WriteString(p.theme.Muted.Render("  (no archived sessions)") + "\n")
	} else {
		// Header
		header := fmt.Sprintf("  %-16s %-12s %-12s %-20s %-16s %s",
			"SESSION", "AGENT", "RESULT", "REPO/BRANCH", "CHECKPOINT", "ENDED")
		sb.WriteString(p.theme.Muted.Render(header) + "\n")
		sb.WriteString(p.theme.Muted.Render("  "+strings.Repeat("─", p.width-4)) + "\n")

		for _, a := range p.archives {
			resultStyle := p.resultStyle(a.Result)
			repoBranch := truncStr(a.Repo+"/"+a.Branch, 20)
			endStr := a.EndedAt.Format("15:04:05")
			durStr := fmt.Sprintf("%ds", a.DurationSeconds)
			line := fmt.Sprintf("  %-16s %-12s %-12s %-20s %-8s %s",
				truncStr(a.SessionID, 16),
				truncStr(a.AgentID, 12),
				resultStyle.Render(fmt.Sprintf("%-12s", a.Result)),
				repoBranch,
				durStr,
				endStr)
			sb.WriteString(line + "\n")
			if a.MergeSHA != "" {
				sb.WriteString(p.theme.Muted.Render(fmt.Sprintf("    → merge: %s", truncStr(a.MergeSHA, p.width-16))) + "\n")
			}
		}
		sb.WriteString(fmt.Sprintf("\n  %s", p.theme.Muted.Render(fmt.Sprintf("Total: %d archived sessions", len(p.archives)))))
	}

	p.viewport.SetContent(sb.String())
}

func (p ArchivesPane) resultStyle(result string) lipgloss.Style {
	switch result {
	case "done", "ended":
		return p.theme.Pass
	case "lost", "expired":
		return p.theme.Fail
	case "cancelled":
		return p.theme.Warning
	default:
		return p.theme.Base
	}
}

func (p ArchivesPane) View() string {
	if !p.ready {
		return p.theme.Muted.Render("  Loading archives…")
	}
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(p.viewport.View())
}

func (p *ArchivesPane) SetSize(w, h int) {
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
