package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ─── §3 Session queries ─────────────────────────────────────────────────

func querySessions(ctx context.Context, db *sql.DB, status string, limit, offset int) ([]SessionRow, int, error) {
	hasTable, err := tableExists(ctx, db, "agent_sessions")
	if err != nil {
		return nil, 0, err
	}
	if !hasTable {
		return []SessionRow{}, 0, nil
	}

	// Build WHERE clause based on status filter.
	where := ""
	switch status {
	case "active":
		where = ` WHERE ended_at IS NULL`
	case "ended":
		where = ` WHERE ended_at IS NOT NULL`
	case "working":
		where = ` WHERE ended_at IS NULL AND inferred_status = 'working'`
	case "waiting_for_input":
		where = ` WHERE ended_at IS NULL AND inferred_status = 'waiting_for_input'`
	case "idle":
		where = ` WHERE ended_at IS NULL AND inferred_status = 'idle'`
	}

	// Count total matching rows.
	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_sessions`+where).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("querySessions: count: %w", err)
	}

	// Query rows.
	query := `SELECT session_id, agent_id, mode, COALESCE(tmux_session_name, ''), COALESCE(inferred_status, 'unknown'), started_at, last_seen_at, ended_at, end_reason FROM agent_sessions` + where +
		` ORDER BY started_at DESC LIMIT ? OFFSET ?`

	rows, err := db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("querySessions: query: %w", err)
	}
	defer rows.Close()

	var out []SessionRow
	for rows.Next() {
		var s SessionRow
		var endedAt sql.NullTime
		var endReason sql.NullString
		if err := rows.Scan(&s.SessionID, &s.AgentID, &s.Mode, &s.TmuxSessionName, &s.InferredStatus, &s.StartedAt, &s.LastSeenAt, &endedAt, &endReason); err != nil {
			return nil, 0, fmt.Errorf("querySessions: scan: %w", err)
		}
		if endedAt.Valid {
			t := endedAt.Time
			s.EndedAt = &t
			s.Status = "ended"
		} else {
			s.Status = "active"
		}
		if endReason.Valid {
			s.EndReason = endReason.String
		}
		s.Checkpoint = deriveCheckpoint(s.Status, s.EndReason)
		out = append(out, s)
	}
	if out == nil {
		out = []SessionRow{}
	}
	return out, total, rows.Err()
}

func querySessionByID(ctx context.Context, db *sql.DB, sessionID string) (*SessionRow, error) {
	hasTable, err := tableExists(ctx, db, "agent_sessions")
	if err != nil {
		return nil, err
	}
	if !hasTable {
		return nil, sql.ErrNoRows
	}

	row := db.QueryRowContext(ctx, `
		SELECT session_id, agent_id, mode, COALESCE(tmux_session_name, ''), started_at, last_seen_at, ended_at, end_reason
		FROM agent_sessions WHERE session_id = ?`, sessionID)

	var s SessionRow
	var endedAt sql.NullTime
	var endReason sql.NullString
	if err := row.Scan(&s.SessionID, &s.AgentID, &s.Mode, &s.TmuxSessionName, &s.StartedAt, &s.LastSeenAt, &endedAt, &endReason); err != nil {
		return nil, err
	}
	if endedAt.Valid {
		t := endedAt.Time
		s.EndedAt = &t
		s.Status = "ended"
	} else {
		s.Status = "active"
	}
	if endReason.Valid {
		s.EndReason = endReason.String
	}
	s.Checkpoint = deriveCheckpoint(s.Status, s.EndReason)
	return &s, nil
}

func queryAssignmentsBySession(ctx context.Context, db *sql.DB, sessionID string) ([]AssignmentSummary, error) {
	hasTable, err := tableExists(ctx, db, "agent_assignments")
	if err != nil {
		return nil, err
	}
	if !hasTable {
		return []AssignmentSummary{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
		       state, blocked_reason, assignment_substatus, started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE session_id = ?
		ORDER BY started_at DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AssignmentSummary
	for rows.Next() {
		var a AssignmentSummary
		var state, blockedReason, substatus, supersededBy sql.NullString
		var endedAt sql.NullTime
		if err := rows.Scan(
			&a.AssignmentID, &a.SessionID, &a.AgentID, &a.Repo, &a.Branch, &a.Worktree, &a.TaskID,
			&state, &blockedReason, &substatus, &a.StartedAt, &endedAt, &a.EndReason, &supersededBy,
		); err != nil {
			return nil, err
		}
		if state.Valid {
			a.State = state.String
		}
		if blockedReason.Valid {
			a.BlockedReason = blockedReason.String
		}
		if substatus.Valid {
			a.Substatus = substatus.String
		}
		if endedAt.Valid {
			t := endedAt.Time
			a.EndedAt = &t
		}
		if supersededBy.Valid {
			a.SupersededBy = supersededBy.String
		}
		out = append(out, a)
	}
	if out == nil {
		out = []AssignmentSummary{}
	}
	return out, rows.Err()
}

// ─── §4 Assignment detail queries ────────────────────────────────────────

func queryAssignmentByID(ctx context.Context, db *sql.DB, assignmentID string) (*AssignmentSummary, error) {
	hasTable, err := tableExists(ctx, db, "agent_assignments")
	if err != nil {
		return nil, err
	}
	if !hasTable {
		return nil, sql.ErrNoRows
	}

	row := db.QueryRowContext(ctx, `
		SELECT assignment_id, session_id, agent_id, repo, branch, worktree, task_id,
		       state, blocked_reason, assignment_substatus, started_at, ended_at, end_reason, superseded_by
		FROM agent_assignments
		WHERE assignment_id = ?`, assignmentID)

	var a AssignmentSummary
	var state, blockedReason, substatus, supersededBy sql.NullString
	var endedAt sql.NullTime
	if err := row.Scan(
		&a.AssignmentID, &a.SessionID, &a.AgentID, &a.Repo, &a.Branch, &a.Worktree, &a.TaskID,
		&state, &blockedReason, &substatus, &a.StartedAt, &endedAt, &a.EndReason, &supersededBy,
	); err != nil {
		return nil, err
	}
	if state.Valid {
		a.State = state.String
	}
	if blockedReason.Valid {
		a.BlockedReason = blockedReason.String
	}
	if substatus.Valid {
		a.Substatus = substatus.String
	}
	if endedAt.Valid {
		t := endedAt.Time
		a.EndedAt = &t
	}
	if supersededBy.Valid {
		a.SupersededBy = supersededBy.String
	}
	return &a, nil
}

func queryRuleChecksByAssignment(ctx context.Context, db *sql.DB, assignmentID string) ([]AssignmentRuleCheckRow, error) {
	hasTable, err := tableExists(ctx, db, "assignment_rule_checks")
	if err != nil {
		return nil, err
	}
	if !hasTable {
		return []AssignmentRuleCheckRow{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
		       result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
		FROM assignment_rule_checks
		WHERE assignment_id = ?
		ORDER BY checked_at DESC`, assignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AssignmentRuleCheckRow
	for rows.Next() {
		var check AssignmentRuleCheckRow
		var violationRaised int
		var violationAction string
		var resolvedAt sql.NullTime
		if err := rows.Scan(
			&check.CheckID, &check.AssignmentID, &check.SessionID, &check.RuleID, &check.RuleVersion, &check.CheckedAt,
			&check.Result, &violationRaised, &violationAction, &check.Detail, &resolvedAt, &check.ResolvedBy,
		); err != nil {
			return nil, err
		}
		if violationAction != "" {
			_ = json.Unmarshal([]byte(violationAction), &check.ViolationActionTaken)
		}
		check.ViolationRaised = violationRaised != 0
		check.ResolvedAt = nullTimePtr(resolvedAt)
		out = append(out, check)
	}
	if out == nil {
		out = []AssignmentRuleCheckRow{}
	}
	return out, rows.Err()
}

