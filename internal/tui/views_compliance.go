package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ComplianceRuleRow holds the display model for a compliance rule (§2.9).
type ComplianceRuleRow struct {
	RuleID      string
	RuleVersion int
	Description string
	Enforcement string // "blocking", "warning", "info"
}

// ComplianceCheckRow holds the display model for a compliance check result.
type ComplianceCheckRow struct {
	CheckID      string
	AssignmentID string
	SessionID    string
	RuleID       string
	Result       string // "pass", "fail", "pending"
	Violation    bool
	CheckedAt    time.Time
	ResolvedBy   string
}

// ComplianceViewModel is the combined compliance display model (§2.9).
type ComplianceViewModel struct {
	Rules      []ComplianceRuleRow
	Checks     []ComplianceCheckRow
	Violations int
}

// complianceRefreshMsg delivers compliance data to the pane.
type complianceRefreshMsg struct{ vm ComplianceViewModel }

// CompliancePane renders rules, checks, and violations (§2.9).
type CompliancePane struct {
	vm       ComplianceViewModel
	viewport viewport.Model
	theme    Theme
	width    int
	height   int
	ready    bool
}

// NewCompliancePane creates a compliance pane.
func NewCompliancePane(theme Theme) CompliancePane {
	return CompliancePane{theme: theme}
}

func (p CompliancePane) Init() tea.Cmd { return nil }

func (p CompliancePane) Update(msg tea.Msg) (CompliancePane, tea.Cmd) {
	if m, ok := msg.(complianceRefreshMsg); ok {
		p.vm = m.vm
		p.refreshViewport()
	}
	if p.ready {
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p *CompliancePane) refreshViewport() {
	if !p.ready {
		return
	}
	var sb strings.Builder

	sb.WriteString(p.theme.Accent.Render("  COMPLIANCE DASHBOARD") + "\n")
	sb.WriteString(p.theme.Muted.Render("  ────────────────────────────────────────") + "\n")

	// Violations summary
	if p.vm.Violations > 0 {
		sb.WriteString(p.theme.Fail.Render(fmt.Sprintf("  ⚠ %d active violation(s)", p.vm.Violations)) + "\n\n")
	} else {
		sb.WriteString(p.theme.Pass.Render("  ✓ No active violations") + "\n\n")
	}

	// Rules
	sb.WriteString(p.theme.Accent.Render("  RULES") + "\n")
	if len(p.vm.Rules) == 0 {
		sb.WriteString(p.theme.Muted.Render("  (no rules configured)") + "\n")
	} else {
		header := fmt.Sprintf("  %-14s %-4s %-12s %s", "RULE", "VER", "ENFORCEMENT", "DESCRIPTION")
		sb.WriteString(p.theme.Muted.Render(header) + "\n")
		for _, r := range p.vm.Rules {
			enfStyle := p.enforcementStyle(r.Enforcement)
			sb.WriteString(fmt.Sprintf("  %-14s v%-3d %-12s %s\n",
				p.theme.Base.Render(r.RuleID),
				r.RuleVersion,
				enfStyle.Render(r.Enforcement),
				p.theme.Muted.Render(truncStr(r.Description, p.width-40))))
		}
	}

	// Recent checks
	sb.WriteString(p.theme.Accent.Render("\n  RECENT CHECKS") + "\n")
	if len(p.vm.Checks) == 0 {
		sb.WriteString(p.theme.Muted.Render("  (no checks recorded)") + "\n")
	} else {
		header := fmt.Sprintf("  %-12s %-14s %-8s %-8s %s",
			"ASSIGNMENT", "RULE", "RESULT", "VIOL", "CHECKED")
		sb.WriteString(p.theme.Muted.Render(header) + "\n")
		for _, c := range p.vm.Checks {
			resultStyle := p.resultCheckStyle(c.Result)
			violStr := "—"
			if c.Violation {
				violStr = p.theme.Fail.Render("YES")
			}
			sb.WriteString(fmt.Sprintf("  %-12s %-14s %-8s %-8s %s\n",
				truncStr(c.AssignmentID, 12),
				c.RuleID,
				resultStyle.Render(c.Result),
				violStr,
				p.theme.Muted.Render(c.CheckedAt.Format("15:04:05"))))
		}
	}

	p.viewport.SetContent(sb.String())
}

func (p CompliancePane) enforcementStyle(e string) lipgloss.Style {
	switch e {
	case "blocking":
		return p.theme.Fail
	case "warning":
		return p.theme.Warning
	default:
		return p.theme.Muted
	}
}

func (p CompliancePane) resultCheckStyle(r string) lipgloss.Style {
	switch r {
	case "pass":
		return p.theme.Pass
	case "fail":
		return p.theme.Fail
	case "pending":
		return p.theme.Warning
	default:
		return p.theme.Base
	}
}

func (p CompliancePane) View() string {
	if !p.ready {
		return p.theme.Muted.Render("  Loading compliance…")
	}
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(p.viewport.View())
}

func (p *CompliancePane) SetSize(w, h int) {
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
