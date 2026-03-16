package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/tui/adapters"
)

// GatePane renders the left-pane gate timeline.
type GatePane struct {
	vm       adapters.GateViewModel
	selected int
	theme    Theme
	width    int
	height   int
}

func NewGatePane(theme Theme) GatePane {
	return GatePane{theme: theme}
}

func (p GatePane) Init() tea.Cmd { return nil }

func (p GatePane) Update(msg tea.Msg) (GatePane, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.selected > 0 {
				p.selected--
			}
		case "down", "j":
			total := 2 + len(p.vm.PipelineRows)
			if p.selected < total-1 {
				p.selected++
			}
		}
	case gateRefreshMsg:
		p.vm = msg.vm
	}
	return p, nil
}

func (p GatePane) View() string {
	if p.width == 0 {
		return ""
	}
	lines := make([]string, 0, p.height)
	w := p.width - 2

	lines = append(lines, p.theme.ListHeader.Render("  GATES"))
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))

	authGates := []struct{ name, status string }{
		{"copilot", p.vm.CopilotStatus},
		{"litellm", p.vm.LiteLLMStatus},
	}
	for i, g := range authGates {
		icon := gate.StateIcon(g.status)
		if g.name == p.vm.CurrentGate {
			icon = "●"
		}
		stateStyle := p.theme.GateStatusStyle(g.status)
		label := fmt.Sprintf("%s %-8s  %s", icon, g.name, g.status)
		var line string
		if p.selected == i {
			line = p.theme.ListSelected.Width(w).Render(fmt.Sprintf("  %s %-8s  %s", icon, g.name, g.status))
		} else {
			line = p.theme.GateAuthoritative.Render(fmt.Sprintf("  %s", stateStyle.Render(label)))
		}
		lines = append(lines, line)
	}

	lines = append(lines, "")
	lines = append(lines, p.theme.Muted.Render("  ── pipeline (local) ──"))
	for i, row := range p.vm.PipelineRows {
		idx := 2 + i
		icon := gate.StateIcon(row.Status)
		var line string
		if p.selected == idx {
			line = p.theme.ListSelected.Width(w).Render(fmt.Sprintf("  %s %-8s", icon, row.Name))
		} else {
			line = p.theme.GatePipeline.Render(fmt.Sprintf("  %s %-8s", icon, row.Name))
		}
		lines = append(lines, line)
	}

	for len(lines) < p.height {
		lines = append(lines, "")
	}
	content := strings.Join(lines[:minInt(len(lines), p.height)], "\n")
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(content)
}

func (p *GatePane) SetSize(w, h int) { p.width = w; p.height = h }
func (p *GatePane) SetVM(vm adapters.GateViewModel) { p.vm = vm }
