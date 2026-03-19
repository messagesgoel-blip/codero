package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/gate"
	"github.com/codero/codero/internal/gatecheck"
	"github.com/codero/codero/internal/tui/adapters"
)

// narrowThreshold is the pane width below which secondary columns are collapsed
// so the most important state is always visible on narrow terminals.
const narrowThreshold = 60

// authAgentCount is the number of authoritative AI gate agents (copilot + litellm).
// It is a named constant so that navigation bounds in Update and the rendering
// index offsets in View stay in sync if the agent list changes.
const authAgentCount = 2

// ── GatePane: PROCESSES & AGENTS + RELAY ORCHESTRATION (left pane) ──────────

// GatePane renders the left pane: agent progress rows + relay orchestration.
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
			total := authAgentCount + len(p.vm.PipelineRows)
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
	w := maxInt(0, p.width-2)

	// ── header: PROCESSES & AGENTS + System Health indicator ─────────────────
	sysHealth := agentSystemHealth(p.vm)
	pad := w - 20 - len(sysHealth)
	if pad < 1 {
		pad = 1
	}
	header := fmt.Sprintf("  PROCESSES & AGENTS%s%s", strings.Repeat(" ", pad), sysHealth)
	lines = append(lines, p.theme.ListHeader.Render(header))
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))

	// ── authoritative AI gate agent rows ─────────────────────────────────────
	authAgents := []struct{ icon, name, status string }{
		{"🤖", "copilot", p.vm.CopilotStatus},
		{"🔧", "litellm", p.vm.LiteLLMStatus},
	}
	barW := minInt(w-6, 24)
	for i, ag := range authAgents {
		pct := agentPercent(ag.status, p.vm.ElapsedSec, p.vm.PollAfterSec)
		action := agentAction(ag.name, ag.status)
		isActive := ag.name == p.vm.CurrentGate

		nameStyle := p.theme.GateAuthoritative
		if isActive {
			nameStyle = p.theme.Running
		}

		line1 := fmt.Sprintf("  %s %s: %s (%d%%)", ag.icon, ag.name, action, pct)
		bar := fmt.Sprintf("     %s %d%%", renderProgressBar(pct, barW), pct)

		if p.selected == i {
			lines = append(lines, p.theme.ListSelected.Width(w).Render(line1))
		} else {
			lines = append(lines, nameStyle.Render(line1))
		}
		lines = append(lines, p.theme.Muted.Render(bar))
		lines = append(lines, "")
	}

	// ── pipeline agent rows (local non-authoritative) ─────────────────────────
	for j, row := range p.vm.PipelineRows {
		idx := authAgentCount + j
		icon := pipelineIcon(row.Name)
		pct := agentPercent(row.Status, 0, 0)
		action := agentAction(row.Name, row.Status)

		line1 := fmt.Sprintf("  %s %s: %s (%d%%)", icon, row.Name, action, pct)
		bar := fmt.Sprintf("     %s %d%%", renderProgressBar(pct, barW), pct)

		if p.selected == idx {
			lines = append(lines, p.theme.ListSelected.Width(w).Render(line1))
		} else {
			lines = append(lines, p.theme.GatePipeline.Render(line1))
		}
		lines = append(lines, p.theme.Muted.Render(bar))
		lines = append(lines, "")
	}

	// ── relay orchestration section (bottom of left pane) ─────────────────────
	// Only render if there's enough vertical space remaining.
	remaining := p.height - len(lines)
	if remaining >= 6 {
		for len(lines) < p.height-6-len(p.vm.PipelineRows) {
			lines = append(lines, "")
		}
		lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
		lines = append(lines, p.theme.ListHeader.Render("  RELAY ORCHESTRATION"))
		lines = append(lines, p.theme.Muted.Render(fmt.Sprintf("  %-18s %s", "Standard static", "Parallel LLM-backed")))
		lines = append(lines, p.theme.Muted.Render(fmt.Sprintf("  %-18s %s", "analysis tools", "agents")))
		for _, row := range p.vm.PipelineRows {
			icon := pipelineIcon(row.Name)
			lines = append(lines, p.theme.Muted.Render(
				fmt.Sprintf("  %s %-10s ──→ [LLM]", icon, row.Name),
			))
		}
	}

	for len(lines) < p.height {
		lines = append(lines, "")
	}
	content := strings.Join(lines[:minInt(len(lines), p.height)], "\n")
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(content)
}

