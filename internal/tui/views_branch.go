package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codero/codero/internal/state"
)

// BranchPane renders the branch context section of the left sidebar.
type BranchPane struct {
	record *state.BranchRecord
	theme  Theme
	width  int
	height int
}

func NewBranchPane(theme Theme) BranchPane {
	return BranchPane{theme: theme}
}

func (p BranchPane) Init() tea.Cmd { return nil }

func (p BranchPane) Update(msg tea.Msg) (BranchPane, tea.Cmd) {
	if m, ok := msg.(branchRefreshMsg); ok {
		p.record = m.record
	}
	return p, nil
}

func (p BranchPane) View() string {
	if p.width == 0 {
		return ""
	}
	lines := make([]string, 0, 8)
	lines = append(lines, p.theme.ListHeader.Render("  BRANCH"))
	lines = append(lines, p.theme.Muted.Render(strings.Repeat("─", p.width-2)))

	if p.record == nil {
		lines = append(lines, p.theme.Muted.Render("  —"))
	} else {
		r := p.record
		short := r.HeadHash
		if len(short) > 8 {
			short = short[:8]
		}
		lines = append(lines, p.theme.Base.Render(fmt.Sprintf("  %s", truncStr(r.Branch, p.width-4))))
		lines = append(lines, p.theme.Muted.Render(fmt.Sprintf("  %s", r.Repo)))
		lines = append(lines, "")
		lines = append(lines, p.theme.Muted.Render(fmt.Sprintf("  state:   %s", string(r.State))))
		lines = append(lines, p.theme.Muted.Render(fmt.Sprintf("  commit:  %s", short)))
		lines = append(lines, p.theme.Muted.Render(fmt.Sprintf("  retries: %d/%d", r.RetryCount, r.MaxRetries)))
		if r.Approved {
			lines = append(lines, p.theme.Pass.Render("  ✓ approved"))
		}
		if r.CIGreen {
			lines = append(lines, p.theme.Pass.Render("  ✓ CI green"))
		}
	}

	for len(lines) < p.height {
		lines = append(lines, "")
	}
	content := strings.Join(lines[:minInt(len(lines), p.height)], "\n")
	return lipgloss.NewStyle().Width(p.width).Height(p.height).Render(content)
}

func (p *BranchPane) SetSize(w, h int) { p.width = w; p.height = h }
func (p *BranchPane) SetRecord(r *state.BranchRecord) { p.record = r }
