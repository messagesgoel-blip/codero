package dashboard

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codero/codero/internal/config"
	"github.com/codero/codero/internal/state"
)

const runtimeActivityWindow = 3 * time.Minute
const runtimeRecentActivityThreshold = 90 * time.Second
const runtimeStartingWindow = 45 * time.Second

type recentRuntimeActivity struct {
	RuntimeBytes int64
	OutputBytes  int64
	OutputLines  int64
	ToolCalls    int64
	FileWrites   int64
	DiffChanges  int64
	ProcEvents   int64
}

type runtimeProjection struct {
	SessionID               string
	AgentID                 string
	Family                  string
	LaunchMode              string
	Mode                    string
	Repo                    string
	Branch                  string
	Worktree                string
	PRNumber                int
	OwnerAgent              string
	Task                    *ActiveTask
	AttachmentState         string
	AttributionSource       string
	AttributionConfidence   string
	LifecycleState          string
	ActivityState           string
	StartedAt               time.Time
	LastHeartbeatAt         time.Time
	LastActivityAt          *time.Time
	ProgressAt              *time.Time
	LastIOAt                *time.Time
	ElapsedSec              int64
	WorkingDurationSec      int64
	IdleDurationSec         int64
	OutputMB                float64
	ContextPressure         string
	CompactCount            int
	InferredStatus          string
	InferredStatusUpdatedAt *time.Time
	TmuxSessionName         string
	Status                  string
	EndedAt                 *time.Time
	EndReason               string
}

func loadRecentRuntimeActivity(ctx context.Context, db *sql.DB, window time.Duration) (map[string]recentRuntimeActivity, error) {
	out := make(map[string]recentRuntimeActivity)
	hasActivity, err := tableExists(ctx, db, "session_activity")
	if err != nil {
		return nil, fmt.Errorf("loadRecentRuntimeActivity: check session_activity: %w", err)
	}
	if !hasActivity {
		return out, nil
	}

	cutoff := time.Now().UTC().Add(-window).Format("2006-01-02T15:04")
	rows, err := db.QueryContext(ctx, `
		SELECT session_id,
		       CASE WHEN COUNT(*) = 1 THEN MAX(runtime_bytes) ELSE MAX(runtime_bytes) - MIN(runtime_bytes) END AS runtime_bytes,
		       CASE WHEN COUNT(*) = 1 THEN MAX(output_bytes) ELSE MAX(output_bytes) - MIN(output_bytes) END AS output_bytes,
		       CASE WHEN COUNT(*) = 1 THEN MAX(output_lines) ELSE MAX(output_lines) - MIN(output_lines) END AS output_lines,
		       CASE WHEN COUNT(*) = 1 THEN MAX(tool_calls) ELSE MAX(tool_calls) - MIN(tool_calls) END AS tool_calls,
		       CASE WHEN COUNT(*) = 1 THEN MAX(file_writes) ELSE MAX(file_writes) - MIN(file_writes) END AS file_writes,
		       CASE WHEN COUNT(*) = 1 THEN MAX(diff_changes) ELSE MAX(diff_changes) - MIN(diff_changes) END AS diff_changes,
		       CASE WHEN COUNT(*) = 1 THEN MAX(proc_events) ELSE MAX(proc_events) - MIN(proc_events) END AS proc_events
		FROM session_activity
		WHERE bucket >= ?
		GROUP BY session_id`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("loadRecentRuntimeActivity: query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			sessionID string
			sample    recentRuntimeActivity
		)
		if err := rows.Scan(
			&sessionID,
			&sample.RuntimeBytes,
			&sample.OutputBytes,
			&sample.OutputLines,
			&sample.ToolCalls,
			&sample.FileWrites,
			&sample.DiffChanges,
			&sample.ProcEvents,
		); err != nil {
			return nil, fmt.Errorf("loadRecentRuntimeActivity: scan: %w", err)
		}
		out[sessionID] = sample
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loadRecentRuntimeActivity: rows: %w", err)
	}
	return out, nil
}

