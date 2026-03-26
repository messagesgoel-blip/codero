package tui

import (
	"context"
	"errors"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui/adapters"
)

type activeSessionsRefreshMsg struct{ sessions []dashboard.ActiveSession }
type blockReasonsRefreshMsg struct{ reasons []dashboard.BlockReason }

func (m Model) loadLiveShellCmds() tea.Cmd {
	return tea.Batch(
		m.loadGateChecksCmd(),
		m.loadBranchCmd(),
		m.loadQueueCmd(),
		m.loadEventsCmd(),
		m.loadActiveSessionsCmd(),
		m.loadBlockReasonsCmd(),
	)
}

func (m Model) loadGateChecksCmd() tea.Cmd {
	return func() tea.Msg {
		report, _, err := dashboard.LoadGateCheckReport(m.cfg.RepoPath)
		if err != nil {
			return errMsg{err: err}
		}
		if report == nil {
			return checksRefreshMsg{}
		}
		return checksRefreshMsg{vm: adapters.FromCheckReport(*report)}
	}
}

func (m Model) loadBranchCmd() tea.Cmd {
	return func() tea.Msg {
		if m.cfg.StateDB == nil || m.cfg.Repo == "" || m.cfg.Branch == "" {
			return branchRefreshMsg{}
		}
		record, err := state.GetBranch(m.cfg.StateDB, m.cfg.Repo, m.cfg.Branch)
		if err != nil {
			if errors.Is(err, state.ErrBranchNotFound) {
				return branchRefreshMsg{}
			}
			return errMsg{err: err}
		}
		return branchRefreshMsg{record: record}
	}
}

func (m Model) loadQueueCmd() tea.Cmd {
	return func() tea.Msg {
		if m.cfg.StateDB == nil {
			return queueRefreshMsg{}
		}
		branches, err := state.ListActiveBranches(m.cfg.StateDB)
		if err != nil {
			return errMsg{err: err}
		}
		items := adapters.FromBranchRecords(filterBranchesForRepo(branches, m.cfg.Repo))
		return queueRefreshMsg{items: items}
	}
}

func (m Model) loadEventsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.cfg.StateDB == nil || m.cfg.Repo == "" || m.cfg.Branch == "" {
			return eventsRefreshMsg{}
		}
		events, err := state.ListDeliveryEvents(m.cfg.StateDB, m.cfg.Repo, m.cfg.Branch, 0)
		if err != nil {
			return errMsg{err: err}
		}
		return eventsRefreshMsg{events: tailDeliveryEvents(events, 50)}
	}
}

func (m Model) loadActiveSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.cfg.StateDB == nil {
			return activeSessionsRefreshMsg{}
		}
		sessions, err := dashboard.LoadActiveSessions(contextOrBackground(m.cfg.Context), m.cfg.StateDB.Unwrap(), 8)
		if err != nil {
			return errMsg{err: err}
		}
		return activeSessionsRefreshMsg{sessions: sessions}
	}
}

func (m Model) loadBlockReasonsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.cfg.StateDB == nil {
			return blockReasonsRefreshMsg{}
		}
		reasons, err := dashboard.LoadBlockReasons(contextOrBackground(m.cfg.Context), m.cfg.StateDB.Unwrap())
		if err != nil {
			return errMsg{err: err}
		}
		return blockReasonsRefreshMsg{reasons: reasons}
	}
}

func filterBranchesForRepo(branches []state.BranchRecord, repo string) []state.BranchRecord {
	if repo == "" {
		sort.SliceStable(branches, func(i, j int) bool {
			if branches[i].QueuePriority != branches[j].QueuePriority {
				return branches[i].QueuePriority > branches[j].QueuePriority
			}
			return branches[i].UpdatedAt.After(branches[j].UpdatedAt)
		})
		return branches
	}

	filtered := make([]state.BranchRecord, 0, len(branches))
	for _, branch := range branches {
		if branch.Repo == repo {
			filtered = append(filtered, branch)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].QueuePriority != filtered[j].QueuePriority {
			return filtered[i].QueuePriority > filtered[j].QueuePriority
		}
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	return filtered
}

func tailDeliveryEvents(events []state.DeliveryEvent, limit int) []state.DeliveryEvent {
	if limit <= 0 || len(events) <= limit {
		return events
	}
	return append([]state.DeliveryEvent(nil), events[len(events)-limit:]...)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}