// ─── §5 Feedback queries ────────────────────────────────────────────────

func queryFeedbackByTask(ctx context.Context, db *sql.DB, taskID string) ([]FeedbackItem, error) {
	hasTable, err := tableExists(ctx, db, "findings")
	if err != nil {
		return nil, err
	}
	if !hasTable {
		return []FeedbackItem{}, nil
	}

	// taskID is treated as repo/branch pattern — match on branch LIKE or repo LIKE.
	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, repo, branch, severity, category, file, line, message, source, rule_id, ts
		FROM findings
		WHERE branch = ? OR repo = ? OR run_id = ?
		ORDER BY ts DESC
		LIMIT 200`, taskID, taskID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FeedbackItem
	for rows.Next() {
		var f FeedbackItem
		if err := rows.Scan(&f.ID, &f.RunID, &f.Repo, &f.Branch, &f.Severity, &f.Category, &f.File, &f.Line, &f.Message, &f.Source, &f.RuleID, &f.Ts); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if out == nil {
		out = []FeedbackItem{}
	}
	return out, rows.Err()
}

func queryFeedbackHistory(ctx context.Context, db *sql.DB, limit int) ([]FeedbackItem, error) {
	hasTable, err := tableExists(ctx, db, "findings")
	if err != nil {
		return nil, err
	}
	if !hasTable {
		return []FeedbackItem{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id, run_id, repo, branch, severity, category, file, line, message, source, rule_id, ts
		FROM findings
		ORDER BY ts DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FeedbackItem
	for rows.Next() {
		var f FeedbackItem
		if err := rows.Scan(&f.ID, &f.RunID, &f.Repo, &f.Branch, &f.Severity, &f.Category, &f.File, &f.Line, &f.Message, &f.Source, &f.RuleID, &f.Ts); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if out == nil {
		out = []FeedbackItem{}
	}
	return out, rows.Err()
}

// ─── §6 Gate queries ────────────────────────────────────────────────────

func queryGateLive(ctx context.Context, db *sql.DB, sessionID string) (status, provider, progress string) {
	// Find the most recent running review_run for assignments linked to this session.
	row := db.QueryRowContext(ctx, `
		SELECT rr.status, rr.provider
		FROM review_runs rr
		JOIN agent_assignments aa ON rr.repo = aa.repo AND rr.branch = aa.branch
		WHERE aa.session_id = ?
		ORDER BY rr.created_at DESC
		LIMIT 1`, sessionID)

	var st, prov string
	if err := row.Scan(&st, &prov); err != nil {
		return "idle", "", ""
	}
	prog := ""
	if st == "running" {
		prog = "in_progress"
	}
	return st, prov, prog
}

func queryGateResultsBySession(ctx context.Context, db *sql.DB, sessionID string) ([]RunRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT rr.id, rr.repo, rr.branch, rr.head_hash, rr.provider, rr.status,
		       rr.started_at, rr.finished_at, rr.error, rr.created_at
		FROM review_runs rr
		JOIN agent_assignments aa ON rr.repo = aa.repo AND rr.branch = aa.branch
		WHERE aa.session_id = ?
		ORDER BY rr.created_at DESC
		LIMIT 50`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RunRow
	for rows.Next() {
		var r RunRow
		var startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Repo, &r.Branch, &r.HeadHash,
			&r.Provider, &r.Status, &startedAt, &finishedAt, &r.Error, &r.CreatedAt); err != nil {
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
		r.Manual = r.Provider == "manual"
		out = append(out, r)
	}
	if out == nil {
		out = []RunRow{}
	}
	return out, rows.Err()
}

