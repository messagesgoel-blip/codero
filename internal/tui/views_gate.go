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

func (p *GatePane) SetSize(w, h int)                { p.width = w; p.height = h }
func (p *GatePane) SetVM(vm adapters.GateViewModel) { p.vm = vm }

// checksRefreshMsg is sent when a new gate-check report is available.
type checksRefreshMsg struct{ vm adapters.CheckReportViewModel }

// ChecksPane renders a panel showing all gate-check results from the local
// engine run. Disabled/skipped rows are always shown.
type ChecksPane struct {
vm       adapters.CheckReportViewModel
selected int
theme    Theme
width    int
height   int
}

// NewChecksPane creates a ChecksPane with the given theme.
func NewChecksPane(theme Theme) ChecksPane {
return ChecksPane{theme: theme}
}

func (p ChecksPane) Init() tea.Cmd { return nil }

func (p ChecksPane) Update(msg tea.Msg) (ChecksPane, tea.Cmd) {
switch msg := msg.(type) {
case tea.KeyMsg:
switch msg.String() {
case "up", "k":
if p.selected > 0 {
p.selected--
}
case "down", "j":
if p.selected < len(p.vm.Checks)-1 {
p.selected++
}
}
case checksRefreshMsg:
p.vm = msg.vm
p.selected = 0
}
return p, nil
}

func (p ChecksPane) View() string {
if p.width == 0 {
return ""
}
lines := make([]string, 0, p.height)
w := p.width - 2

lines = append(lines, p.theme.ListHeader.Render("  GATE CHECKS"))
lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))

// Summary counters row
s := p.vm.Summary
counters := fmt.Sprintf("  pass=%d  fail=%d  skip=%d  infra=%d  disabled=%d  [%s]",
s.Passed, s.Failed, s.Skipped, s.InfraBypassed, s.Disabled, s.Profile)
if s.Overall == "FAIL" {
lines = append(lines, p.theme.Fail.Render(counters))
} else {
lines = append(lines, p.theme.Pass.Render(counters))
}
lines = append(lines, "")

// Per-check rows grouped by status prominence
for i, c := range p.vm.Checks {
icon := adapters.StatusIcon(c.Status)
req := ""
if c.Required {
req = " *"
}
label := fmt.Sprintf("  %s %-22s  %-12s  %-6s%s", icon, c.ID, c.Group, c.Status, req)
if c.Reason != "" {
label += "  " + c.Reason
}

var line string
if p.selected == i {
line = p.theme.ListSelected.Width(w).Render(label)
} else {
switch c.Status {
case "FAIL":
line = p.theme.Fail.Render(label)
case "INFRA_BYPASS":
line = p.theme.GatePipeline.Render(label)
case "DISABLED", "SKIP":
line = p.theme.Muted.Render(label)
default:
line = p.theme.Pass.Render(label)
}
}
lines = append(lines, line)
}

for len(lines) < p.height {
lines = append(lines, "")
}
content := strings.Join(lines[:minInt(len(lines), p.height)], "\n")
return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(content)
}

func (p *ChecksPane) SetSize(w, h int)                          { p.width = w; p.height = h }
func (p *ChecksPane) SetVM(vm adapters.CheckReportViewModel)    { p.vm = vm }
