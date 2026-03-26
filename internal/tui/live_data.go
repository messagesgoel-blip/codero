package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codero/codero/internal/dashboard"
	"github.com/codero/codero/internal/state"
	"github.com/codero/codero/internal/tui/adapters"
)

type activeSessionsRefreshMsg struct {
	sessions      []dashboard.ActiveSession
	overviewRows  []overviewSessionRow
	pipelineCards []pipelineCard
	health        missionControlHealth
}
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
			return activeSessionsRefreshMsg{health: missionControlHealth{
				Status:       "unknown",
				RedisStatus:  "unknown",
				GitHubStatus: "unknown",
			}}
		}
		ctx := contextOrBackground(m.cfg.Context)
		sqlDB := m.cfg.StateDB.Unwrap()
		sessions, err := dashboard.LoadActiveSessions(ctx, sqlDB, 8)
		if err != nil {
			return errMsg{err: err}
		}
		msg := activeSessionsRefreshMsg{sessions: sessions}
		msg.overviewRows = buildOverviewRows(ctx, m.cfg.StateDB, sessions)
		msg.pipelineCards = buildPipelineCards(ctx, m.cfg.StateDB, sessions)
		msg.health = missionControlHealth{
			Status:       "unknown",
			RedisStatus:  "unknown",
			GitHubStatus: "unknown",
		}
		if m.cfg.DaemonBaseURL != "" {
			if health, err := fetchDaemonHealth(ctx, m.cfg.DaemonBaseURL); err == nil {
				msg.health = health
			} else {
				msg.health.Status = "error"
				msg.health.RedisStatus = "error"
			}
		}
		if gh, err := fetchGitHubConnectivity(m.cfg.SettingsDir); err == nil {
			msg.health.GitHubStatus = gh
		}
		return msg
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

