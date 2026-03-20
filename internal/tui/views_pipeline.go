package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/tui/adapters"
)

var pipelineDefaultSteps = []string{
	"init", "schema-validate", "secret-scan", "lint-staged", "type-check",
	"unit-tests", "integration", "coverage-check", "dependency-audit",
	"license-scan", "size-check", "conflict-markers", "whitespace",
	"newline-eof", "branch-protection", "review-approval", "ci-status",
	"deploy-preview", "smoke-test", "security-scan", "changelog", "merge-ready",
}

// PipelinePane renders the pipeline progress column from the current gate report.
type PipelinePane struct {
	vm       adapters.CheckReportViewModel
	theme    Theme
	width    int
	height   int
	activeIx int
}

func NewPipelinePane(theme Theme) PipelinePane {
	return PipelinePane{theme: theme, activeIx: 0}
}

func (p PipelinePane) Init() tea.Cmd { return nil }

func (p PipelinePane) Update(msg tea.Msg) (PipelinePane, tea.Cmd) {
	switch msg.(type) {
	case tickMsg:
		if len(p.vm.Checks) == 0 && len(pipelineDefaultSteps) > 0 {
			p.activeIx = (p.activeIx + 1) % len(pipelineDefaultSteps)
		}
	case checksRefreshMsg:
		p.activeIx = -1
	}
	return p, nil
}

func (p *PipelinePane) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *PipelinePane) SetVM(vm adapters.CheckReportViewModel) {
	p.vm = vm
	if len(vm.Checks) > 0 {
		p.activeIx = -1
	}
}

func (p PipelinePane) View() string {
	if p.width <= 2 || p.height <= 0 {
		return ""
	}
	w := p.width - 2
	lines := make([]string, 0, p.height)

	lines = append(lines, p.theme.PaneHeader.Width(w).Render("PIPELINE"))
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))

	steps := p.pipelineSteps()
	passCount := 0
	failCount := 0
	for _, step := range steps {
		switch step.status {
		case "pass":
			passCount++
		case "fail":
			failCount++
		}
	}

	summary := fmt.Sprintf("  %d steps", len(steps))
	if passCount > 0 || failCount > 0 {
		summary += fmt.Sprintf("  %d✓", passCount)
		if failCount > 0 {
			summary += fmt.Sprintf("  %d✕", failCount)
		}
	}
	lines = append(lines, "")
	lines = append(lines, p.theme.Muted.Render(summary))
	lines = append(lines, "")

	for i, step := range steps {
		lines = append(lines, p.renderStep(step, i == p.activeIx, i < p.activeIx, w))
	}

	if len(steps) == 0 {
		lines = append(lines, p.theme.Muted.Render("  (waiting…)"))
	}

	lines = append(lines, "")
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
	lines = append(lines, p.theme.Muted.Render("  ✓ Pass ● ✕ Fail"))
	lines = append(lines, p.theme.Muted.Render("  ↷ Skip ● ○ Idle"))

	for len(lines) < p.height {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(p.width).Render(strings.Join(lines[:minInt(len(lines), p.height)], "\n"))
}

type pipelineStep struct {
	name     string
	status   string
	duration int64
}

func (p PipelinePane) pipelineSteps() []pipelineStep {
	if len(p.vm.Checks) == 0 {
		steps := make([]pipelineStep, len(pipelineDefaultSteps))
		for i, name := range pipelineDefaultSteps {
			status := "idle"
			if i == p.activeIx {
				status = "running"
			}
			steps[i] = pipelineStep{name: name, status: status}
		}
		return steps
	}
	steps := make([]pipelineStep, 0, len(p.vm.Checks))
	for _, c := range p.vm.Checks {
		steps = append(steps, pipelineStep{
			name:     c.Name,
			status:   c.Status,
			duration: c.DurMS,
		})
	}
	return steps
}

func (p PipelinePane) renderStep(step pipelineStep, isActive, isPast bool, width int) string {
	icon := "○"
	style := p.theme.Muted
	switch step.status {
	case "pass":
		icon = "✓"
		style = p.theme.Pass
	case "fail":
		icon = "✕"
		style = p.theme.Fail
	case "skip":
		icon = "↷"
		style = p.theme.Warning
	case "disabled":
		icon = "–"
		style = p.theme.Disabled
	case "running":
		icon = "●"
		style = p.theme.Running
	}
	if isActive && step.status == "idle" {
		icon = "●"
		style = p.theme.Running
	}

	nameW := width - 4
	if step.duration > 0 {
		nameW -= 6 // space for duration
	}
	if nameW < 4 {
		nameW = 4
	}

	label := fmt.Sprintf(" %s %-*s", icon, nameW, truncStr(step.name, nameW))
	if step.duration > 0 {
		label += fmt.Sprintf(" %3dms", step.duration)
	}

	if isActive {
		return p.theme.ListSelected.Width(width).Render(label)
	}
	if isPast {
		style = style.Copy().Faint(true)
	}
	return style.Render(label)
}