func buildRuntimeProjection(
	ctx context.Context,
	db *sql.DB,
	s sessionRow,
	assignment activeAssignment,
	recent recentRuntimeActivity,
) runtimeProjection {
	now := time.Now().UTC()
	startedAt := startedAtForSession(
		sql.NullTime{Time: s.StartedAt, Valid: !s.StartedAt.IsZero()},
		sql.NullTime{},
		sql.NullTime{Time: s.LastSeenAt, Valid: !s.LastSeenAt.IsZero()},
	)
	elapsed := time.Since(startedAt)
	if elapsed < 0 {
		elapsed = 0
	}

	canonicalRepo := strings.TrimSpace(assignment.Repo)
	if canonicalRepo == "" {
		canonicalRepo = strings.TrimSpace(s.SessionRepo)
	}
	canonicalBranch := strings.TrimSpace(assignment.Branch)
	if canonicalBranch == "" {
		canonicalBranch = strings.TrimSpace(s.SessionBranch)
	}

	attachmentState := resolveRuntimeAttachmentState(s, assignment, canonicalRepo, canonicalBranch)
	attributionSource, attributionConfidence := resolveRuntimeAttribution(s, assignment, canonicalRepo, canonicalBranch)
	lastActivityAt := runtimeLastActivityAt(s.LastProgressAt, s.LastIOAt)
	activityState := resolveRuntimeActivityState(now, s, assignment, recent, attachmentState, lastActivityAt)
	lifecycleState := resolveRuntimeLifecycleState(now, s, attachmentState, activityState, lastActivityAt)

	task := resolveTaskFromAssignment(assignment.TaskID, canonicalBranch)
	if task != nil && task.Phase == "" {
		task.Phase = activityState
	}

	prNumber := 0
	if canonicalRepo != "" && canonicalBranch != "" {
		prNumber = lookupPRNumber(ctx, db, canonicalRepo, canonicalBranch)
	}

	workingSec, idleSec := resolveRuntimeDurations(elapsed, s.InferredStatus, s.InferredStatusUpdatedAt)

	return runtimeProjection{
		SessionID:               s.SessionID,
		AgentID:                 s.AgentID,
		Family:                  resolveRuntimeFamily(s.AgentID),
		LaunchMode:              resolveRuntimeLaunchMode(s, assignment),
		Mode:                    s.Mode,
		Repo:                    canonicalRepo,
		Branch:                  canonicalBranch,
		Worktree:                assignment.Worktree,
		PRNumber:                prNumber,
		OwnerAgent:              resolveOwnerAgent(s.AgentID, canonicalBranch),
		Task:                    task,
		AttachmentState:         attachmentState,
		AttributionSource:       attributionSource,
		AttributionConfidence:   attributionConfidence,
		LifecycleState:          lifecycleState,
		ActivityState:           activityState,
		StartedAt:               startedAt,
		LastHeartbeatAt:         s.LastSeenAt,
		LastActivityAt:          lastActivityAt,
		ProgressAt:              nullTimePtr(s.LastProgressAt),
		LastIOAt:                nullTimePtr(s.LastIOAt),
		ElapsedSec:              int64(elapsed.Seconds()),
		WorkingDurationSec:      workingSec,
		IdleDurationSec:         idleSec,
		OutputMB:                sessionOutputMB(s.SessionID, s.OutputBytes),
		ContextPressure:         s.ContextPressure,
		CompactCount:            s.CompactCount,
		InferredStatus:          s.InferredStatus,
		InferredStatusUpdatedAt: nullTimePtr(s.InferredStatusUpdatedAt),
		TmuxSessionName:         s.TmuxSessionName,
		Status:                  resolveRuntimeStatus(s.EndedAt),
		EndedAt:                 nullTimePtr(s.EndedAt),
		EndReason:               s.EndReason,
	}
}

func resolveRuntimeFamily(agentID string) string {
	if kind := config.InferAgentKind(agentID, ""); kind != "" {
		return kind
	}
	return "unknown"
}

func resolveRuntimeLaunchMode(s sessionRow, assignment activeAssignment) string {
	switch {
	case strings.TrimSpace(s.TmuxSessionName) != "":
		return "detached_tmux"
	case strings.TrimSpace(assignment.Worktree) != "":
		return "wrapped"
	default:
		return "external"
	}
}

func resolveRuntimeAttachmentState(s sessionRow, assignment activeAssignment, canonicalRepo, canonicalBranch string) string {
	switch {
	case assignment.Repo != "" && assignment.Branch != "":
		return "attached"
	case canonicalRepo != "" || canonicalBranch != "" || assignment.Worktree != "" || assignment.TaskID != "":
		return "inferred"
	default:
		return "orphaned"
	}
}

func resolveRuntimeAttribution(s sessionRow, assignment activeAssignment, canonicalRepo, canonicalBranch string) (string, string) {
	source := strings.TrimSpace(s.AttributionSource)
	confidence := strings.TrimSpace(s.AttributionConfidence)

	if canonicalRepo != "" || canonicalBranch != "" {
		if source == "" || source == state.AttributionSourceUnknown {
			if assignment.Repo != "" || assignment.Branch != "" {
				source = state.AttributionSourceAssignmentState
			} else {
				source = state.AttributionSourceLaunchContext
			}
		}
		if confidence == "" || confidence == state.AttributionConfidenceUnknown {
			confidence = state.AttributionConfidenceForSource(source)
		}
		return source, confidence
	}

	if assignment.Worktree != "" || assignment.TaskID != "" {
		return state.AttributionSourceLaunchContext, state.AttributionConfidenceLow
	}
	return state.AttributionSourceUnresolved, state.AttributionConfidenceLow
}

func runtimeLastActivityAt(progressAt, lastIOAt sql.NullTime) *time.Time {
	var latest time.Time
	switch {
	case progressAt.Valid && lastIOAt.Valid:
		if progressAt.Time.After(lastIOAt.Time) {
			latest = progressAt.Time
		} else {
			latest = lastIOAt.Time
		}
	case progressAt.Valid:
		latest = progressAt.Time
	case lastIOAt.Valid:
		latest = lastIOAt.Time
	default:
		return nil
	}
	t := latest.UTC()
	return &t
}

