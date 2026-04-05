package dashboard

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codero/codero/internal/gatecheck"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/scheduler"
)

// gateCheckReportPath returns the path to the last gate-check report file.
// It honours CODERO_GATE_CHECK_REPORT_PATH and falls back to
// gatecheck.DefaultReportPath when the variable is unset.
func gateCheckReportPath() string {
	if p := os.Getenv("CODERO_GATE_CHECK_REPORT_PATH"); p != "" {
		return p
	}
	return gatecheck.DefaultReportPath
}

const activeSessionStatesSQL = "('submitted', 'waiting', 'queued_cli', 'cli_reviewing', 'review_approved', 'merge_ready', 'blocked', 'expired')"

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	row := db.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`,
		table,
	)
	var exists int
	if err := row.Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func scanTimeValue(value any) (time.Time, bool, error) {
	switch v := value.(type) {
	case nil:
		return time.Time{}, false, nil
	case time.Time:
		return v, true, nil
	case string:
		return parseTimeString(v)
	case []byte:
		return parseTimeString(string(v))
	default:
		return time.Time{}, false, fmt.Errorf("unsupported time value type %T", value)
	}
}

func parseTimeString(value string) (time.Time, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("unsupported time format %q", value)
}

// queryOverview returns today's aggregate run stats.
func queryOverview(ctx context.Context, db *sql.DB) (runsToday, passedToday int, blockedCount int, avgGateSec float64, err error) {
	// runs today + passed today
	row := db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END)
		FROM review_runs
		WHERE DATE(created_at) = DATE('now')`)
	var passed sql.NullInt64
	if err = row.Scan(&runsToday, &passed); err != nil {
		return
	}
	passedToday = int(passed.Int64)

	// blocked branches
	if err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM branch_states WHERE state = 'blocked'`).Scan(&blockedCount); err != nil {
		return
	}

	// avg gate time for completed runs today
	var avg sql.NullFloat64
	if err = db.QueryRowContext(ctx, `
		SELECT AVG(
			CAST((julianday(finished_at) - julianday(started_at)) * 86400 AS REAL)
		)
		FROM review_runs
		WHERE status = 'completed'
		  AND DATE(created_at) = DATE('now')
		  AND started_at IS NOT NULL
		  AND finished_at IS NOT NULL`).Scan(&avg); err != nil {
		return
	}
	if avg.Valid {
		avgGateSec = avg.Float64
	} else {
		avgGateSec = -1
	}
	return
}

// querySparkline7d returns the last 7 days of daily run stats.
func querySparkline7d(ctx context.Context, db *sql.DB) ([]DayStats, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			DATE(created_at)                                        AS day,
			COUNT(*)                                                AS total,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END)  AS passed
		FROM review_runs
		WHERE created_at >= DATE('now', '-6 days')
		GROUP BY DATE(created_at)
		ORDER BY day ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DayStats
	for rows.Next() {
		var d DayStats
		if err := rows.Scan(&d.Date, &d.Total, &d.Passed); err != nil {
			return nil, err
		}
		d.Failed = d.Total - d.Passed
		out = append(out, d)
	}
	return out, rows.Err()
}

// queryRepos returns the latest branch-state summary per repo.
func queryRepos(ctx context.Context, db *sql.DB) ([]RepoSummary, error) {
	// Latest branch record per repo (by updated_at).
	rows, err := db.QueryContext(ctx, `
		SELECT b.repo, b.branch, b.state, b.head_hash, b.updated_at, COALESCE(b.pr_number, 0)
		FROM branch_states b
		INNER JOIN (
			SELECT repo, MAX(updated_at) AS max_upd
			FROM branch_states
			GROUP BY repo
		) latest ON b.repo = latest.repo AND b.updated_at = latest.max_upd
		ORDER BY b.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RepoSummary
	for rows.Next() {
		var s RepoSummary
		if err := rows.Scan(&s.Repo, &s.Branch, &s.State, &s.HeadHash, &s.UpdatedAt, &s.PRNumber); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Enrich with last run info and gate summary.
	for i := range out {
		enrichRepoSummary(ctx, db, &out[i])
	}
	return out, nil
}

// enrichRepoSummary adds last_run_status and gate_summary to a RepoSummary.
func enrichRepoSummary(ctx context.Context, db *sql.DB, s *RepoSummary) {
	// Last run status + time.
	row := db.QueryRowContext(ctx, `
		SELECT status, finished_at
		FROM review_runs
		WHERE repo = ?
		ORDER BY created_at DESC
		LIMIT 1`, s.Repo)
	var status string
	var finAt sql.NullTime
	if err := row.Scan(&status, &finAt); err == nil {
		s.LastRunStatus = status
		if finAt.Valid {
			t := finAt.Time
			s.LastRunAt = &t
		}
	}

	// Gate pills: aggregate provider outcomes for this repo's last few runs.
	rows, err := db.QueryContext(ctx, `
		SELECT provider, status
		FROM review_runs
		WHERE repo = ?
		ORDER BY created_at DESC
		LIMIT 6`, s.Repo)
	if err != nil {
		return
	}
	defer rows.Close()

	seen := map[string]string{}
	for rows.Next() {
		var prov, st string
		if rows.Scan(&prov, &st) == nil {
			if _, exists := seen[prov]; !exists {
				seen[prov] = statusToPillState(st)
			}
		}
	}
	for name, pillState := range seen {
		s.GateSummary = append(s.GateSummary, GatePill{Name: name, Status: pillState})
	}
}

func statusToPillState(status string) string {
	switch status {
	case "completed":
		return "pass"
	case "failed":
		return "fail"
	case "running":
		return "run"
	default:
		return "idle"
	}
}

