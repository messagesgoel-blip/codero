package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui/adapters"
)

// QueuePane renders the active branch queue as a scrollable list.
type QueuePane struct {
	items    []adapters.QueueItem
	selected int
	viewport viewport.Model
	theme    Theme
	width    int
	height   int
	ready    bool
}

func NewQueuePane(theme Theme) QueuePane {
	return QueuePane{theme: theme}
}

func (p QueuePane) Init() tea.Cmd { return nil }

func (p QueuePane) Update(msg tea.Msg) (QueuePane, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.selected > 0 {
				p.selected--
				p.refreshViewport()
			}
		case "down", "j":
			if p.selected < len(p.items)-1 {
				p.selected++
				p.refreshViewport()
			}
		}
	case queueRefreshMsg:
		p.items = msg.items
		p.refreshViewport()
	}
	if p.ready {
		p.viewport, cmd = p.viewport.Update(msg)
	}
	return p, cmd
}

func (p *QueuePane) refreshViewport() {
	if !p.ready {
		return
	}
	var sb strings.Builder
	if len(p.items) == 0 {
		sb.WriteString(p.theme.Muted.Render("  (no active branches)"))
	} else {
		header := fmt.Sprintf("  %-24s  %-14s  %-8s  %s", "BRANCH", "STATE", "ELAPSED", "COMMIT")
		sb.WriteString(p.theme.ListHeader.Render(header) + "\n")
		for i, item := range p.items {
			elapsed := adapters.ElapsedLabel(item.WaitingSec)
			if i == p.selected {
				line := fmt.Sprintf("  %-24s  %-14s  %-8s  %s", truncStr(item.Branch, 24), item.State, elapsed, item.HeadHash)
				sb.WriteString(p.theme.ListSelected.Width(p.width-2).Render(line) + "\n")
			} else {
				// Color-code the state chip; render the rest in normal style.
				stateStyle := p.theme.StateStyle(state.State(item.State))
				branch := p.theme.ListNormal.Render(fmt.Sprintf("  %-24s  ", truncStr(item.Branch, 24)))
				st := stateStyle.Render(fmt.Sprintf("%-14s", item.State))
				rest := p.theme.Muted.Render(fmt.Sprintf("  %-8s  %s", elapsed, item.HeadHash))
				sb.WriteString(branch + st + rest + "\n")
			}
		}
	}
	p.viewport.SetContent(sb.String())
}

func (p QueuePane) View() string {
	if !p.ready {
		return p.theme.Muted.Render("  Loading queue…")
	}
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(p.viewport.View())
}

func (p *QueuePane) SetSize(w, h int) {
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

// SetItems updates the queue items and refreshes the viewport.
func (p *QueuePane) SetItems(items []adapters.QueueItem) {
	p.items = items
	p.refreshViewport()
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
