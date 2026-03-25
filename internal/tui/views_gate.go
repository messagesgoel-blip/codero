package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/dashboard"
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
	sessions []dashboard.ActiveSession
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
			total := authAgentCount + len(p.vm.PipelineRows) + len(p.sessions)
			if p.selected < total-1 {
				p.selected++
			}
		}
	case gateRefreshMsg:
		p.vm = msg.vm
	case activeSessionsRefreshMsg:
		p.sessions = msg.sessions
	}
	return p, nil
}

func (p GatePane) View() string {
	if p.width <= 2 || p.height <= 0 {
		return ""
	}
	lines := make([]string, 0, p.height)
	w := p.width - 2

	// ── header: PROCESSES & AGENTS + System Health indicator ─────────────────
	sysHealth := agentSystemHealth(p.vm)
	headerTitle := p.theme.PaneHeader.Render("PROCESSES & AGENTS")
	headerHealth := agentSystemHealthStyle(p.theme, p.vm).Render(sysHealth)
	pad := w - lipgloss.Width(headerTitle) - lipgloss.Width(headerHealth)
	if pad < 0 {
		pad = 0
	}
	header := headerTitle + strings.Repeat(" ", pad) + headerHealth
	lines = append(lines, header)
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
	lines = append(lines, "")

	// ── authoritative AI gate agent rows ─────────────────────────────────────
	authAgents := []struct{ icon, name, status string }{
		{"🤖", "copilot", p.vm.CopilotStatus},
		{"🔧", "litellm", p.vm.LiteLLMStatus},
	}
	barW := minInt(w-10, 20)
	for i, ag := range authAgents {
		pct := agentPercent(ag.status, p.vm.ElapsedSec, p.vm.PollAfterSec)
		action := agentAction(ag.name, ag.status)
		isActive := ag.name == p.vm.CurrentGate

		nameStyle := p.theme.GateAuthoritative
		if isActive {
			nameStyle = p.theme.Running
		}

		line1 := fmt.Sprintf("  %s %-10s %s", ag.icon, ag.name, p.theme.Muted.Render(action))
		bar := fmt.Sprintf("     %s %3d%%", renderProgressBar(pct, barW), pct)

		if p.selected == i {
			lines = append(lines, p.theme.ListSelected.Width(w).Render(line1))
		} else {
			lines = append(lines, nameStyle.Render(line1))
		}
		lines = append(lines, bar)
		lines = append(lines, "")
	}

	// ── pipeline agent rows (local non-authoritative) ─────────────────────────
	for j, row := range p.vm.PipelineRows {
		idx := authAgentCount + j
		icon := pipelineIcon(row.Name)
		pct := agentPercent(row.Status, 0, 0)
		action := agentAction(row.Name, row.Status)

		line1 := fmt.Sprintf("  %s %-10s %s", icon, row.Name, p.theme.Muted.Render(action))
		bar := fmt.Sprintf("     %s %3d%%", renderProgressBar(pct, barW), pct)

		if p.selected == idx {
			lines = append(lines, p.theme.ListSelected.Width(w).Render(line1))
		} else {
			lines = append(lines, p.theme.GatePipeline.Render(line1))
		}
		lines = append(lines, bar)
		lines = append(lines, "")
	}

	// ── live sessions sourced from the canonical active-sessions view ────────
	if len(p.sessions) > 0 {
		lines = append(lines, p.theme.PaneHeader.Width(w).Render("LIVE SESSIONS"))
		lines = append(lines, p.theme.Muted.Render(" agent / branch / elapsed"))
		lines = append(lines, "")

		for j, session := range p.sessions {
			idx := authAgentCount + len(p.vm.PipelineRows) + j
			stateLabel := strings.ToUpper(session.ActivityState)
			stateStyle := p.activityStateStyle(session.ActivityState)
			line1 := fmt.Sprintf("  ● %-12s %s", truncStr(session.AgentID, 12), stateStyle.Render(stateLabel))

			elapsed := adapters.ElapsedLabel(int(session.ElapsedSec))
			target := truncStr(session.Branch, maxInt(8, w-18))
			if target == "" {
				target = truncStr(session.Repo, maxInt(8, w-18))
			}
			line2 := fmt.Sprintf("     %-8s %s", elapsed, target)
			if session.PRNumber > 0 {
				line2 += fmt.Sprintf("  #%d", session.PRNumber)
			}

			if p.selected == idx {
				lines = append(lines, p.theme.ListSelected.Width(w).Render(line1))
			} else {
				lines = append(lines, p.theme.Base.Render(line1))
			}
			lines = append(lines, p.theme.Muted.Render(line2))
			lines = append(lines, "")
		}
	}

	// ── relay orchestration section (bottom of left pane) ─────────────────────
	// Only render if there's enough vertical space remaining.
	remaining := p.height - len(lines)
	if remaining >= 6 {
		for len(lines) < p.height-6 {
			lines = append(lines, "")
		}
		lines = append(lines, p.theme.PaneHeader.Width(w).Render("RELAY ORCHESTRATION"))
		lines = append(lines, p.theme.Muted.Render(" Static Tools      AI Agents"))
		for _, row := range p.vm.PipelineRows[:minInt(len(p.vm.PipelineRows), 3)] {
			icon := pipelineIcon(row.Name)
			lines = append(lines, p.theme.Muted.Render(
				fmt.Sprintf(" %s %-12s ──→ [LLM]", icon, truncStr(row.Name, 12)),
			))
		}
	}

	for len(lines) < p.height {
		lines = append(lines, "")
	}
	content := strings.Join(lines[:minInt(len(lines), p.height)], "\n")
	return lipgloss.NewStyle().Width(p.width).Render(content)
}

