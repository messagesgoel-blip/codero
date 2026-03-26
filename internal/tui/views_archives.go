package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// defaultArchivesPageSize is the default page size when CODERO_TUI_ARCHIVES_PAGE_SIZE is unset.
const defaultArchivesPageSize = 25

// archivesPageSize returns the configured page size from env or the default.
func archivesPageSize() int {
	if v := os.Getenv("CODERO_TUI_ARCHIVES_PAGE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultArchivesPageSize
}

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
	archives   []ArchiveRow
	page       int
	pageSize   int
	totalPages int
	viewport   viewport.Model
	theme      Theme
	width      int
	height     int
	ready      bool
}

// NewArchivesPane creates an archives pane.
func NewArchivesPane(theme Theme) ArchivesPane {
	return ArchivesPane{
		theme:    theme,
		page:     0,
		pageSize: archivesPageSize(),
	}
}

func (p ArchivesPane) Init() tea.Cmd { return nil }

func (p ArchivesPane) Update(msg tea.Msg) (ArchivesPane, tea.Cmd) {
	if m, ok := msg.(archivesRefreshMsg); ok {
		p.archives = m.archives
		p.totalPages = (len(p.archives) + p.pageSize - 1) / p.pageSize
		if p.totalPages == 0 {
			p.totalPages = 1
		}
		if p.page >= p.totalPages {
			p.page = maxInt(0, p.totalPages-1)
		}
		p.refreshViewport()
	}
	if p.ready {
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(msg)
		return p, cmd
	}
	return p, nil
}

// NextPage advances to the next page if available.
func (p *ArchivesPane) NextPage() {
	if p.page < p.totalPages-1 {
		p.page++
		p.refreshViewport()
	}
}

// PrevPage goes to the previous page if available.
func (p *ArchivesPane) PrevPage() {
	if p.page > 0 {
		p.page--
		p.refreshViewport()
	}
}

// FirstPage jumps to the first page.
func (p *ArchivesPane) FirstPage() {
	p.page = 0
	p.refreshViewport()
}

// LastPage jumps to the last page.
func (p *ArchivesPane) LastPage() {
	if p.totalPages > 0 {
		p.page = p.totalPages - 1
		p.refreshViewport()
	}
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
		// Calculate page slice
		start := p.page * p.pageSize
		end := start + p.pageSize
		if end > len(p.archives) {
			end = len(p.archives)
		}
		pageArchives := p.archives[start:end]

		// Header with columns: agent_id, task, repo/branch, result, duration, commit_count, archived_at
		header := fmt.Sprintf("  %-12s %-12s %-20s %-10s %8s %6s %-16s",
			"AGENT_ID", "TASK", "REPO/BRANCH", "RESULT", "DURATION", "COMMITS", "ARCHIVED_AT")
		sb.WriteString(p.theme.Muted.Render(header) + "\n")
		sb.WriteString(p.theme.Muted.Render("  "+strings.Repeat("─", p.width-4)) + "\n")

		for _, a := range pageArchives {
			resultStyle := p.resultStyle(a.Result)
			repoBranch := truncStr(a.Repo+"/"+a.Branch, 20)
			durStr := formatDuration(a.DurationSeconds)
			archivedAt := a.EndedAt.Format("2006-01-02 15:04")
			taskDisplay := truncStr(a.TaskID, 12)
			if taskDisplay == "" {
				taskDisplay = truncStr(a.TaskSource, 12)
			}

			line := fmt.Sprintf("  %-12s %-12s %-20s %-10s %8s %6d %-16s",
				truncStr(a.AgentID, 12),
				taskDisplay,
				repoBranch,
				resultStyle.Render(fmt.Sprintf("%-10s", a.Result)),
				durStr,
				a.CommitCount,
				archivedAt)
			sb.WriteString(line + "\n")
			if a.MergeSHA != "" {
				sb.WriteString(p.theme.Muted.Render(fmt.Sprintf("    → merge: %s", truncStr(a.MergeSHA, p.width-16))) + "\n")
			}
		}

		// Pagination info
		pageInfo := fmt.Sprintf("  Page %d/%d — showing %d-%d of %d archived sessions",
			p.page+1, p.totalPages, start+1, end, len(p.archives))
		sb.WriteString("\n" + p.theme.Muted.Render(pageInfo) + "\n")
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

// formatDuration formats a duration in seconds to a human-readable string.
func formatDuration(seconds int) string {
	if seconds < 0 {
		return "0s"
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%ds", minutes, secs)
	}
	hours := minutes / 60
	mins := minutes % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
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