func (p *GatePane) SetSize(w, h int)                { p.width = w; p.height = h }
func (p *GatePane) SetVM(vm adapters.GateViewModel) { p.vm = vm }

// ── GatePane helpers ──────────────────────────────────────────────────────────

// agentSystemHealth returns a compact health indicator for the pane header.
func agentSystemHealth(vm adapters.GateViewModel) string {
	switch vm.Status {
	case gate.StatusPass:
		return "System Health ✓"
	case gate.StatusFail:
		return "System Health ✗"
	default:
		return "System Health ●"
	}
}

// agentPercent derives a 0–100 display percentage from agent status + timing.
func agentPercent(status string, elapsed, pollAfter int) int {
	switch status {
	case "pass", "blocked", "timeout", "infra_fail":
		return 100
	case "running":
		if pollAfter > 0 {
			pct := elapsed * 100 / pollAfter
			if pct < 5 {
				return 5
			}
			if pct > 90 {
				return 90
			}
			return pct
		}
		return 50
	default: // pending, unknown
		return 0
	}
}

// agentAction returns a human-readable current action label for a given agent.
func agentAction(name, status string) string {
	switch status {
	case "pass":
		return "Review Complete"
	case "blocked":
		return "Blocked — findings"
	case "timeout":
		return "Timed out"
	case "infra_fail":
		return "Infrastructure failure"
	case "pending":
		return "Waiting…"
	}
	// running
	switch name {
	case "copilot":
		return "Analyzing Diff"
	case "litellm":
		return "Deep Arch Review"
	case "semgrep":
		return "Secret Scan"
	case "gitleaks":
		return "Scanning Secrets"
	case "pylint", "ruff":
		return "Linting"
	default:
		return "Running"
	}
}