func (p *GatePane) SetSize(w, h int)                { p.width = w; p.height = h }
func (p *GatePane) SetVM(vm adapters.GateViewModel) { p.vm = vm }

func (p GatePane) activityStateStyle(activity string) lipgloss.Style {
	switch activity {
	case "blocked", "stalled":
		return p.theme.Fail
	case "waiting", "queued":
		return p.theme.Warning
	case "reviewing", "running":
		return p.theme.Running
	case "merge_ready", "merged", "done":
		return p.theme.Pass
	default:
		return p.theme.Accent
	}
}

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

func agentSystemHealthStyle(t Theme, vm adapters.GateViewModel) lipgloss.Style {
	switch vm.Status {
	case gate.StatusPass:
		return t.Pass
	case gate.StatusFail:
		return t.Fail
	default:
		return t.Running
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
	if p.width <= 2 || p.height <= 0 {
		return ""
	}
	lines := make([]string, 0, p.height)
	w := p.width - 2
	narrow := w < narrowThreshold

	// ── header ──────────────────────────────────────────────────────────────
	lines = append(lines, p.theme.PaneHeader.Width(w).Render("FINDINGS & ROUTING DASHBOARD"))
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
	lines = append(lines, p.theme.Bold.Render("  PRIORITIZED BUCKETS"))
	lines = append(lines, "")

	// ── severity buckets ─────────────────────────────────────────────────────
	buckets := p.bucketChecks()
	for _, b := range buckets {
		label := fmt.Sprintf("  %s %s", b.icon, b.label)
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
					entry += "  " + p.theme.Muted.Render(truncStr(reason, w-34))
				}
			}
			lines = append(lines, entry)
		}
		if len(b.checks) == 0 {
			lines = append(lines, p.theme.Muted.Render("     – none"))
		}
		// Visual analysis path link (matches mockup)
		lines = append(lines, p.theme.Muted.Render("     → Visual analysis path"))
		lines = append(lines, "")
	}

	// ── routing flowchart ─────────────────────────────────────────────────────
	remaining := p.height - len(lines)
	if remaining >= 8 {
		lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
		lines = append(lines, p.theme.Bold.Render("  ROUTING FLOWCHART"))
		lines = append(lines, p.theme.Base.Render("  Finding → AI Agent Review → Human"))
		lines = append(lines, p.theme.Muted.Render("  Target: @security @tech"))
		lines = append(lines, "")

		// ── summary ──────────────────────────────────────────────────────────────
		s := p.vm.Summary
		critCount := len(buckets[0].checks)
		highCount := len(buckets[1].checks)
		riskLabel := checksRiskScore(critCount, highCount)

		lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", w)))
		lines = append(lines, p.theme.Bold.Render("  Summary"))
		lines = append(lines, p.theme.Base.Render(fmt.Sprintf("  Findings Found: %d", s.Failed)))

		riskStyle := p.theme.Running
		if critCount > 0 {
			riskStyle = p.theme.Fail
		} else if highCount > 0 {
			riskStyle = p.theme.Warning
		} else if s.Failed == 0 {
			riskStyle = p.theme.Pass
		}

		lines = append(lines, p.theme.Base.Render("  Risk Score: ")+riskStyle.Render(riskLabel))
		// Estimated lines analyzed (heuristic: ~500 lines per check)
		estLines := s.Total * 500
		lines = append(lines, p.theme.Base.Render(fmt.Sprintf("  Est. Lines Analyzed: ~%d", estLines)))
	}

	for len(lines) < p.height {
		lines = append(lines, "")
	}
	content := strings.Join(lines[:minInt(len(lines), p.height)], "\n")
	return lipgloss.NewStyle().Width(p.width).Render(content)
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
