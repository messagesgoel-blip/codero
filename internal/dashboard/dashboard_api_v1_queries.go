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

	// Count total matching rows.
	countQuery := `SELECT COUNT(*) FROM agent_sessions`
	args := []interface{}{}
	if status != "" {
		switch status {
		case "active":
			countQuery += ` WHERE ended_at IS NULL`
		case "ended":
			countQuery += ` WHERE ended_at IS NOT NULL`
		}
	}
	var total int
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("querySessions: count: %w", err)
	}

	// Query rows.
	query := `SELECT session_id, agent_id, mode, started_at, last_seen_at, ended_at, end_reason FROM agent_sessions`
	if status != "" {
		switch status {
		case "active":
			query += ` WHERE ended_at IS NULL`
		case "ended":
			query += ` WHERE ended_at IS NOT NULL`
		}
	}
	query += ` ORDER BY started_at DESC LIMIT ? OFFSET ?`

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
		if err := rows.Scan(&s.SessionID, &s.AgentID, &s.Mode, &s.StartedAt, &s.LastSeenAt, &endedAt, &endReason); err != nil {
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
		SELECT session_id, agent_id, mode, started_at, last_seen_at, ended_at, end_reason
		FROM agent_sessions WHERE session_id = ?`, sessionID)

	var s SessionRow
	var endedAt sql.NullTime
	var endReason sql.NullString
	if err := row.Scan(&s.SessionID, &s.AgentID, &s.Mode, &s.StartedAt, &s.LastSeenAt, &endedAt, &endReason); err != nil {
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
		WHERE state IN ('queued_cli', 'cli_reviewing', 'merge_ready', 'coding', 'local_review', 'reviewed')
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
			COALESCE(SUM(CASE WHEN state IN ('coding', 'local_review', 'cli_reviewing', 'reviewed') THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN state = 'blocked' THEN 1 ELSE 0 END), 0),
			COUNT(*)
		FROM branch_states`).Scan(&pending, &active, &blocked, &total)
	return
}