func resolveRuntimeActivityState(
	now time.Time,
	s sessionRow,
	assignment activeAssignment,
	recent recentRuntimeActivity,
	attachmentState string,
	lastActivityAt *time.Time,
) string {
	if s.EndedAt.Valid {
		if s.EndReason == "lost" || s.EndReason == "expired" || s.EndReason == "stuck_abandoned" {
			return "failed"
		}
		return "completed"
	}
	if attachmentState == "orphaned" && assignment.Worktree == "" && s.SessionRepo == "" && s.SessionBranch == "" {
		return "orphaned"
	}

	substatus := strings.ToLower(strings.TrimSpace(assignment.Substatus))
	switch {
	case strings.HasPrefix(substatus, "blocked_"):
		return "blocked"
	case substatus == state.AssignmentSubstatusWaitingForCI || substatus == state.AssignmentSubstatusWaitingForMergeApproval:
		return "syncing"
	}

	switch s.InferredStatus {
	case state.InferredStatusWaitingForInput:
		return "waiting_input"
	case state.InferredStatusIdle:
		if assignment.Worktree == "" && s.SessionRepo == "" && s.SessionBranch == "" {
			return "idle"
		}
	}

	if lastActivityAt == nil {
		if now.Sub(s.StartedAt) <= runtimeStartingWindow {
			return "starting"
		}
		if s.InferredStatus == state.InferredStatusWorking {
			return "thinking"
		}
		return "idle"
	}

	if now.Sub(*lastActivityAt) <= runtimeRecentActivityThreshold {
		switch {
		case recent.FileWrites > 0 || recent.DiffChanges > 0:
			return "editing"
		case recent.ProcEvents > 0:
			return "running_command"
		case recent.ToolCalls > 0 || recent.OutputLines > 0 || recent.OutputBytes > 0 || recent.RuntimeBytes > 0 || s.InferredStatus == state.InferredStatusWorking:
			return "thinking"
		}
	}

	if s.InferredStatus == state.InferredStatusWorking {
		return "thinking"
	}
	return "idle"
}

func resolveRuntimeLifecycleState(
	now time.Time,
	s sessionRow,
	attachmentState string,
	activityState string,
	lastActivityAt *time.Time,
) string {
	if s.EndedAt.Valid {
		if s.EndReason == "lost" || s.EndReason == "expired" || s.EndReason == "stuck_abandoned" {
			return "failed"
		}
		return "finalized"
	}
	if s.LastRecoveredAt.Valid && !s.LastSeenAt.After(s.LastRecoveredAt.Time) {
		return "recovered"
	}
	if attachmentState == "orphaned" {
		return "orphaned"
	}
	if activityState == "blocked" || activityState == "syncing" {
		return "blocked"
	}
	if lastActivityAt == nil && now.Sub(s.StartedAt) <= runtimeStartingWindow {
		return "registered"
	}
	if attachmentState == "attached" {
		return "active"
	}
	if attachmentState == "inferred" {
		return "attributed"
	}
	return "registered"
}

func resolveRuntimeDurations(elapsed time.Duration, inferredStatus string, updatedAt sql.NullTime) (int64, int64) {
	if elapsed < 0 {
		elapsed = 0
	}
	if !updatedAt.Valid {
		return int64(elapsed.Seconds()), 0
	}

	statusDuration := time.Since(updatedAt.Time).Seconds()
	if statusDuration < 0 {
		statusDuration = 0
	}

	var workingSec, idleSec int64
	switch inferredStatus {
	case state.InferredStatusIdle, state.InferredStatusWaitingForInput:
		idleSec = int64(statusDuration)
		workingSec = int64(elapsed.Seconds()) - idleSec
	case state.InferredStatusWorking:
		workingSec = int64(elapsed.Seconds())
	default:
		workingSec = int64(elapsed.Seconds())
	}
	if workingSec < 0 {
		workingSec = 0
	}
	if idleSec < 0 {
		idleSec = 0
	}
	return workingSec, idleSec
}

func resolveRuntimeStatus(endedAt sql.NullTime) string {
	if endedAt.Valid {
		return "ended"
	}
	return "active"
}

func sessionOutputMB(sessionID string, outputBytes int64) float64 {
	if outputBytes > 0 {
		return float64(outputBytes) / (1024 * 1024)
	}

	safeID := filepath.Base(sessionID)
	if safeID != sessionID || safeID == "." || safeID == ".." || strings.ContainsAny(safeID, `/\`) {
		return 0
	}

	tailPath := filepath.Join(os.TempDir(), "codero-tails", safeID+".log")
	if d := os.Getenv("CODERO_TAIL_DIR"); d != "" {
		tailPath = filepath.Join(d, safeID+".log")
	}
	if stat, err := os.Stat(tailPath); err == nil && !stat.IsDir() {
		return float64(stat.Size()) / (1024 * 1024)
	}
	return 0
}