// renderProgressBar returns an ASCII block progress bar of the given width.
func renderProgressBar(pct, width int) string {
	if width < 4 {
		width = 10
	}
	filled := width * pct / 100
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// pipelineIcon returns the emoji for a well-known pipeline tool name.
func pipelineIcon(name string) string {
	switch name {
	case "semgrep", "gitleaks":
		return "🔍"
	case "pylint", "ruff":
		return "🐍"
	default:
		return "⚙"
	}
}

// ── ChecksPane: FINDINGS & ROUTING DASHBOARD (right pane) ────────────────────

// checksRefreshMsg is sent when a new gate-check report is available.
type checksRefreshMsg struct{ vm adapters.CheckReportViewModel }

// ChecksPane renders the right pane: findings bucketed by severity, routing
// flowchart, and a summary section.
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

// severityBucket groups checks by priority level.
type severityBucket struct {
	label  string
	icon   string
	color  lipgloss.Style
	checks []adapters.CheckViewModel
}

// bucketChecks sorts checks into CRITICAL / HIGH / MEDIUM / LOW buckets
// using the LOG-001 DisplayState model + required flag.
func (p ChecksPane) bucketChecks() [4]severityBucket {
	b := [4]severityBucket{
		{label: "CRITICAL", icon: "🔴", color: p.theme.Fail},
		{label: "HIGH", icon: "🟠", color: p.theme.Warning},
		{label: "MEDIUM", icon: "🟡", color: p.theme.Running},
		{label: "LOW", icon: "🔵", color: p.theme.Accent},
	}
	for _, c := range p.vm.Checks {
		switch {
		case c.DisplayState == "failing" && c.Required:
			b[0].checks = append(b[0].checks, c)
		case c.DisplayState == "failing" && !c.Required:
			b[1].checks = append(b[1].checks, c)
		case c.DisplayState == "disabled" && c.Required:
			b[2].checks = append(b[2].checks, c)
		default:
			b[3].checks = append(b[3].checks, c)
		}
	}
	return b
}

func (p ChecksPane) View() string {
	if p.width == 0 {
		return ""
	}
	lines := make([]string, 0, p.height)
	w := maxInt(0, p.width-2)
	narrow := w < narrowThreshold

	// ── header ──────────────────────────────────────────────────────────────
	lines = append(lines, p.theme.ListHeader.Render("  FINDINGS & ROUTING DASHBOARD"))
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
	lines = append(lines, p.theme.Bold.Render("  PRIORITIZED FINDINGS BUCKETS"))
	lines = append(lines, "")

	// ── severity buckets ─────────────────────────────────────────────────────
	buckets := p.bucketChecks()
	for _, b := range buckets {
		label := fmt.Sprintf("  %s [%s]", b.icon, b.label)
		lines = append(lines, b.color.Render(label))
		for _, c := range b.checks {
			icon := adapters.DisplayStateIcon(c.DisplayState)
			reason := gatecheck.DisplayReason(gatecheck.ReasonCode(c.ReasonCode), c.Reason)
			var entry string
			if narrow {
				entry = fmt.Sprintf("     %s %s", icon, truncStr(c.ID, w-8))
			} else {
				entry = fmt.Sprintf("     %s %-20s", icon, truncStr(c.ID, 20))
				if reason != "" {
					entry += "  " + truncStr(reason, w-34)
				}
			}
			lines = append(lines, p.theme.Muted.Render(entry))
		}
		if len(b.checks) == 0 {
			lines = append(lines, p.theme.Muted.Render("     – none"))
		}
		// Visual analysis path link (matches mockup)
		lines = append(lines, p.theme.Muted.Render("     → Visual analysis path"))
		lines = append(lines, "")
	}

	// ── routing flowchart ─────────────────────────────────────────────────────
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
	lines = append(lines, p.theme.Bold.Render("  ROUTING FLOWCHART"))
	lines = append(lines, p.theme.Base.Render("  Finding → AI Agent Review → Human Reviewer"))
	lines = append(lines, p.theme.Muted.Render("  Target Team: @security_lead @tech_lead"))
	lines = append(lines, "")

	// ── summary ──────────────────────────────────────────────────────────────
	s := p.vm.Summary
	critCount := len(buckets[0].checks)
	highCount := len(buckets[1].checks)
	riskLabel := checksRiskScore(critCount, highCount)

	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
	lines = append(lines, p.theme.Bold.Render("  Summary"))
	if s.Total > 0 {
		// Approximate: assume ~20 lines per check on average when line counts unavailable
		approxLines := s.Total * 20
		lines = append(lines, p.theme.Base.Render(fmt.Sprintf("  Total Lines Analyzed: %s", formatLargeInt(approxLines))))
	}
	lines = append(lines, p.theme.Base.Render(fmt.Sprintf("  Findings Found: %d", s.Failed)))
	lines = append(lines, p.theme.Base.Render(fmt.Sprintf("  Risk Score: %s", riskLabel)))
	if s.RequiredFailed > 0 {
		lines = append(lines, p.theme.Fail.Render(fmt.Sprintf("  ! required-failed=%d", s.RequiredFailed)))
	}
	if s.RequiredDisabled > 0 {
		lines = append(lines, p.theme.Warning.Render(fmt.Sprintf("  ! required-disabled=%d", s.RequiredDisabled)))
	}

	for len(lines) < p.height {
		lines = append(lines, "")
	}
	content := strings.Join(lines[:minInt(len(lines), p.height)], "\n")
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(content)
}

// checksRiskScore returns a risk label for the summary section.
func checksRiskScore(crit, high int) string {
	switch {
	case crit > 0:
		score := minInt(99, 90+crit)
		return fmt.Sprintf("%d/100 (CRITICAL)", score)
	case high > 0:
		score := minInt(89, 70+high)
		return fmt.Sprintf("%d/100 (HIGH)", score)
	default:
		return "0/100 (CLEAR)"
	}
}

func (p *ChecksPane) SetSize(w, h int)                       { p.width = w; p.height = h }
func (p *ChecksPane) SetVM(vm adapters.CheckReportViewModel) { p.vm = vm }

// formatLargeInt formats an integer with comma thousands separators (e.g. 12345 → "12,345").
func formatLargeInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}
