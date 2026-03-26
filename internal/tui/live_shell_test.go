package tui

import (
	"strings"
	"testing"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui/adapters"
)

func TestGatePane_RendersLiveSessions(t *testing.T) {
	pane := NewGatePane(DefaultTheme)
	pane.SetSize(40, 24)
	pane.SetVM(AdapterFromPath(t.TempDir()))

	updated, _ := pane.Update(activeSessionsRefreshMsg{
		sessions: []dashboard.ActiveSession{
			{
				SessionID:     "sess-1",
				AgentID:       "codex",
				Branch:        "feat/UI-001",
				ActivityState: "reviewing",
				ElapsedSec:    125,
				PRNumber:      108,
			},
		},
	})

	view := updated.View()
	for _, want := range []string{"LIVE SESSIONS", "codex", "feat/UI-001", "#108"} {
		if !strings.Contains(view, want) {
			t.Fatalf("gate pane missing %q\nview:\n%s", want, view)
		}
	}
}

func TestMergeStatusLabel_PrefersBlockReasons(t *testing.T) {
	m := Model{
		theme:        DefaultTheme,
		blockReasons: []dashboard.BlockReason{{Source: "semgrep", Count: 2}},
		checksVM:     adapters.CheckReportViewModel{Summary: adapters.CheckSummaryViewModel{Failed: 4}},
		branchRecord: &state.BranchRecord{State: state.StateMergeReady},
	}

	got := mergeStatusLabel(m)
	if !strings.Contains(got, "merge blocked: semgrep (2)") {
		t.Fatalf("mergeStatusLabel() = %q, want block reason summary", got)
	}
}

func TestMergeStatusLabel_FallsBackToMergeReady(t *testing.T) {
	m := Model{
		theme:        DefaultTheme,
		branchRecord: &state.BranchRecord{State: state.StateMergeReady},
	}

	got := mergeStatusLabel(m)
	if !strings.Contains(got, "merge ready") {
		t.Fatalf("mergeStatusLabel() = %q, want merge ready", got)
	}
}