// ─── §7 Merge queries ──────────────────────────────────────────────────

func queryMergeEligibility(ctx context.Context, db *sql.DB, assignmentID string) (eligible bool, gateStatus, reason string) {
	// Look up the assignment.
	row := db.QueryRowContext(ctx, `
		SELECT repo, branch, state FROM agent_assignments WHERE assignment_id = ?`, assignmentID)

	var repo, branch, state string
	if err := row.Scan(&repo, &branch, &state); err != nil {
		return false, "unknown", "assignment not found"
	}

	// Check latest review run status for this repo/branch.
	runRow := db.QueryRowContext(ctx, `
		SELECT status FROM review_runs
		WHERE repo = ? AND branch = ?
		ORDER BY created_at DESC
		LIMIT 1`, repo, branch)

	var runStatus string
	if err := runRow.Scan(&runStatus); err != nil {
		return true, "none", "no gate runs found"
	}

	switch runStatus {
	case "completed":
		return true, "passed", ""
	case "failed":
		return false, "failed", "gate check failed"
	case "running":
		return false, "running", "gate check in progress"
	default:
		return false, runStatus, "gate check status: " + runStatus
	}
}

// ─── §9 Compliance violation queries ────────────────────────────────────

func queryComplianceViolations(ctx context.Context, db *sql.DB, limit int) ([]AssignmentRuleCheckRow, error) {
	hasTable, err := tableExists(ctx, db, "assignment_rule_checks")
	if err != nil {
		return nil, err
	}
	if !hasTable {
		return []AssignmentRuleCheckRow{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
		       result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
		FROM assignment_rule_checks
		WHERE violation_raised = 1
		ORDER BY checked_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AssignmentRuleCheckRow
	for rows.Next() {
		var check AssignmentRuleCheckRow
		var violationRaised int
		var violationAction string
		var resolvedAt sql.NullTime
		if err := rows.Scan(
			&check.CheckID, &check.AssignmentID, &check.SessionID, &check.RuleID, &check.RuleVersion, &check.CheckedAt,
			&check.Result, &violationRaised, &violationAction, &check.Detail, &resolvedAt, &check.ResolvedBy,
		); err != nil {
			return nil, err
		}
		if violationAction != "" {
			_ = json.Unmarshal([]byte(violationAction), &check.ViolationActionTaken)
		}
		check.ViolationRaised = violationRaised != 0
		check.ResolvedAt = nullTimePtr(resolvedAt)
		out = append(out, check)
	}
	if out == nil {
		out = []AssignmentRuleCheckRow{}
	}
	return out, rows.Err()
}

// ─── §10 Queue queries ──────────────────────────────────────────────────

func queryQueue(ctx context.Context, db *sql.DB) ([]QueueItem, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, repo, branch, state, queue_priority, owner_session_id,
		       submission_time, created_at
		FROM branch_states
		WHERE state IN ('queued_cli', 'cli_reviewing', 'merge_ready', 'submitted', 'waiting', 'review_approved')
		ORDER BY queue_priority DESC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []QueueItem
	for rows.Next() {
		var q QueueItem
		var subTime, createdAt sql.NullTime
		if err := rows.Scan(&q.ID, &q.Repo, &q.Branch, &q.State, &q.Priority, &q.OwnerSessionID, &subTime, &createdAt); err != nil {
			return nil, err
		}
		if subTime.Valid {
			q.SubmissionTime = subTime.Time
		} else if createdAt.Valid {
			q.SubmissionTime = createdAt.Time
		}
		out = append(out, q)
	}
	if out == nil {
		out = []QueueItem{}
	}
	return out, rows.Err()
}

func queryQueueStats(ctx context.Context, db *sql.DB) (pending, active, blocked, total int, err error) {
	err = db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN state IN ('queued_cli', 'merge_ready') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN state IN ('submitted', 'waiting', 'cli_reviewing', 'review_approved') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN state = 'blocked' THEN 1 ELSE 0 END), 0),
			COUNT(*)
		FROM branch_states`).Scan(&pending, &active, &blocked, &total)
	return
}

// ─── §2.8 Session archives queries ───────────────────────────────────────

// ArchiveRow is the API response shape for a session archive.
type ArchiveRow struct {
	ArchiveID       string `json:"archive_id"`
	SessionID       string `json:"session_id"`
	AgentID         string `json:"agent_id"`
	Result          string `json:"result"`
	Repo            string `json:"repo"`
	Branch          string `json:"branch"`
	TaskID          string `json:"task_id,omitempty"`
	TaskSource      string `json:"task_source,omitempty"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at"`
	DurationSeconds int    `json:"duration_seconds"`
	CommitCount     int    `json:"commit_count"`
	MergeSHA        string `json:"merge_sha,omitempty"`
}

