package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/state"
)

// EventsPane renders a scrollable delivery event log.
type EventsPane struct {
	events   []state.DeliveryEvent
	viewport viewport.Model
	theme    Theme
	width    int
	height   int
	ready    bool
}

func NewEventsPane(theme Theme) EventsPane {
	return EventsPane{theme: theme}
}

func (p EventsPane) Init() tea.Cmd { return nil }

func (p EventsPane) Update(msg tea.Msg) (EventsPane, tea.Cmd) {
	var cmd tea.Cmd
	if m, ok := msg.(eventsRefreshMsg); ok {
		p.events = m.events
		p.refreshViewport()
	}
	if p.ready {
		p.viewport, cmd = p.viewport.Update(msg)
	}
	return p, cmd
}

func (p *EventsPane) refreshViewport() {
	if !p.ready {
		return
	}
	var sb strings.Builder
	if len(p.events) == 0 {
		sb.WriteString(p.theme.Muted.Render("  (no events)"))
	} else {
		for _, e := range p.events {
			ts := e.CreatedAt.Format("15:04:05")
			payloadWidth := p.width - 40
			if payloadWidth < 0 {
				payloadWidth = 0
			}
			evStyle := p.eventTypeStyle(e.EventType)
			tsStr := p.theme.Muted.Render(fmt.Sprintf("  %s  ", ts))
			typeStr := evStyle.Render(fmt.Sprintf("%-20s", e.EventType))
			payloadStr := p.theme.Base.Render(fmt.Sprintf("  %s", truncStr(e.Payload, payloadWidth)))
			sb.WriteString(tsStr + typeStr + payloadStr + "\n")
		}
	}
	p.viewport.SetContent(sb.String())
}

// eventTypeStyle maps delivery event types to visual styles.
func (p EventsPane) eventTypeStyle(eventType string) lipgloss.Style {
	switch {
	case eventType == "finding_bundle" || eventType == "finding":
		return p.theme.Fail
	case eventType == "state_transition":
		return p.theme.Accent
	case eventType == "review_complete":
		return p.theme.Pass
	case eventType == "review_started", eventType == "gate_running":
		return p.theme.Running
	case eventType == "system", eventType == "error":
		return p.theme.Warning
	default:
		return p.theme.Muted
	}
}

func (p EventsPane) View() string {
	if !p.ready {
		return p.theme.Muted.Render("  Loading events…")
	}
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(p.viewport.View())
}

func (p *EventsPane) SetSize(w, h int) {
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