// queryActivity returns the most recent delivery events.
func queryActivity(ctx context.Context, db *sql.DB, limit int) ([]ActivityEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, repo, branch, event_type, payload, created_at
		FROM delivery_events
		ORDER BY created_at DESC, seq DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ActivityEvent
	for rows.Next() {
		var e ActivityEvent
		if err := rows.Scan(&e.Seq, &e.Repo, &e.Branch, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// queryBlockReasons returns ranked error sources from findings.
func queryBlockReasons(ctx context.Context, db *sql.DB) ([]BlockReason, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT source, COUNT(*) AS cnt
		FROM findings
		WHERE severity = 'error'
		GROUP BY source
		ORDER BY cnt DESC
		LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BlockReason
	for rows.Next() {
		var b BlockReason
		if err := rows.Scan(&b.Source, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// queryGateHealth returns per-provider pass rates across all runs.
func queryGateHealth(ctx context.Context, db *sql.DB) ([]GateHealth, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			provider,
			COUNT(*)                                               AS total,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS passed
		FROM review_runs
		GROUP BY provider
		ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GateHealth
	for rows.Next() {
		var g GateHealth
		if err := rows.Scan(&g.Provider, &g.Total, &g.Passed); err != nil {
			return nil, err
		}
		if g.Total > 0 {
			g.PassRate = float64(g.Passed) / float64(g.Total) * 100
		} else {
			g.PassRate = -1
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// queryActiveSessions returns the current fresh session list for the GUI.
// Deduplication by session ID is performed in Go before the page-size limit
// is applied, so callers receive the first `limit` *unique* sessions.
func queryActiveSessions(ctx context.Context, db *sql.DB, limit int) ([]ActiveSession, error) {
	hasAgentSessions, err := tableExists(ctx, db, "agent_sessions")
	if err != nil {
		return nil, fmt.Errorf("queryActiveSessions: check agent_sessions: %w", err)
	}

	var out []ActiveSession
	seenSessions := map[string]bool{}
	if hasAgentSessions {
		live, err := queryActiveSessionsFromAgentSessions(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, session := range live {
			if seenSessions[session.SessionID] {
				continue
			}
			seenSessions[session.SessionID] = true
			out = append(out, session)
		}
	}

	legacy, err := queryActiveSessionsFromBranchStates(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, session := range legacy {
		if seenSessions[session.SessionID] {
			continue
		}
		seenSessions[session.SessionID] = true
		out = append(out, session)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].LastHeartbeatAt.Equal(out[j].LastHeartbeatAt) {
			return out[i].LastHeartbeatAt.After(out[j].LastHeartbeatAt)
		}
		return out[i].SessionID < out[j].SessionID
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type sessionRow struct {
	SessionID               string
	AgentID                 string
	Mode                    string
	TmuxSessionName         string
	StartedAt               time.Time
	LastSeenAt              time.Time
	LastProgressAt          sql.NullTime
	LastIOAt                sql.NullTime
	EndedAt                 sql.NullTime
	EndReason               string
	ContextPressure         string
	CompactCount            int
	InferredStatus          string
	InferredStatusUpdatedAt sql.NullTime
	SessionRepo             string
	SessionBranch           string
	OutputBytes             int64
	AttributionSource       string
	AttributionConfidence   string
	LastRecoveredAt         sql.NullTime
}

func queryActiveSessionsFromAgentSessions(ctx context.Context, db *sql.DB) ([]ActiveSession, error) {
	threshold := time.Now().UTC().Add(-scheduler.SessionHeartbeatTTL)
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, agent_id, mode, COALESCE(tmux_session_name, ''), started_at, last_seen_at, last_progress_at, last_io_at, ended_at, COALESCE(end_reason, ''),
		       COALESCE(context_pressure, 'normal') AS context_pressure,
		       COALESCE(compact_count, 0) AS compact_count,
		       COALESCE(inferred_status, 'unknown') AS inferred_status,
		       inferred_status_updated_at,
		       COALESCE(repo, '') AS session_repo,
		       COALESCE(branch, '') AS session_branch,
		       COALESCE(output_bytes, 0) AS output_bytes,
		       COALESCE(attribution_source, 'unknown') AS attribution_source,
		       COALESCE(attribution_confidence, 'unknown') AS attribution_confidence,
		       last_recovered_at
		FROM agent_sessions
		WHERE ended_at IS NULL
		ORDER BY last_seen_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("queryActiveSessions: agent_sessions query failed: %w", err)
	}
	defer rows.Close()

	var sessions []sessionRow
	seenSessions := map[string]bool{}
	for rows.Next() {
		var s sessionRow
		if err := rows.Scan(&s.SessionID, &s.AgentID, &s.Mode, &s.TmuxSessionName, &s.StartedAt, &s.LastSeenAt, &s.LastProgressAt, &s.LastIOAt, &s.EndedAt, &s.EndReason,
			&s.ContextPressure, &s.CompactCount, &s.InferredStatus, &s.InferredStatusUpdatedAt,
			&s.SessionRepo, &s.SessionBranch, &s.OutputBytes, &s.AttributionSource, &s.AttributionConfidence, &s.LastRecoveredAt); err != nil {
			return nil, fmt.Errorf("queryActiveSessions: agent_sessions scan row: %w", err)
		}
		if s.SessionID == "" {
			continue
		}
		if s.LastSeenAt.Before(threshold) {
			continue
		}
		if seenSessions[s.SessionID] {
			continue
		}
		seenSessions[s.SessionID] = true
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queryActiveSessions: agent_sessions rows error: %w", err)
	}

	assignments, err := loadActiveAssignments(ctx, db, threshold)
	if err != nil {
		return nil, err
	}
	recentActivity, err := loadRecentRuntimeActivity(ctx, db, runtimeActivityWindow)
	if err != nil {
		return nil, err
	}

	var out []ActiveSession
	for _, s := range sessions {
		assignment, hasAssignment := assignments[s.SessionID]
		if !hasAssignment {
			assignment = activeAssignment{}
		}
		projection := buildRuntimeProjection(ctx, db, s, assignment, recentActivity[s.SessionID])
		out = append(out, ActiveSession{
			SessionID:               projection.SessionID,
			AgentID:                 projection.AgentID,
			Family:                  projection.Family,
			LaunchMode:              projection.LaunchMode,
			Repo:                    projection.Repo,
			Branch:                  projection.Branch,
			Worktree:                projection.Worktree,
			PRNumber:                projection.PRNumber,
			OwnerAgent:              projection.OwnerAgent,
			Mode:                    projection.Mode,
			LifecycleState:          projection.LifecycleState,
			ActivityState:           projection.ActivityState,
			AttachmentState:         projection.AttachmentState,
			AttributionSource:       projection.AttributionSource,
			AttributionConfidence:   projection.AttributionConfidence,
			Task:                    projection.Task,
			StartedAt:               projection.StartedAt,
			LastHeartbeatAt:         projection.LastHeartbeatAt,
			LastActivityAt:          projection.LastActivityAt,
			ProgressAt:              projection.ProgressAt,
			LastIOAt:                projection.LastIOAt,
			ElapsedSec:              projection.ElapsedSec,
			WorkingDurationSec:      projection.WorkingDurationSec,
			IdleDurationSec:         projection.IdleDurationSec,
			OutputMB:                projection.OutputMB,
			ContextPressure:         projection.ContextPressure,
			CompactCount:            projection.CompactCount,
			InferredStatus:          projection.InferredStatus,
			InferredStatusUpdatedAt: projection.InferredStatusUpdatedAt,
		})
	}
	return out, nil
}

func queryActiveSessionsFromBranchStates(ctx context.Context, db *sql.DB) ([]ActiveSession, error) {
	// Fresh heartbeats only: stale sessions are filtered out so the panel mirrors
	// live activity rather than historical branch ownership.
	threshold := time.Now().Add(-scheduler.SessionHeartbeatTTL)

	// No SQL LIMIT here — limit is applied after dedup below so we never
	// discard the only row for a session that appears multiple times early.
	rows, err := db.QueryContext(ctx, `
		SELECT owner_session_id, repo, branch, state,
		       owner_session_last_seen, submission_time, created_at, updated_at,
		       pr_number, owner_agent
		FROM branch_states
		WHERE owner_session_id <> ''
		  AND owner_session_last_seen IS NOT NULL
		  AND state IN `+activeSessionStatesSQL+`
		ORDER BY owner_session_last_seen DESC, updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("queryActiveSessions: query failed: %w", err)
	}
	defer rows.Close()

	var out []ActiveSession
	seenSessions := map[string]bool{}
	for rows.Next() {
		var sessionID, repo, branch, state, ownerAgent string
		var prNumber int
		var lastSeen, submissionTime, createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&sessionID, &repo, &branch, &state, &lastSeen, &submissionTime, &createdAt, &updatedAt, &prNumber, &ownerAgent); err != nil {
			return nil, fmt.Errorf("queryActiveSessions: scan row: %w", err)
		}
		if !lastSeen.Valid {
			continue
		}
		if lastSeen.Time.Before(threshold) {
			continue
		}
		if seenSessions[sessionID] {
			continue
		}
		seenSessions[sessionID] = true

		startedAt := startedAtForSession(submissionTime, createdAt, lastSeen)
		elapsed := time.Since(startedAt)
		if elapsed < 0 {
			elapsed = 0
		}

		ownerAgent = strings.TrimSpace(ownerAgent)
		agentID := resolveOwnerAgent(ownerAgent)
		out = append(out, ActiveSession{
			SessionID:             sessionID,
			AgentID:               agentID,
			Repo:                  repo,
			Branch:                branch,
			PRNumber:              prNumber,
			OwnerAgent:            ownerAgent,
			LifecycleState:        legacySessionLifecycleState(state),
			ActivityState:         legacySessionActivityState(state),
			AttachmentState:       "attached",
			AttributionSource:     "assignment_state",
			AttributionConfidence: "medium",
			Task:                  resolveTaskFromBranch(branch, state),
			StartedAt:             startedAt,
			LastHeartbeatAt:       lastSeen.Time,
			ProgressAt:            nil,
			ElapsedSec:            int64(elapsed.Seconds()),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queryActiveSessions: rows error: %w", err)
	}
	return out, nil
}

func queryAssignments(ctx context.Context, db *sql.DB, limit int) ([]AssignmentSummary, error) {
	hasAssignments, err := tableExists(ctx, db, "agent_assignments")
	if err != nil {
		return nil, fmt.Errorf("queryAssignments: check agent_assignments: %w", err)
	}
	if !hasAssignments {
		return []AssignmentSummary{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id, state, blocked_reason, assignment_substatus,
		       started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		ORDER BY started_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("queryAssignments: query failed: %w", err)
	}
	defer rows.Close()

	type assignmentRow struct {
		AssignmentID  string
		SessionID     string
		AgentID       string
		Repo          string
		Branch        string
		Worktree      string
		TaskID        string
		State         sql.NullString
		BlockedReason sql.NullString
		Substatus     sql.NullString
		StartedAt     time.Time
		EndedAt       sql.NullTime
		EndReason     string
		SupersededBy  sql.NullString
	}

	var raw []assignmentRow
	for rows.Next() {
		var row assignmentRow
		if err := rows.Scan(
			&row.AssignmentID, &row.SessionID, &row.AgentID, &row.Repo, &row.Branch, &row.Worktree, &row.TaskID, &row.State, &row.BlockedReason, &row.Substatus,
			&row.StartedAt, &row.EndedAt, &row.EndReason, &row.SupersededBy,
		); err != nil {
			return nil, fmt.Errorf("queryAssignments: scan row: %w", err)
		}
		raw = append(raw, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queryAssignments: rows error: %w", err)
	}

	liveSessions, err := queryActiveSessions(ctx, db, 0)
	if err != nil {
		return nil, fmt.Errorf("queryAssignments: load live sessions: %w", err)
	}
	liveBySessionID := make(map[string]ActiveSession, len(liveSessions))
	for _, session := range liveSessions {
		liveBySessionID[session.SessionID] = session
	}

	branchStateByRepoBranch := map[string]string{}
	branchRows, err := db.QueryContext(ctx, `
		SELECT repo, branch, state
		FROM branch_states
		ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("queryAssignments: branch_states query failed: %w", err)
	}
	defer branchRows.Close()
	for branchRows.Next() {
		var repo, branch, state string
		if scanErr := branchRows.Scan(&repo, &branch, &state); scanErr != nil {
			return nil, fmt.Errorf("queryAssignments: scan branch row: %w", scanErr)
		}
		key := repo + "::" + branch
		if _, exists := branchStateByRepoBranch[key]; !exists {
			branchStateByRepoBranch[key] = state
		}
	}
	if err := branchRows.Err(); err != nil {
		return nil, fmt.Errorf("queryAssignments: branch rows error: %w", err)
	}

	var out []AssignmentSummary
	for _, row := range raw {
		summary := AssignmentSummary{
			AssignmentID: row.AssignmentID,
			SessionID:    row.SessionID,
			AgentID:      row.AgentID,
			Repo:         row.Repo,
			Branch:       row.Branch,
			Worktree:     row.Worktree,
			TaskID:       row.TaskID,
			StartedAt:    row.StartedAt,
			EndReason:    row.EndReason,
			PRNumber:     lookupPRNumber(ctx, db, row.Repo, row.Branch),
		}
		if row.Substatus.Valid {
			summary.Substatus = row.Substatus.String
		}
		if row.State.Valid {
			summary.State = row.State.String
		}
		if row.BlockedReason.Valid {
			summary.BlockedReason = row.BlockedReason.String
		}
		if row.EndedAt.Valid {
			endedAt := row.EndedAt.Time
			summary.EndedAt = &endedAt
		}
		if row.SupersededBy.Valid {
			summary.SupersededBy = row.SupersededBy.String
		}
		if summary.EndedAt == nil {
			if session, ok := liveBySessionID[row.SessionID]; ok {
				summary.Mode = session.Mode
				summary.ActivityState = session.ActivityState
			}
			if activityState := assignmentActivityStateFromSubstatus(summary.Substatus); activityState != "active" || summary.ActivityState == "" {
				summary.ActivityState = activityState
			}
		}
		if branchState, ok := branchStateByRepoBranch[row.Repo+"::"+row.Branch]; ok {
			summary.BranchState = branchState
		}
		if summary.State == "" || (summary.EndedAt != nil && summary.State == "active") {
			summary.State = assignmentStateFromSummary(summary)
		}
		out = append(out, summary)
	}
	return out, nil
}

func queryCompliance(ctx context.Context, db *sql.DB, limit int) ([]AgentRuleRow, []AssignmentRuleCheckRow, error) {
	hasRules, err := tableExists(ctx, db, "agent_rules")
	if err != nil {
		return nil, nil, fmt.Errorf("queryCompliance: check agent_rules: %w", err)
	}
	hasChecks, err := tableExists(ctx, db, "assignment_rule_checks")
	if err != nil {
		return nil, nil, fmt.Errorf("queryCompliance: check assignment_rule_checks: %w", err)
	}
	if !hasRules || !hasChecks {
		return []AgentRuleRow{}, []AssignmentRuleCheckRow{}, nil
	}

	ruleRows, err := db.QueryContext(ctx, `
		SELECT rule_id, rule_name, rule_kind, description, enforcement,
		       violation_action, routing_target, rule_version, active
		FROM agent_rules
		ORDER BY rule_id ASC, rule_version DESC`)
	if err != nil {
		return nil, nil, fmt.Errorf("queryCompliance: rules query failed: %w", err)
	}
	defer ruleRows.Close()

	var rules []AgentRuleRow
	for ruleRows.Next() {
		var rule AgentRuleRow
		var violationAction string
		var active int
		if err := ruleRows.Scan(
			&rule.RuleID, &rule.RuleName, &rule.RuleKind, &rule.Description, &rule.Enforcement,
			&violationAction, &rule.RoutingTarget, &rule.RuleVersion, &active,
		); err != nil {
			return nil, nil, fmt.Errorf("queryCompliance: scan rule row: %w", err)
		}
		if violationAction != "" {
			if err := json.Unmarshal([]byte(violationAction), &rule.ViolationAction); err != nil {
				return nil, nil, fmt.Errorf("queryCompliance: decode rule %s actions: %w", rule.RuleID, err)
			}
		}
		rule.Active = active != 0
		rules = append(rules, rule)
	}
	if err := ruleRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("queryCompliance: rules rows error: %w", err)
	}

	checkRows, err := db.QueryContext(ctx, `
		SELECT check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
		       result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
		FROM assignment_rule_checks
		ORDER BY checked_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("queryCompliance: checks query failed: %w", err)
	}
	defer checkRows.Close()

	var checks []AssignmentRuleCheckRow
	for checkRows.Next() {
		var check AssignmentRuleCheckRow
		var violationRaised int
		var violationAction string
		var resolvedAt sql.NullTime
		if err := checkRows.Scan(
			&check.CheckID, &check.AssignmentID, &check.SessionID, &check.RuleID, &check.RuleVersion, &check.CheckedAt,
			&check.Result, &violationRaised, &violationAction, &check.Detail, &resolvedAt, &check.ResolvedBy,
		); err != nil {
			return nil, nil, fmt.Errorf("queryCompliance: scan check row: %w", err)
		}
		if violationAction != "" {
			if err := json.Unmarshal([]byte(violationAction), &check.ViolationActionTaken); err != nil {
				return nil, nil, fmt.Errorf("queryCompliance: decode check %s actions: %w", check.CheckID, err)
			}
		}
		check.ViolationRaised = violationRaised != 0
		check.ResolvedAt = nullTimePtr(resolvedAt)
		checks = append(checks, check)
	}
	if err := checkRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("queryCompliance: checks rows error: %w", err)
	}

	return rules, checks, nil
}

func assignmentStateFromSummary(summary AssignmentSummary) string {
	if derived := assignmentStateFromSubstatus(summary.Substatus); derived != "" {
		if summary.EndedAt == nil && derived == "completed" {
			return "active"
		}
		return derived
	}
	if summary.EndedAt == nil {
		switch summary.ActivityState {
		case "blocked":
			return "blocked"
		case "waiting":
			return "waiting"
		default:
			return "active"
		}
	}
	switch summary.EndReason {
	case "superseded":
		return "superseded"
	case "expired", "lost":
		return "lost"
	case "cancelled":
		return "cancelled"
	case "completed":
		return "completed"
	default:
		return "ended"
	}
}

func assignmentActivityStateFromSubstatus(substatus string) string {
	normalized := strings.ToLower(strings.TrimSpace(substatus))
	switch {
	case normalized == "":
		return "active"
	case strings.HasPrefix(normalized, "blocked_"):
		return "blocked"
	case strings.HasPrefix(normalized, "waiting_for_"):
		return "waiting"
	case strings.HasPrefix(normalized, "terminal_"):
		return "completed"
	default:
		return "active"
	}
}

func assignmentStateFromSubstatus(substatus string) string {
	normalized := strings.ToLower(strings.TrimSpace(substatus))
	switch {
	case normalized == "":
		return ""
	case normalized == "waiting_for_merge_approval":
		return "blocked"
	case strings.HasPrefix(normalized, "blocked_"):
		return "blocked"
	case normalized == "terminal_cancelled":
		return "cancelled"
	case normalized == "terminal_lost", normalized == "terminal_stuck_abandoned":
		return "lost"
	case strings.HasPrefix(normalized, "terminal_"):
		return "completed"
	default:
		return "active"
	}
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func queryAgentEvents(ctx context.Context, db *sql.DB, limit int) ([]AgentEventRow, error) {
	hasAgentEvents, err := tableExists(ctx, db, "agent_events")
	if err != nil {
		return nil, fmt.Errorf("queryAgentEvents: check agent_events: %w", err)
	}
	if !hasAgentEvents {
		return []AgentEventRow{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, session_id, agent_id, event_type, payload, created_at
		FROM agent_events
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("queryAgentEvents: query failed: %w", err)
	}
	defer rows.Close()

	var out []AgentEventRow
	for rows.Next() {
		var event AgentEventRow
		if err := rows.Scan(&event.ID, &event.SessionID, &event.AgentID, &event.EventType, &event.Payload, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("queryAgentEvents: scan row: %w", err)
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queryAgentEvents: rows error: %w", err)
	}
	return out, nil
}

type activeAssignment struct {
	Repo      string
	Branch    string
	Worktree  string
	TaskID    string
	Substatus string
}

func loadActiveAssignments(ctx context.Context, db *sql.DB, threshold time.Time) (map[string]activeAssignment, error) {
	out := make(map[string]activeAssignment)
	hasAssignments, err := tableExists(ctx, db, "agent_assignments")
	if err != nil {
		return nil, fmt.Errorf("queryActiveSessions: check agent_assignments: %w", err)
	}
	if !hasAssignments {
		return out, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id, assignment_substatus, started_at
		FROM agent_assignments
		WHERE ended_at IS NULL
		  AND session_id IN (
			  SELECT session_id
			  FROM agent_sessions
			  WHERE ended_at IS NULL AND last_seen_at >= ?
		  )
		ORDER BY session_id, started_at DESC`, threshold)
	if err != nil {
		return nil, fmt.Errorf("queryActiveSessions: agent_assignments query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var assignmentID, sessionID, agentID, repo, branch, worktree, taskID string
		var substatus sql.NullString
		var startedAt time.Time
		if err := rows.Scan(&assignmentID, &sessionID, &agentID, &repo, &branch, &worktree, &taskID, &substatus, &startedAt); err != nil {
			return nil, fmt.Errorf("queryActiveSessions: agent_assignments scan row: %w", err)
		}
		if sessionID == "" {
			continue
		}
		if _, exists := out[sessionID]; exists {
			continue
		}
		out[sessionID] = activeAssignment{
			Repo:      repo,
			Branch:    branch,
			Worktree:  worktree,
			TaskID:    taskID,
			Substatus: substatus.String,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queryActiveSessions: agent_assignments rows error: %w", err)
	}
	return out, nil
}

func startedAtForSession(submissionTime, createdAt, lastSeen sql.NullTime) time.Time {
	switch {
	case submissionTime.Valid:
		return submissionTime.Time
	case createdAt.Valid:
		return createdAt.Time
	case lastSeen.Valid:
		return lastSeen.Time
	default:
		return time.Now().UTC()
	}
}

func legacySessionLifecycleState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked", "queued_cli", "cli_reviewing", "waiting", "expired":
		return "blocked"
	default:
		return "active"
	}
}

func legacySessionActivityState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked":
		return "blocked"
	case "queued_cli", "cli_reviewing", "waiting", "expired":
		return "syncing"
	default:
		return "thinking"
	}
}

// queryRuns returns the most recent review runs.
func queryRuns(ctx context.Context, db *sql.DB, limit int) ([]RunRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, repo, branch, head_hash, provider, status,
		       started_at, finished_at, error, created_at
		FROM review_runs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RunRow
	for rows.Next() {
		var r RunRow
		var startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Repo, &r.Branch, &r.HeadHash,
			&r.Provider, &r.Status, &startedAt, &finishedAt,
			&r.Error, &r.CreatedAt); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			t := startedAt.Time
			r.StartedAt = &t
		}
		if finishedAt.Valid {
			t := finishedAt.Time
			r.FinishedAt = &t
		}
		// Manual runs are identified by provider "manual".
		r.Manual = r.Provider == "manual"
		out = append(out, r)
	}
	return out, rows.Err()
}

// queryLatestActivitySeq returns the highest delivery_events seq across all repos.
// Returns 0 if the table is empty.
func queryLatestActivitySeq(ctx context.Context, db *sql.DB) (int64, error) {
	var seq sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT MAX(seq) FROM delivery_events`).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if seq.Valid {
		return seq.Int64, nil
	}
	return 0, nil
}

// queryActivitySince returns delivery_events newer than sinceSeq.
func queryActivitySince(ctx context.Context, db *sql.DB, sinceSeq int64, limit int) ([]ActivityEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, repo, branch, event_type, payload, created_at,
		       COALESCE(session_id, ''), COALESCE(assignment_id, '')
		FROM delivery_events
		WHERE seq > ?
		ORDER BY seq ASC
		LIMIT ?`, sinceSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ActivityEvent
	for rows.Next() {
		var e ActivityEvent
		if err := rows.Scan(&e.Seq, &e.Repo, &e.Branch, &e.EventType, &e.Payload, &e.CreatedAt,
			&e.SessionID, &e.AssignmentID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// insertManualReviewRun creates a pending manual review run and returns its ID.
func insertManualReviewRun(ctx context.Context, db *sql.DB, id, repo, branch, headHash string) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
		INSERT INTO review_runs (id, repo, branch, head_hash, provider, status, started_at, error, created_at)
		VALUES (?, ?, ?, ?, 'manual', 'pending', ?, '', ?)`,
		id, repo, branch, headHash, now, now)
	return err
}

// resolveTaskFromBranch derives task context from a branch name.
// Returns nil unless the branch uses the literal feat/PROJ-NNN-description pattern
// (e.g. feat/COD-056-fix-auth). Callers must render nil task gracefully.
func resolveTaskFromBranch(branch, state string) *ActiveTask {
	if !strings.HasPrefix(branch, "feat/") {
		return nil
	}
	b := strings.TrimPrefix(branch, "feat/")
	// Must be PROJ-NNN-description where the second segment is numeric,
	// e.g. COD-056-dashboard-activity-health → taskID="COD-056".
	parts := strings.SplitN(b, "-", 3)
	if len(parts) < 3 {
		return nil
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return nil // second segment is not a numeric ID — not a real task branch
	}
	taskID := parts[0] + "-" + parts[1]
	title := strings.ReplaceAll(parts[2], "-", " ")
	phase := ""
	if state != "" {
		phase = sessionPhaseLabel(state)
	}
	return &ActiveTask{
		ID:    taskID,
		Title: title,
		Phase: phase,
	}
}

func resolveTaskFromAssignment(taskID, branch string) *ActiveTask {
	branchTask := resolveTaskFromBranch(branch, "")
	if taskID == "" {
		return branchTask
	}
	if branchTask == nil {
		return &ActiveTask{
			ID:    taskID,
			Title: strings.ReplaceAll(taskID, "-", " "),
		}
	}
	branchTask.ID = taskID
	if branchTask.Title == "" {
		branchTask.Title = strings.ReplaceAll(taskID, "-", " ")
	}
	return branchTask
}

// resolveOwnerAgent returns the persisted owner/launch profile ID for a session.
func resolveOwnerAgent(agentFromDB string) string {
	return strings.TrimSpace(agentFromDB)
}

func lookupPRNumber(ctx context.Context, db *sql.DB, repo, branch string) int {
	var pr sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT pr_number FROM branch_states WHERE repo = ? AND branch = ?`,
		repo, branch,
	).Scan(&pr); err == nil && pr.Valid {
		return int(pr.Int64)
	}
	return 0
}

// sessionPhaseLabel maps a raw branch state to a human-readable phase label.
func sessionPhaseLabel(state string) string {
	switch state {
	case "submitted":
		return "submitted"
	case "waiting":
		return "waiting"
	case "queued_cli":
		return "queued for review"
	case "cli_reviewing":
		return "review in progress"
	case "review_approved":
		return "review approved"
	case "merge_ready":
		return "merge ready"
	case "merged":
		return "merged"
	case "blocked":
		return "blocked"
	case "expired":
		return "expired"
	case "abandoned":
		return "abandoned"
	case "stale":
		return "stale"
	default:
		return "unknown"
	}
}

// stalledProgressThreshold is how long progress_at can be stale before
// an agent is considered stalled (alive but not producing output).
const stalledProgressThreshold = 90 * time.Second

// stalledHeartbeatThreshold is the max heartbeat age for a session to be
// considered "alive" for stalled detection. Must be alive to be stalled.
const stalledHeartbeatThreshold = 2 * time.Minute

// staleFeedThreshold is the age after which a feed is considered stale.
const staleFeedThreshold = 5 * time.Minute

// queryDashboardHealth probes database connectivity, per-feed freshness, and
// the live active-agent count. It is the backend for GET /api/v1/dashboard/health.
// It returns an error when a freshness/count query fails so the handler can
// surface a real backend failure rather than silently serving stale defaults.
func queryDashboardHealth(ctx context.Context, db *sql.DB) (DashboardHealth, error) {
	h := DashboardHealth{GeneratedAt: time.Now().UTC(), SchemaVersion: SchemaVersionV1}
	dbHealthy := true

	// Database health: a lightweight ping.
	if err := db.PingContext(ctx); err != nil {
		loglib.Error("dashboard: health db ping failed",
			loglib.FieldComponent, "dashboard", "error", err)
		h.Database = ServiceStatus{Status: "down", Message: "database unreachable"}
		dbHealthy = false
	} else {
		h.Database = ServiceStatus{Status: "ok"}
	}

	// Active-sessions feed freshness: age of the most recent heartbeat.
	if dbHealthy {
		sessions, err := queryActiveSessions(ctx, db, 0)
		if err != nil {
			return h, fmt.Errorf("queryDashboardHealth: active sessions query: %w", err)
		}
		h.ActiveAgentCount = len(sessions)
		if len(sessions) > 0 {
			age := time.Since(sessions[0].LastHeartbeatAt)
			status := "ok"
			if age > staleFeedThreshold {
				status = "stale"
			}
			h.Feeds.ActiveSessions = FeedStatus{
				Status:       status,
				LastRefresh:  sessions[0].LastHeartbeatAt.UTC(),
				FreshnessSec: int64(age.Seconds()),
			}
		} else {
			h.Feeds.ActiveSessions = FeedStatus{Status: "unavailable"}
		}

		staleCount, expiredCount, reconStatus, err := querySessionMonitoring(ctx, db)
		if err != nil {
			return h, fmt.Errorf("queryDashboardHealth: session monitoring query: %w", err)
		}
		h.StaleSessionCount = staleCount
		h.ExpiredSessionCount = expiredCount
		h.ReconciliationStatus = reconStatus
	} else {
		h.Feeds.ActiveSessions = FeedStatus{Status: "unavailable"}
		h.ReconciliationStatus = "unavailable"
	}

	// Gate-checks feed freshness: mod time of the last report file.
	reportPath := gateCheckReportPath()
	if info, err := os.Stat(reportPath); err == nil {
		if !info.Mode().IsRegular() {
			h.Feeds.GateChecks = FeedStatus{Status: "unavailable"}
		} else {
			age := time.Since(info.ModTime())
			status := "ok"
			if age > staleFeedThreshold {
				status = "stale"
			}
			h.Feeds.GateChecks = FeedStatus{
				Status:       status,
				LastRefresh:  info.ModTime().UTC(),
				FreshnessSec: int64(age.Seconds()),
			}
		}
	} else {
		h.Feeds.GateChecks = FeedStatus{Status: "unavailable"}
	}

	// Best-effort metrics derived from local files and DB history.
	// Failures are silently ignored so a missing coverage file or empty
	// review_runs table does not degrade the DB/feed health report.
	repoRoot := os.Getenv("CODERO_REPO_PATH")
	if repoRoot == "" {
		repoRoot = "."
	}
	h.SecurityScore = computeSecurityScore(reportPath)
	h.CoveragePct = parseCoverageFilePath(resolveCoveragePath())
	etaDetail := queryETADetail(ctx, db, "", "")
	if etaDetail != nil {
		h.ETAMin = &etaDetail.ETAMin
		h.ETADetail = etaDetail
	}

	return h, nil
}

// resolveCoveragePath determines the absolute or relative path to the
// coverage.out file used by the dashboard.
// Precedence: CODERO_COVERAGE_PATH > {CODERO_REPO_PATH}/coverage.out > ./coverage.out
func resolveCoveragePath() string {
	if p := os.Getenv("CODERO_COVERAGE_PATH"); p != "" {
		return p
	}
	repoRoot := os.Getenv("CODERO_REPO_PATH")
	if repoRoot == "" {
		repoRoot = "."
	}
	return filepath.Join(repoRoot, "coverage.out")
}

func querySessionMonitoring(ctx context.Context, db *sql.DB) (staleCount, expiredCount int, status string, err error) {
	now := time.Now().UTC()
	staleThreshold := now.Add(-staleFeedThreshold)
	expiredThreshold := now.Add(-scheduler.SessionHeartbeatTTL)

	hasAgentSessions, err := tableExists(ctx, db, "agent_sessions")
	if err != nil {
		return 0, 0, "", err
	}

	if hasAgentSessions {
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM agent_sessions
			 WHERE ended_at IS NULL AND last_seen_at < ? AND last_seen_at >= ?`,
			staleThreshold, expiredThreshold,
		).Scan(&staleCount); err != nil {
			return 0, 0, "", err
		}
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM agent_sessions
			 WHERE ended_at IS NULL AND last_seen_at < ?`,
			expiredThreshold,
		).Scan(&expiredCount); err != nil {
			return 0, 0, "", err
		}
	} else {
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM branch_states
			 WHERE owner_session_id <> ''
			   AND owner_session_last_seen IS NOT NULL
			   AND owner_session_last_seen < ?
			   AND owner_session_last_seen >= ?
			   AND state IN `+activeSessionStatesSQL,
			staleThreshold, expiredThreshold,
		).Scan(&staleCount); err != nil {
			return 0, 0, "", err
		}
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM branch_states
			 WHERE owner_session_id <> ''
			   AND owner_session_last_seen IS NOT NULL
			   AND owner_session_last_seen < ?
			   AND state IN `+activeSessionStatesSQL,
			expiredThreshold,
		).Scan(&expiredCount); err != nil {
			return 0, 0, "", err
		}
	}

	switch {
	case expiredCount > 0:
		status = "attention"
	case staleCount > 0:
		status = "stale"
	default:
		status = "ok"
	}
	return staleCount, expiredCount, status, nil
}

// computeSecurityScore derives a 0–10 score from the gate-check report.
// Returns nil when the file is missing, empty, or unparseable.
func computeSecurityScore(reportPath string) *SecurityScoreStats {
	data, err := os.ReadFile(reportPath) //nolint:gosec
	if err != nil {
		return nil
	}
	var report struct {
		Summary struct {
			Passed         int `json:"passed"`
			Failed         int `json:"failed"`
			RequiredFailed int `json:"required_failed"`
			Total          int `json:"total"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil
	}
	total := report.Summary.Total
	if total == 0 {
		return nil
	}
	requiredFailed := report.Summary.RequiredFailed
	failed := report.Summary.Failed
	good := total - requiredFailed - failed
	if good < 0 {
		good = 0
	}
	score := good * 10 / total
	pct := float64(report.Summary.Passed) / float64(total) * 100
	return &SecurityScoreStats{
		Score:    score,
		Pct:      pct,
		Critical: requiredFailed,
		High:     failed,
		Total:    total,
	}
}

// parseCoverageFilePath parses a Go coverage.out file at the given absolute or
// relative path and returns the statement coverage percentage.
// Returns nil when the file is missing, empty, or unparseable.
func parseCoverageFilePath(path string) *float64 {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil
	}
	defer f.Close()

	var totalStmts, coveredStmts int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// Format: pkg/file.go:startLine.col,endLine.col numStmts hitCount
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		numStmts, err1 := strconv.Atoi(fields[1])
		hitCount, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			continue
		}
		totalStmts += numStmts
		if hitCount > 0 {
			coveredStmts += numStmts
		}
	}
	if err := scanner.Err(); err != nil {
		return nil
	}
	if totalStmts == 0 {
		return nil
	}
	pct := float64(coveredStmts) / float64(totalStmts) * 100
	return &pct
}

// queryETADetail estimates remaining minutes for the active review run using
// calibrated percentile metrics. It computes avg, p50, and p90 from completed
// run durations over the last 7 days, filtered by repo and/or branch prefix.
// Returns nil when there is no historical data.
func queryETADetail(ctx context.Context, db *sql.DB, repo, branchPrefix string) *ETADetail {
	// Fetch completed run durations in minutes
	rows, err := db.QueryContext(ctx, `
		SELECT ROUND((julianday(finished_at) - julianday(started_at)) * 1440)
		FROM review_runs
		WHERE status IN ('completed', 'approved') AND finished_at IS NOT NULL
		  AND started_at >= datetime('now', '-7 days')
		  AND (? = '' OR repo = ?)
		  AND (? = '' OR branch LIKE ? || '%')
		ORDER BY (julianday(finished_at) - julianday(started_at)) ASC`,
		repo, repo, branchPrefix, branchPrefix)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var durations []float64
	for rows.Next() {
		var d sql.NullFloat64
		if err := rows.Scan(&d); err == nil && d.Valid {
			durations = append(durations, d.Float64)
		}
	}
	if len(durations) == 0 {
		return nil
	}

	// Compute avg
	var sum float64
	for _, d := range durations {
		sum += d
	}
	avg := sum / float64(len(durations))

	// Compute percentiles (durations already sorted ASC from query)
	p50 := percentile(durations, 0.50)
	p90 := percentile(durations, 0.90)

	// Find elapsed time for current running run
	var elapsedMin float64
	_ = db.QueryRowContext(ctx, `
		SELECT ROUND((julianday('now') - julianday(started_at)) * 1440)
		FROM review_runs
		WHERE status = 'running'
		  AND (? = '' OR repo = ?)
		  AND (? = '' OR branch LIKE ? || '%')
		ORDER BY started_at DESC LIMIT 1`, repo, repo, branchPrefix, branchPrefix).Scan(&elapsedMin)

	eta := p50 - elapsedMin
	if eta < 0 {
		eta = 0
	}

	return &ETADetail{
		AvgMin:     int(avg),
		P50Min:     int(p50),
		P90Min:     int(p90),
		ElapsedMin: int(elapsedMin),
		ETAMin:     int(eta),
	}
}

// percentile returns the value at the given percentile from a sorted slice.
// Uses linear interpolation between adjacent values.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	// Index for percentile (0-indexed)
	idx := p * float64(n-1)
	lower := int(idx)
	if lower >= n-1 {
		return sorted[n-1]
	}
	upper := lower + 1
	frac := idx - float64(lower)
	return sorted[lower] + frac*(sorted[upper]-sorted[lower])
}

// AgentRosterRow holds per-agent aggregated stats from the last 30 days.
type AgentRosterRow struct {
	AgentID        string    `json:"agent_id"`
	ActiveSessions int       `json:"active_sessions"`
	TotalSessions  int       `json:"total_sessions"`
	LastSeen       time.Time `json:"last_seen"`
	AvgElapsedSec  float64   `json:"avg_elapsed_sec"`
	TotalTokens    int64     `json:"total_tokens"`
	TokensPerSec   float64   `json:"tokens_per_sec"`
	ActivePressure string    `json:"active_pressure"`
	Status         string    `json:"status"` // active | idle | offline | disabled
}

// queryAgentRoster returns per-agent stats aggregated over the last 30 days.
// token metrics are joined from session_token_metrics when that table exists.
func queryAgentRoster(ctx context.Context, db *sql.DB) ([]AgentRosterRow, error) {
	hasMetrics, err := tableExists(ctx, db, "session_token_metrics")
	if err != nil {
		return nil, fmt.Errorf("queryAgentRoster: check session_token_metrics: %w", err)
	}

	var query string
	if hasMetrics {
		query = `
		SELECT
		  s.agent_id,
		  COUNT(*) AS total_sessions,
		  COUNT(CASE WHEN s.ended_at IS NULL THEN 1 END) AS active_sessions,
		  MAX(s.last_seen_at) AS last_seen,
		  AVG(CASE WHEN s.ended_at IS NOT NULL
		        THEN (strftime('%s', s.ended_at) - strftime('%s', s.started_at))
		      END) AS avg_elapsed_sec,
		  COALESCE(SUM(m.total_prompt_tokens + m.total_completion_tokens), 0) AS total_tokens,
		  MAX(CASE WHEN s.ended_at IS NULL THEN s.context_pressure ELSE NULL END) AS active_pressure
		FROM agent_sessions s
		LEFT JOIN (
		  SELECT session_id,
		    SUM(prompt_tokens) AS total_prompt_tokens,
		    SUM(completion_tokens) AS total_completion_tokens
		  FROM session_token_metrics
		  GROUP BY session_id
		) m ON m.session_id = s.session_id
		WHERE s.started_at > datetime('now', '-30 days')
		GROUP BY s.agent_id
		ORDER BY active_sessions DESC, last_seen DESC`
	} else {
		query = `
		SELECT
		  agent_id,
		  COUNT(*) AS total_sessions,
		  COUNT(CASE WHEN ended_at IS NULL THEN 1 END) AS active_sessions,
		  MAX(last_seen_at) AS last_seen,
		  AVG(CASE WHEN ended_at IS NOT NULL
		        THEN (strftime('%s', ended_at) - strftime('%s', started_at))
		      END) AS avg_elapsed_sec,
		  0 AS total_tokens,
		  MAX(CASE WHEN ended_at IS NULL THEN context_pressure ELSE NULL END) AS active_pressure
		FROM agent_sessions
		WHERE started_at > datetime('now', '-30 days')
		GROUP BY agent_id
		ORDER BY active_sessions DESC, last_seen DESC`
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("queryAgentRoster: query: %w", err)
	}
	defer rows.Close()

	offlineThreshold := time.Now().UTC().Add(-5 * time.Minute)
	var out []AgentRosterRow
	for rows.Next() {
		var r AgentRosterRow
		var lastSeenRaw any
		var avgElapsed sql.NullFloat64
		var activePressure sql.NullString
		if err := rows.Scan(
			&r.AgentID, &r.TotalSessions, &r.ActiveSessions,
			&lastSeenRaw, &avgElapsed, &r.TotalTokens, &activePressure,
		); err != nil {
			return nil, fmt.Errorf("queryAgentRoster: scan: %w", err)
		}
		if parsed, ok, err := scanTimeValue(lastSeenRaw); err != nil {
			return nil, fmt.Errorf("queryAgentRoster: parse last_seen: %w", err)
		} else if ok {
			r.LastSeen = parsed
		}
		if avgElapsed.Valid {
			r.AvgElapsedSec = avgElapsed.Float64
		}
		if activePressure.Valid {
			r.ActivePressure = activePressure.String
		}
		// TokensPerSec = aggregate throughput: total tokens across all sessions
		// divided by (completed sessions × avg elapsed per session).
		// This gives tokens/sec as if all completed sessions ran sequentially.
		// Not meaningful for 0 completed sessions or 0 avg elapsed.
		completedSessions := r.TotalSessions - r.ActiveSessions
		if completedSessions > 0 && r.AvgElapsedSec > 0 {
			r.TokensPerSec = float64(r.TotalTokens) / (float64(completedSessions) * r.AvgElapsedSec)
		}
		// Status is assigned by the caller after merging tracking config.
		// Here we only set the DB-derived status.
		if r.ActiveSessions > 0 {
			r.Status = "active"
		} else if !r.LastSeen.IsZero() && r.LastSeen.Before(offlineThreshold) {
			r.Status = "offline"
		} else {
			r.Status = "idle"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AgentRecentSessionRow holds summary data for one of an agent's recent sessions.
type AgentRecentSessionRow struct {
	SessionID string     `json:"session_id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	EndReason string     `json:"end_reason"`
	Repo      string     `json:"repo"`
	Branch    string     `json:"branch"`
}

// queryAgentRecentSessions returns the last 5 sessions for a given agent, with
// the most recent assignment's repo/branch for context.
func queryAgentRecentSessions(ctx context.Context, db *sql.DB, agentID string) ([]AgentRecentSessionRow, error) {
	const query = `
	SELECT
	  s.session_id,
	  s.started_at,
	  s.ended_at,
	  s.end_reason,
	  COALESCE((SELECT repo   FROM agent_assignments WHERE session_id = s.session_id ORDER BY started_at DESC LIMIT 1), '') AS repo,
	  COALESCE((SELECT branch FROM agent_assignments WHERE session_id = s.session_id ORDER BY started_at DESC LIMIT 1), '') AS branch
	FROM agent_sessions s
	WHERE s.agent_id = ?
	ORDER BY s.started_at DESC
	LIMIT 5`

	rows, err := db.QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, fmt.Errorf("queryAgentRecentSessions: %w", err)
	}
	defer rows.Close()

	var out []AgentRecentSessionRow
	for rows.Next() {
		var r AgentRecentSessionRow
		var endedAt sql.NullTime
		if err := rows.Scan(&r.SessionID, &r.StartedAt, &endedAt, &r.EndReason, &r.Repo, &r.Branch); err != nil {
			return nil, fmt.Errorf("queryAgentRecentSessions: scan: %w", err)
		}
		if endedAt.Valid {
			r.EndedAt = &endedAt.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// queryETAMinutes is retained for backwards compatibility.
// It delegates to queryETADetail and returns only the eta_min value.
func queryETAMinutes(ctx context.Context, db *sql.DB, repo string) *int {
	detail := queryETADetail(ctx, db, repo, "")
	if detail == nil {
		return nil
	}
	return &detail.ETAMin
}

// ScanNodeRepos scans a directory for git repositories and identifies
// which are connected (in connectedRepos) and which are orphans.
func ScanNodeRepos(ctx context.Context, scanPath string, connectedRepos []string) ([]NodeRepoSummary, error) {
	entries, err := os.ReadDir(scanPath)
	if err != nil {
		return nil, fmt.Errorf("ScanNodeRepos: read dir %s: %w", scanPath, err)
	}

	connectedSet := make(map[string]bool)
	for _, r := range connectedRepos {
		// connectedRepos might be full paths or just names
		connectedSet[filepath.Base(r)] = true
	}

	var out []NodeRepoSummary
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(scanPath, entry.Name())
		if isGitRepo(path) {
			connected := connectedSet[entry.Name()]
			out = append(out, NodeRepoSummary{
				Name:      entry.Name(),
				Path:      path,
				Connected: connected,
				IsOrphan:  !connected,
				LastScan:  time.Now().UTC(),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	return out, nil
}

func isGitRepo(path string) bool {
	// Check for .git directory or file (for worktrees)
	gitPath := filepath.Join(path, ".git")
	_, err := os.Stat(gitPath)
	return err == nil
}