// deriveCheckpoint maps session status + end_reason to a lifecycle checkpoint.
func deriveCheckpoint(status, endReason string) string {
	switch {
	case status == "active":
		return "CODING"
	case status == "ended" && endReason == "completed":
		return "FINALIZED"
	case status == "ended" && endReason == "expired":
		return "EXPIRED"
	case status == "ended" && endReason == "failed":
		return "FAILED"
	case status == "ended":
		return "ENDED"
	default:
		return "LAUNCHED"
	}
}

func querySessionArchives(ctx context.Context, db *sql.DB, limit int) ([]ArchiveRow, error) {
	exists, err := tableExists(ctx, db, "session_archives")
	if err != nil {
		return nil, fmt.Errorf("check session_archives: %w", err)
	}
	if !exists {
		return []ArchiveRow{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT archive_id, session_id, agent_id, result,
		       COALESCE(repo, ''), COALESCE(branch, ''),
		       COALESCE(task_id, ''), COALESCE(task_source, ''),
		       COALESCE(started_at, ''), COALESCE(ended_at, ''),
		       COALESCE(duration_seconds, 0), COALESCE(commit_count, 0),
		       COALESCE(merge_sha, '')
		FROM session_archives
		ORDER BY ended_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArchiveRow
	for rows.Next() {
		var a ArchiveRow
		if err := rows.Scan(&a.ArchiveID, &a.SessionID, &a.AgentID, &a.Result,
			&a.Repo, &a.Branch, &a.TaskID, &a.TaskSource,
			&a.StartedAt, &a.EndedAt,
			&a.DurationSeconds, &a.CommitCount, &a.MergeSHA); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ─── Pipeline query (MIG-035) ─────────────────────────────────────────────

func queryPipeline(ctx context.Context, db *sql.DB) ([]PipelineCard, error) {
	hasSessions, err := tableExists(ctx, db, "agent_sessions")
	if err != nil {
		return nil, err
	}
	if !hasSessions {
		return []PipelineCard{}, nil
	}

	hasAssignments, err := tableExists(ctx, db, "agent_assignments")
	if err != nil {
		return nil, err
	}
	if !hasAssignments {
		return []PipelineCard{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT s.session_id, s.agent_id, COALESCE(a.assignment_id, ''), COALESCE(a.task_id, ''),
		       COALESCE(a.repo, ''), COALESCE(a.branch, ''), COALESCE(a.state, ''),
		       COALESCE(a.assignment_substatus, ''), COALESCE(a.assignment_version, 0), a.started_at, s.last_seen_at,
		       COALESCE(bs.pr_number, 0)
		FROM agent_sessions s
		LEFT JOIN agent_assignments a ON a.session_id = s.session_id AND a.ended_at IS NULL
		LEFT JOIN branch_states bs ON bs.repo = COALESCE(a.repo, '') AND bs.branch = COALESCE(a.branch, '')
		WHERE s.ended_at IS NULL
		ORDER BY s.started_at DESC
		LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PipelineCard
	for rows.Next() {
		var card PipelineCard
		var startedAt, updatedAt sql.NullTime
		var version int
		if err := rows.Scan(&card.SessionID, &card.AgentID, &card.AssignmentID, &card.TaskID, &card.Repo, &card.Branch, &card.State, &card.Substatus, &version, &startedAt, &updatedAt, &card.PRNumber); err != nil {
			return nil, err
		}
		card.Checkpoint = pipelineCardStageLabel(card.Substatus, card.State)
		card.Version = version
		if startedAt.Valid {
			card.StartedAt = startedAt.Time
		}
		if updatedAt.Valid {
			card.UpdatedAt = updatedAt.Time
		}
		card.StageSec = pipelineCardDuration(card.StartedAt, card.UpdatedAt)
		out = append(out, card)
	}
	if out == nil {
		out = []PipelineCard{}
	}
	return out, rows.Err()
}

func deriveCheckpointFromSubstatus(substatus string) string {
	switch substatus {
	case "in_progress", "needs_revision":
		return "GATING"
	case "waiting_for_ci":
		return "PUSHED"
	case "waiting_for_merge_approval":
		return "PR_ACTIVE"
	case "terminal_finished", "terminal_waiting_comments", "terminal_waiting_next_task":
		return "MERGED"
	case "blocked_credential_failure", "blocked_merge_conflict", "blocked_external_dependency", "blocked_ci_failure", "blocked_policy":
		return "MONITORING"
	default:
		return "SUBMITTED"
	}
}