func (m Model) loadSelectedSessionDetailCmd(row overviewSessionRow) tea.Cmd {
	return func() tea.Msg {
		if m.cfg.StateDB == nil || row.Session.SessionID == "" {
			return sessionDrillMsg{detail: SessionDetail{SessionID: row.Session.SessionID, Checkpoint: row.Checkpoint}}
		}
		ctx := contextOrBackground(m.cfg.Context)
		sqlDB := m.cfg.StateDB
		sessionRow, err := state.GetAgentSession(ctx, sqlDB, row.Session.SessionID)
		if err != nil {
			if errors.Is(err, state.ErrAgentSessionNotFound) {
				return sessionDrillMsg{detail: SessionDetail{SessionID: row.Session.SessionID, Checkpoint: row.Checkpoint}}
			}
			return errMsg{err: err}
		}
		assignments, err := state.ListAgentAssignments(ctx, sqlDB, row.Session.SessionID)
		if err != nil {
			return errMsg{err: err}
		}
		var currentAssignment *state.AgentAssignment
		if active, err := state.GetActiveAgentAssignment(ctx, sqlDB, row.Session.SessionID); err == nil {
			currentAssignment = active
		}
		detail := SessionDetail{
			SessionID:  row.Session.SessionID,
			AgentID:    row.Session.AgentID,
			Mode:       row.Session.Mode,
			Status:     "active",
			TmuxName:   sessionRow.TmuxSessionName,
			Checkpoint: row.Checkpoint,
			StartedAt:  sessionRow.StartedAt,
			LastSeenAt: sessionRow.LastSeenAt,
			PRNumber:   row.Session.PRNumber,
		}
		if sessionRow.EndedAt != nil {
			detail.EndedAt = sessionRow.EndedAt
			detail.EndReason = sessionRow.EndReason
			detail.Status = "ended"
		}
		if detail.AgentID == "" {
			detail.AgentID = sessionRow.AgentID
		}
		if detail.Mode == "" {
			detail.Mode = sessionRow.Mode
		}
		if detail.StartedAt.IsZero() {
			detail.StartedAt = row.Session.StartedAt
		}
		if detail.LastSeenAt.IsZero() {
			detail.LastSeenAt = row.Session.LastHeartbeatAt
		}
		detail.Assignments = make([]SessionAssignmentRow, 0, len(assignments))
		for _, assignment := range assignments {
			row := SessionAssignmentRow{
				AssignmentID: assignment.ID,
				Repo:         assignment.Repo,
				Branch:       assignment.Branch,
				TaskID:       assignment.TaskID,
				State:        assignment.State,
				Substatus:    assignment.Substatus,
				Version:      assignment.Version,
				StartedAt:    assignment.StartedAt,
				EndedAt:      assignment.EndedAt,
			}
			detail.Assignments = append(detail.Assignments, row)
		}
		if currentAssignment != nil {
			detail.GateSummary = fmt.Sprintf("%s / %s", currentAssignment.State, currentAssignment.Substatus)
		}
		detail.Timeline = buildSessionTimeline(&detail)
		return sessionDrillMsg{detail: detail}
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

func buildOverviewRows(ctx context.Context, db *state.DB, sessions []dashboard.ActiveSession) []overviewSessionRow {
	rows := make([]overviewSessionRow, 0, len(sessions))
	for _, session := range sessions {
		stage, _ := resolveSessionPipelineStage(ctx, db, session)
		rows = append(rows, overviewSessionRow{
			Session:         session,
			Checkpoint:      stage,
			HeartbeatAgeSec: int64(time.Since(session.LastHeartbeatAt).Seconds()),
		})
	}
	return rows
}

func buildPipelineCards(ctx context.Context, db *state.DB, sessions []dashboard.ActiveSession) []pipelineCard {
	cards := make([]pipelineCard, 0, len(sessions))
	for _, session := range sessions {
		stage, assignment := resolveSessionPipelineStage(ctx, db, session)
		if assignment == nil {
			continue
		}
		stageDur := time.Since(assignment.StartedAt)
		if stageDur < 0 {
			stageDur = 0
		}
		cards = append(cards, pipelineCard{
			SessionID:  session.SessionID,
			AgentID:    session.AgentID,
			Branch:     session.Branch,
			Checkpoint: stage,
			Version:    assignment.Version,
			StageSec:   int64(stageDur.Seconds()),
		})
	}
	return cards
}

func resolveSessionPipelineStage(ctx context.Context, db *state.DB, session dashboard.ActiveSession) (string, *state.AgentAssignment) {
	if db == nil {
		return normalizeStageName(session.ActivityState), nil
	}
	assignment, err := state.GetActiveAgentAssignment(ctx, db, session.SessionID)
	if err != nil {
		return normalizeStageName(session.ActivityState), nil
	}
	branchState := state.StateSubmitted
	if assignment.Repo != "" && assignment.Branch != "" {
		if branch, branchErr := state.GetBranch(db, assignment.Repo, assignment.Branch); branchErr == nil {
			branchState = branch.State
		}
	}
	stage := stageForAssignment(branchState, assignment.Substatus, assignment.State, session.PRNumber)
	if stage == "" {
		stage = normalizeStageName(session.ActivityState)
	}
	return stage, assignment
}

func normalizeStageName(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "submitted":
		return "SUBMITTED"
	case "waiting":
		return "SUBMITTED"
	case "queued_cli":
		return "GATING"
	case "cli_reviewing":
		return "COMMITTED"
	case "review_approved":
		return "PUSHED"
	case "merge_ready":
		return "MERGE_READY"
	case "merged":
		return "MERGED"
	case "blocked":
		return "MONITORING"
	case "":
		return "SUBMITTED"
	default:
		return "GATING"
	}
}

func stageForAssignment(branchState state.State, substatus, assignmentState string, prNumber int) string {
	switch branchState {
	case state.StateSubmitted, state.StateWaiting:
		return "SUBMITTED"
	case state.StateQueuedCLI:
		return "GATING"
	case state.StateCLIReviewing:
		return "COMMITTED"
	case state.StateReviewApproved:
		switch substatus {
		case state.AssignmentSubstatusWaitingForCI:
			return "PUSHED"
		case state.AssignmentSubstatusWaitingForMergeApproval:
			return "PR_ACTIVE"
		default:
			if prNumber > 0 {
				return "MONITORING"
			}
			return "COMMITTED"
		}
	case state.StateMergeReady:
		return "MERGE_READY"
	case state.StateMerged:
		return "MERGED"
	case state.StateBlocked, state.StateStale, state.StateAbandoned, state.StateExpired:
		switch substatus {
		case state.AssignmentSubstatusWaitingForCI:
			return "PUSHED"
		case state.AssignmentSubstatusWaitingForMergeApproval:
			return "PR_ACTIVE"
		default:
			return "MONITORING"
		}
	}
	switch substatus {
	case state.AssignmentSubstatusWaitingForCI:
		return "PUSHED"
	case state.AssignmentSubstatusWaitingForMergeApproval:
		return "PR_ACTIVE"
	case state.AssignmentSubstatusInProgress, state.AssignmentSubstatusNeedsRevision:
		return "GATING"
	case state.AssignmentSubstatusTerminalFinished, state.AssignmentSubstatusTerminalWaitingComments, state.AssignmentSubstatusTerminalWaitingNextTask:
		return "MERGED"
	case state.AssignmentSubstatusBlockedCredentialFailure, state.AssignmentSubstatusBlockedMergeConflict, state.AssignmentSubstatusBlockedExternalDependency, state.AssignmentSubstatusBlockedCIFailure, state.AssignmentSubstatusBlockedPolicy:
		return "MONITORING"
	default:
		if strings.EqualFold(assignmentState, "completed") {
			return "MERGED"
		}
		if strings.EqualFold(assignmentState, "blocked") {
			return "MONITORING"
		}
		return "SUBMITTED"
	}
}

func fetchDaemonHealth(ctx context.Context, baseURL string) (missionControlHealth, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/health", nil)
	if err != nil {
		return missionControlHealth{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return missionControlHealth{}, err
	}
	defer resp.Body.Close()
	var payload struct {
		Status        string  `json:"status"`
		UptimeSeconds float64 `json:"uptime_seconds"`
		Redis         string  `json:"redis"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return missionControlHealth{}, err
	}
	health := missionControlHealth{
		Status:      payload.Status,
		UptimeSec:   int64(payload.UptimeSeconds),
		RedisStatus: payload.Redis,
	}
	if health.Status == "" {
		health.Status = "unknown"
	}
	return health, nil
}

func fetchGitHubConnectivity(settingsDir string) (string, error) {
	store := dashboard.NewSettingsStore(settingsDir)
	settings, err := store.Load()
	if err != nil {
		return "", err
	}
	for _, integration := range settings.Integrations {
		if strings.EqualFold(integration.ID, "gh-actions") {
			if integration.Connected {
				return "connected", nil
			}
			return "disconnected", nil
		}
	}
	return "unknown", nil
}
