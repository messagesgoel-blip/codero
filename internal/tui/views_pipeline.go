package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/tui/adapters"
)

var pipelineKanbanColumns = []string{
	"SUBMITTED",
	"GATING",
	"COMMITTED",
	"PUSHED",
	"PR_ACTIVE",
	"MONITORING",
	"MERGE_READY",
	"MERGED",
}

var pipelineKanbanColumnGroups = [][]string{
	{"SUBMITTED", "GATING", "COMMITTED", "PUSHED"},
	{"PR_ACTIVE", "MONITORING", "MERGE_READY", "MERGED"},
}

// PipelinePane renders the delivery pipeline kanban.
type PipelinePane struct {
	vm     adapters.CheckReportViewModel
	cards  []pipelineCard
	theme  Theme
	width  int
	height int
}

func NewPipelinePane(theme Theme) PipelinePane {
	return PipelinePane{theme: theme}
}

func (p PipelinePane) Init() tea.Cmd { return nil }

func (p PipelinePane) Update(msg tea.Msg) (PipelinePane, tea.Cmd) {
	switch msg.(type) {
	case checksRefreshMsg:
		// Preserve the legacy gate view model for compatibility, but the board is
		// driven by delivery cards when available.
	}
	return p, nil
}

func (p *PipelinePane) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *PipelinePane) SetVM(vm adapters.CheckReportViewModel) {
	p.vm = vm
}

func (p *PipelinePane) SetCards(cards []pipelineCard) {
	p.cards = append([]pipelineCard(nil), cards...)
	sort.SliceStable(p.cards, func(i, j int) bool {
		if p.cards[i].Checkpoint != p.cards[j].Checkpoint {
			return p.cards[i].Checkpoint < p.cards[j].Checkpoint
		}
		if p.cards[i].Branch != p.cards[j].Branch {
			return p.cards[i].Branch < p.cards[j].Branch
		}
		return p.cards[i].AgentID < p.cards[j].AgentID
	})
}

func (p PipelinePane) View() string {
	if p.width <= 2 || p.height <= 0 {
		return ""
	}

	contentW := maxInt(0, p.width-2)
	contentH := maxInt(0, p.height-2)
	header := p.theme.PaneHeader.Width(contentW).Render("PIPELINE KANBAN")
	sep := p.theme.Muted.Render(strings.Repeat("─", contentW))
	bodyH := maxInt(2, contentH-2)
	groupH := maxInt(4, bodyH/2)
	if len(pipelineKanbanColumnGroups) == 1 {
		groupH = bodyH
	}

	buckets := p.cardsByStage()
	rows := []string{header, sep}
	for groupIdx, group := range pipelineKanbanColumnGroups {
		if groupIdx > 0 {
			rows = append(rows, p.theme.Muted.Render(strings.Repeat("─", contentW)))
		}
		cols := make([]string, 0, len(group))
		gap := 2
		colW := maxInt(10, (contentW-(gap*(len(group)-1)))/len(group))
		for _, stage := range group {
			cols = append(cols, p.renderKanbanColumn(stage, buckets[stage], colW, groupH))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cols...))
	}

	if len(p.cards) == 0 {
		rows = append(rows, "")
		rows = append(rows, p.theme.Muted.Render("  No active assignments"))
	}

	for len(rows) < p.height {
		rows = append(rows, "")
	}
	return lipgloss.NewStyle().Width(p.width).Render(strings.Join(rows[:minInt(len(rows), p.height)], "\n"))
}

func (p PipelinePane) cardsByStage() map[string][]pipelineCard {
	buckets := make(map[string][]pipelineCard, len(pipelineKanbanColumns))
	for _, stage := range pipelineKanbanColumns {
		buckets[stage] = nil
	}
	for _, card := range p.cards {
		stage := strings.ToUpper(strings.TrimSpace(card.Checkpoint))
		if stage == "" {
			stage = "SUBMITTED"
		}
		if _, ok := buckets[stage]; !ok {
			stage = "SUBMITTED"
		}
		buckets[stage] = append(buckets[stage], card)
	}
	for _, stage := range pipelineKanbanColumns {
		sort.SliceStable(buckets[stage], func(i, j int) bool {
			if buckets[stage][i].StageSec != buckets[stage][j].StageSec {
				return buckets[stage][i].StageSec > buckets[stage][j].StageSec
			}
			if buckets[stage][i].Branch != buckets[stage][j].Branch {
				return buckets[stage][i].Branch < buckets[stage][j].Branch
			}
			return buckets[stage][i].AgentID < buckets[stage][j].AgentID
		})
	}
	return buckets
}

func (p PipelinePane) renderKanbanColumn(stage string, cards []pipelineCard, width, height int) string {
	if width < 10 {
		width = 10
	}
	lines := make([]string, 0, height)
	lines = append(lines, p.theme.ListHeader.Width(width).Render(fmt.Sprintf(" %s ", stage)))
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", width)))
	if len(cards) == 0 {
		lines = append(lines, p.theme.Muted.Render("  · idle"))
	} else {
		for _, card := range cards {
			lines = append(lines, p.renderPipelineCard(card, width))
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(strings.Join(lines[:minInt(len(lines), height)], "\n"))
}

func (p PipelinePane) renderPipelineCard(card pipelineCard, width int) string {
	branch := truncStr(card.Branch, maxInt(6, width-10))
	agent := truncStr(card.AgentID, maxInt(6, width-10))
	dur := time.Duration(card.StageSec) * time.Second
	label := fmt.Sprintf("  %s %s v%d %s", agent, branch, card.Version, dur.Truncate(time.Second))
	return p.theme.Base.Render(label)
}
