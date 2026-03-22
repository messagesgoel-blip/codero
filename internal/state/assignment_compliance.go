package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RuleIDGateMustPassBeforeMerge = "RULE-001"
	RuleIDNoSilentFailure         = "RULE-002"
	RuleIDBranchHoldTTL           = "RULE-003"
	RuleIDHeartbeatProgress       = "RULE-004"

	AssignmentSubstatusInProgress                = "in_progress"
	AssignmentSubstatusWaitingForCI              = "waiting_for_ci"
	AssignmentSubstatusWaitingForMergeApproval   = "waiting_for_merge_approval"
	AssignmentSubstatusBlockedCredentialFailure  = "blocked_credential_failure"
	AssignmentSubstatusBlockedMergeConflict      = "blocked_merge_conflict"
	AssignmentSubstatusBlockedExternalDependency = "blocked_external_dependency"
	AssignmentSubstatusBlockedCIFailure          = "blocked_ci_failure"
	AssignmentSubstatusBlockedPolicy             = "blocked_policy"
	AssignmentSubstatusTerminalFinished          = "terminal_finished"
	AssignmentSubstatusTerminalWaitingComments   = "terminal_waiting_for_comments"
	AssignmentSubstatusTerminalWaitingNextTask   = "terminal_waiting_for_next_task"
	AssignmentSubstatusTerminalCancelled         = "terminal_cancelled"
	AssignmentSubstatusTerminalLost              = "terminal_lost"
	AssignmentSubstatusTerminalStuckAbandoned    = "terminal_stuck_abandoned"
)

var (
	ErrAssignmentSubstatusRequired   = errors.New("assignment substatus required")
	ErrInvalidAssignmentSubstatus    = errors.New("invalid assignment substatus")
	ErrAssignmentGateNotPassed       = errors.New("assignment gate must pass before completion")
	ErrAssignmentComplianceNotPassed = errors.New("assignment compliance rules must pass before completion")
)

type assignmentLifecycleState string

const (
	assignmentStateActive     assignmentLifecycleState = "active"
	assignmentStateBlocked    assignmentLifecycleState = "blocked"
	assignmentStateCompleted  assignmentLifecycleState = "completed"
	assignmentStateCancelled  assignmentLifecycleState = "cancelled"
	assignmentStateSuperseded assignmentLifecycleState = "superseded"
	assignmentStateLost       assignmentLifecycleState = "lost"
)

const (
	assignmentBranchHoldTTL            = 72 * time.Hour
	assignmentBranchHoldForceCancelTTL = assignmentBranchHoldTTL + (assignmentBranchHoldTTL / 2)
	assignmentHeartbeatTTL             = 30 * time.Second
	assignmentProgressTTL              = 60 * time.Minute
)

type agentRuleDefinition struct {
	RuleID          string
	RuleName        string
	RuleKind        string
	Description     string
	Enforcement     string
	ViolationAction []string
	RoutingTarget   string
	RuleVersion     int
	Active          bool
}

var defaultAgentRuleDefinitions = map[string]agentRuleDefinition{
	RuleIDGateMustPassBeforeMerge: {
		RuleID:          RuleIDGateMustPassBeforeMerge,
		RuleName:        "Gate must pass before merge",
		RuleKind:        "gate",
		Description:     "An agent may not initiate, trigger, or approve a merge to the target branch until all gate checks for that assignment have a result of pass in assignment_rule_checks. Codero intercepts any merge-triggering action and rejects it if this condition is not met.",
		Enforcement:     "hard",
		ViolationAction: []string{"block", "notify"},
		RoutingTarget:   "routing_team",
		RuleVersion:     1,
		Active:          true,
	},
	RuleIDNoSilentFailure: {
		RuleID:          RuleIDNoSilentFailure,
		RuleName:        "No silent failure",
		RuleKind:        "report",
		Description:     "An agent may not transition an assignment to blocked or any terminal state without supplying a substatus from the approved enum. Codero rejects any state transition that arrives without a substatus on these states.",
		Enforcement:     "hard",
		ViolationAction: []string{"block", "fail"},
		RoutingTarget:   "routing_team",
		RuleVersion:     1,
		Active:          true,
	},
	RuleIDBranchHoldTTL: {
		RuleID:          RuleIDBranchHoldTTL,
		RuleName:        "Branch hold TTL",
		RuleKind:        "hold",
		Description:     "An agent may not hold ownership of a branch for longer than branch_hold_TTL (default: 72 hours). At 1.5x TTL, Codero forcibly releases branch ownership and transitions the assignment to cancelled.",
		Enforcement:     "hard",
		ViolationAction: []string{"block", "notify"},
		RoutingTarget:   "tech_lead",
		RuleVersion:     1,
		Active:          true,
	},
	RuleIDHeartbeatProgress: {
		RuleID:          RuleIDHeartbeatProgress,
		RuleName:        "Heartbeat and progress protocol",
		RuleKind:        "protocol",
		Description:     "An agent must emit a heartbeat every 30 seconds and advance progress_at at least every 60 minutes. Failure on heartbeat triggers the lost path. Failure on progress triggers the stuck path.",
		Enforcement:     "hard",
		ViolationAction: []string{"block", "log", "notify"},
		RoutingTarget:   "infra",
		RuleVersion:     1,
		Active:          true,
	},
}

var activeAssignmentSubstatusSet = map[string]struct{}{
	AssignmentSubstatusInProgress:              {},
	AssignmentSubstatusWaitingForCI:            {},
	AssignmentSubstatusWaitingForMergeApproval: {},
}

var blockedAssignmentSubstatusSet = map[string]struct{}{
	AssignmentSubstatusBlockedCredentialFailure:  {},
	AssignmentSubstatusBlockedMergeConflict:      {},
	AssignmentSubstatusBlockedExternalDependency: {},
	AssignmentSubstatusBlockedCIFailure:          {},
	AssignmentSubstatusBlockedPolicy:             {},
}

var completedAssignmentSubstatusSet = map[string]struct{}{
	AssignmentSubstatusTerminalFinished:        {},
	AssignmentSubstatusTerminalWaitingComments: {},
	AssignmentSubstatusTerminalWaitingNextTask: {},
}

var terminalAssignmentSubstatusSet = map[string]struct{}{
	AssignmentSubstatusTerminalFinished:        {},
	AssignmentSubstatusTerminalWaitingComments: {},
	AssignmentSubstatusTerminalWaitingNextTask: {},
	AssignmentSubstatusTerminalCancelled:       {},
	AssignmentSubstatusTerminalLost:            {},
	AssignmentSubstatusTerminalStuckAbandoned:  {},
}

func normalizeAssignmentSubstatus(substatus string) string {
	return strings.ToLower(strings.TrimSpace(substatus))
}

func validateAttachAssignmentSubstatus(substatus string) (string, error) {
	normalized := normalizeAssignmentSubstatus(substatus)
	if normalized == "" {
		return AssignmentSubstatusInProgress, nil
	}
	if _, ok := activeAssignmentSubstatusSet[normalized]; ok {
		return normalized, nil
	}
	return "", fmt.Errorf("%w: %q", ErrInvalidAssignmentSubstatus, normalized)
}

func validateTerminalAssignmentSubstatus(status, substatus string) (assignmentLifecycleState, string, error) {
	normalizedStatus := strings.ToLower(strings.TrimSpace(status))
	normalizedSubstatus := normalizeAssignmentSubstatus(substatus)
	if normalizedSubstatus == "" {
		return "", "", ErrAssignmentSubstatusRequired
	}

	switch normalizedStatus {
	case "blocked":
		if _, ok := blockedAssignmentSubstatusSet[normalizedSubstatus]; !ok {
			return "", "", fmt.Errorf("%w: blocked transitions require blocked_* substatus, got %q", ErrInvalidAssignmentSubstatus, normalizedSubstatus)
		}
		return assignmentStateBlocked, normalizedSubstatus, nil
	case "cancelled", "canceled":
		if normalizedSubstatus != AssignmentSubstatusTerminalCancelled {
			return "", "", fmt.Errorf("%w: cancelled transitions require %q, got %q", ErrInvalidAssignmentSubstatus, AssignmentSubstatusTerminalCancelled, normalizedSubstatus)
		}
		return assignmentStateCancelled, normalizedSubstatus, nil
	case "lost", "expired":
		if normalizedSubstatus != AssignmentSubstatusTerminalLost && normalizedSubstatus != AssignmentSubstatusTerminalStuckAbandoned {
			return "", "", fmt.Errorf("%w: lost transitions require %q or %q, got %q", ErrInvalidAssignmentSubstatus, AssignmentSubstatusTerminalLost, AssignmentSubstatusTerminalStuckAbandoned, normalizedSubstatus)
		}
		return assignmentStateLost, normalizedSubstatus, nil
	}

	if _, ok := blockedAssignmentSubstatusSet[normalizedSubstatus]; ok {
		if normalizedStatus != "" && normalizedStatus != "blocked" {
			return "", "", fmt.Errorf("%w: status %q conflicts with blocked substatus %q", ErrInvalidAssignmentSubstatus, normalizedStatus, normalizedSubstatus)
		}
		return assignmentStateBlocked, normalizedSubstatus, nil
	}
	if normalizedSubstatus == AssignmentSubstatusTerminalCancelled {
		if normalizedStatus != "" && normalizedStatus != "cancelled" && normalizedStatus != "canceled" {
			return "", "", fmt.Errorf("%w: status %q conflicts with cancelled substatus %q", ErrInvalidAssignmentSubstatus, normalizedStatus, normalizedSubstatus)
		}
		return assignmentStateCancelled, normalizedSubstatus, nil
	}
	if normalizedSubstatus == AssignmentSubstatusTerminalLost || normalizedSubstatus == AssignmentSubstatusTerminalStuckAbandoned {
		if normalizedStatus != "" && normalizedStatus != "lost" && normalizedStatus != "expired" {
			return "", "", fmt.Errorf("%w: status %q conflicts with lost substatus %q", ErrInvalidAssignmentSubstatus, normalizedStatus, normalizedSubstatus)
		}
		return assignmentStateLost, normalizedSubstatus, nil
	}
	if _, ok := completedAssignmentSubstatusSet[normalizedSubstatus]; ok {
		return assignmentStateCompleted, normalizedSubstatus, nil
	}
	if _, ok := terminalAssignmentSubstatusSet[normalizedSubstatus]; ok {
		return assignmentStateCompleted, normalizedSubstatus, nil
	}
	return "", "", fmt.Errorf("%w: %q", ErrInvalidAssignmentSubstatus, normalizedSubstatus)
}

func assignmentActivityStateFromSubstatus(substatus string) string {
	normalized := normalizeAssignmentSubstatus(substatus)
	switch {
	case normalized == "":
		return "active"
	case strings.HasPrefix(normalized, "blocked_"):
		return "blocked"
	case strings.HasPrefix(normalized, "waiting_for_"):
		return "waiting"
	default:
		return "active"
	}
}

func assignmentStateFromSubstatus(substatus string) string {
	normalized := normalizeAssignmentSubstatus(substatus)
	switch {
	case normalized == "":
		return ""
	case strings.HasPrefix(normalized, "blocked_"):
		return string(assignmentStateBlocked)
	case normalized == AssignmentSubstatusTerminalCancelled:
		return string(assignmentStateCancelled)
	case normalized == AssignmentSubstatusTerminalLost || normalized == AssignmentSubstatusTerminalStuckAbandoned:
		return string(assignmentStateLost)
	case strings.HasPrefix(normalized, "terminal_"):
		return string(assignmentStateCompleted)
	default:
		return string(assignmentStateActive)
	}
}

func blockedReasonFromSubstatus(substatus string) string {
	normalized := normalizeAssignmentSubstatus(substatus)
	if strings.HasPrefix(normalized, "blocked_") {
		return strings.TrimPrefix(normalized, "blocked_")
	}
	return ""
}

func seedPendingAssignmentRuleChecksTx(ctx context.Context, tx *sql.Tx, assignmentID, sessionID string) error {
	rules, err := listActiveAgentRuleDefinitionsTx(ctx, tx)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO assignment_rule_checks (
				check_id, assignment_id, session_id, rule_id, rule_version, result
			) VALUES (?, ?, ?, ?, ?, 'pending')`,
			uuid.NewString(), assignmentID, sessionID, rule.RuleID, rule.RuleVersion,
		); err != nil {
			return fmt.Errorf("seed assignment rule checks: insert %s: %w", rule.RuleID, err)
		}
	}
	return nil
}

func listActiveAgentRuleDefinitionsTx(ctx context.Context, tx *sql.Tx) ([]agentRuleDefinition, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT rule_id, rule_name, rule_kind, description, enforcement,
		       violation_action, routing_target, rule_version, active
		FROM agent_rules
		WHERE active = 1
		ORDER BY rule_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list active agent rules: %w", err)
	}
	defer rows.Close()

	var rules []agentRuleDefinition
	for rows.Next() {
		var (
			rule            agentRuleDefinition
			violationAction string
			active          int
		)
		if err := rows.Scan(
			&rule.RuleID, &rule.RuleName, &rule.RuleKind, &rule.Description, &rule.Enforcement,
			&violationAction, &rule.RoutingTarget, &rule.RuleVersion, &active,
		); err != nil {
			return nil, fmt.Errorf("list active agent rules: scan: %w", err)
		}
		if violationAction != "" {
			if err := json.Unmarshal([]byte(violationAction), &rule.ViolationAction); err != nil {
				return nil, fmt.Errorf("list active agent rules: decode %s: %w", rule.RuleID, err)
			}
		}
		rule.Active = active != 0
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active agent rules: rows: %w", err)
	}
	if len(rules) > 0 {
		return rules, nil
	}

	fallback := make([]agentRuleDefinition, 0, len(defaultAgentRuleDefinitions))
	for _, ruleID := range []string{
		RuleIDGateMustPassBeforeMerge,
		RuleIDNoSilentFailure,
		RuleIDBranchHoldTTL,
		RuleIDHeartbeatProgress,
	} {
		fallback = append(fallback, defaultAgentRuleDefinitions[ruleID])
	}
	return fallback, nil
}

func loadAgentRuleDefinitionTx(ctx context.Context, tx *sql.Tx, ruleID string) (agentRuleDefinition, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT rule_id, rule_name, rule_kind, description, enforcement,
		       violation_action, routing_target, rule_version, active
		FROM agent_rules
		WHERE rule_id = ?`,
		ruleID,
	)
	var (
		rule            agentRuleDefinition
		violationAction string
		active          int
	)
	if err := row.Scan(
		&rule.RuleID, &rule.RuleName, &rule.RuleKind, &rule.Description, &rule.Enforcement,
		&violationAction, &rule.RoutingTarget, &rule.RuleVersion, &active,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if fallback, ok := defaultAgentRuleDefinitions[ruleID]; ok {
				return fallback, nil
			}
			return agentRuleDefinition{}, fmt.Errorf("load agent rule %s: %w", ruleID, err)
		}
		return agentRuleDefinition{}, fmt.Errorf("load agent rule %s: %w", ruleID, err)
	}
	if violationAction != "" {
		if err := json.Unmarshal([]byte(violationAction), &rule.ViolationAction); err != nil {
			return agentRuleDefinition{}, fmt.Errorf("load agent rule %s: decode actions: %w", ruleID, err)
		}
	}
	rule.Active = active != 0
	return rule, nil
}

func recordAssignmentRuleCheckTx(
	ctx context.Context,
	tx *sql.Tx,
	assignmentID, sessionID, ruleID, result string,
	violationRaised bool,
	detail any,
	resolvedBy string,
) error {
	rule, err := loadAgentRuleDefinitionTx(ctx, tx, ruleID)
	if err != nil {
		return err
	}

	actions := []string{}
	if violationRaised {
		actions = rule.ViolationAction
	}
	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return fmt.Errorf("record assignment rule check %s: marshal actions: %w", ruleID, err)
	}

	detailJSON := "{}"
	if detail != nil {
		body, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("record assignment rule check %s: marshal detail: %w", ruleID, err)
		}
		detailJSON = string(body)
	}

	var res sql.Result
	if result == "pass" {
		res, err = tx.ExecContext(ctx, `
			UPDATE assignment_rule_checks
			SET checked_at = datetime('now'),
			    result = ?,
			    violation_raised = 0,
			    violation_action_taken = ?,
			    detail = ?,
			    resolved_at = datetime('now'),
			    resolved_by = ?
			WHERE assignment_id = ? AND rule_id = ?`,
			result, string(actionsJSON), detailJSON, resolvedBy, assignmentID, ruleID,
		)
	} else {
		res, err = tx.ExecContext(ctx, `
			UPDATE assignment_rule_checks
			SET checked_at = datetime('now'),
			    result = ?,
			    violation_raised = 1,
			    violation_action_taken = ?,
			    detail = ?,
			    resolved_at = NULL,
			    resolved_by = ''
			WHERE assignment_id = ? AND rule_id = ?`,
			result, string(actionsJSON), detailJSON, assignmentID, ruleID,
		)
	}
	if err != nil {
		return fmt.Errorf("record assignment rule check %s: update: %w", ruleID, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("record assignment rule check %s: rows affected: %w", ruleID, err)
	}
	if rows > 0 {
		return nil
	}

	if result == "pass" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO assignment_rule_checks (
				check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
				result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
			) VALUES (?, ?, ?, ?, ?, datetime('now'), ?, 0, ?, ?, datetime('now'), ?)`,
			uuid.NewString(), assignmentID, sessionID, rule.RuleID, rule.RuleVersion, result, string(actionsJSON), detailJSON, resolvedBy,
		)
	} else {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO assignment_rule_checks (
				check_id, assignment_id, session_id, rule_id, rule_version, checked_at,
				result, violation_raised, violation_action_taken, detail, resolved_at, resolved_by
			) VALUES (?, ?, ?, ?, ?, datetime('now'), ?, 1, ?, ?, NULL, '')`,
			uuid.NewString(), assignmentID, sessionID, rule.RuleID, rule.RuleVersion, result, string(actionsJSON), detailJSON,
		)
	}
	if err != nil {
		return fmt.Errorf("record assignment rule check %s: insert: %w", ruleID, err)
	}
	return nil
}

func evaluateRule001CompletionTx(ctx context.Context, tx *sql.Tx, assignment *AgentAssignment) (bool, map[string]any, error) {
	if assignment == nil || assignment.Repo == "" || assignment.Branch == "" {
		return false, map[string]any{
			"reason": "missing_branch_context",
		}, nil
	}

	var (
		branchState       string
		approvedInt       int
		ciGreenInt        int
		pendingEvents     int
		unresolvedThreads int
	)
	err := tx.QueryRowContext(ctx, `
		SELECT state, approved, ci_green, pending_events, unresolved_threads
		FROM branch_states
		WHERE repo = ? AND branch = ?`,
		assignment.Repo, assignment.Branch,
	).Scan(&branchState, &approvedInt, &ciGreenInt, &pendingEvents, &unresolvedThreads)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, map[string]any{
				"reason": "branch_not_found",
			}, nil
		}
		return false, nil, fmt.Errorf("evaluate RULE-001: load branch %s/%s: %w", assignment.Repo, assignment.Branch, err)
	}

	pass := (branchState == string(StateMergeReady) || branchState == string(StateClosed)) &&
		approvedInt != 0 && ciGreenInt != 0 && pendingEvents == 0 && unresolvedThreads == 0
	return pass, map[string]any{
		"branch_state":       branchState,
		"approved":           approvedInt != 0,
		"ci_green":           ciGreenInt != 0,
		"pending_events":     pendingEvents,
		"unresolved_threads": unresolvedThreads,
		"reason":             mergeGateReason(pass, branchState, approvedInt != 0, ciGreenInt != 0, pendingEvents, unresolvedThreads),
	}, nil
}

func mergeGateReason(pass bool, branchState string, approved, ciGreen bool, pendingEvents, unresolvedThreads int) string {
	if pass {
		return "merge_ready"
	}
	switch {
	case branchState == "":
		return "branch_not_found"
	case branchState != string(StateMergeReady) && branchState != string(StateClosed):
		return "branch_not_merge_ready"
	case !approved:
		return "approval_missing"
	case !ciGreen:
		return "ci_not_green"
	case pendingEvents > 0:
		return "pending_events"
	case unresolvedThreads > 0:
		return "unresolved_threads"
	default:
		return "merge_gate_blocked"
	}
}

func evaluateRule003BranchHoldTTL(now time.Time, assignment *AgentAssignment) (bool, bool, map[string]any) {
	if assignment == nil {
		return false, false, map[string]any{
			"reason": "assignment_missing",
		}
	}
	age := now.UTC().Sub(assignment.StartedAt.UTC())
	if age < 0 {
		age = 0
	}
	pass := age <= assignmentBranchHoldTTL
	forceCancel := age > assignmentBranchHoldForceCancelTTL
	reason := "within_ttl"
	switch {
	case forceCancel:
		reason = "force_release_required"
	case !pass:
		reason = "branch_hold_ttl_exceeded"
	}
	return pass, forceCancel, map[string]any{
		"hold_age_seconds":         int64(age / time.Second),
		"hold_ttl_seconds":         int64(assignmentBranchHoldTTL / time.Second),
		"force_cancel_ttl_seconds": int64(assignmentBranchHoldForceCancelTTL / time.Second),
		"reason":                   reason,
	}
}

type rule004Evaluation struct {
	Pass           bool
	FailurePath    string
	Classification string
	Detail         map[string]any
}

func evaluateRule004HeartbeatProgress(now time.Time, session *AgentSession, assignment *AgentAssignment) rule004Evaluation {
	if session == nil {
		return rule004Evaluation{
			Pass:           false,
			FailurePath:    "lost",
			Classification: "infrastructure_failure",
			Detail: map[string]any{
				"reason": "session_missing",
			},
		}
	}

	heartbeatAge := now.UTC().Sub(session.LastSeenAt.UTC())
	if heartbeatAge < 0 {
		heartbeatAge = 0
	}
	progressRef := session.StartedAt
	if session.LastProgressAt != nil {
		progressRef = *session.LastProgressAt
	} else if assignment != nil && assignment.StartedAt.After(progressRef) {
		progressRef = assignment.StartedAt
	}
	progressAge := now.UTC().Sub(progressRef.UTC())
	if progressAge < 0 {
		progressAge = 0
	}

	detail := map[string]any{
		"heartbeat_age_seconds": int64(heartbeatAge / time.Second),
		"heartbeat_ttl_seconds": int64(assignmentHeartbeatTTL / time.Second),
		"progress_age_seconds":  int64(progressAge / time.Second),
		"progress_ttl_seconds":  int64(assignmentProgressTTL / time.Second),
		"reason":                "protocol_ok",
	}
	if session.LastProgressAt != nil {
		detail["last_progress_at"] = session.LastProgressAt.UTC().Format(time.RFC3339)
	}

	switch {
	case heartbeatAge > assignmentHeartbeatTTL:
		detail["reason"] = "heartbeat_missing"
		return rule004Evaluation{
			Pass:           false,
			FailurePath:    "lost",
			Classification: "infrastructure_failure",
			Detail:         detail,
		}
	case progressAge > assignmentProgressTTL:
		detail["reason"] = "progress_stale"
		return rule004Evaluation{
			Pass:           false,
			FailurePath:    "stuck",
			Classification: "stuck_assignment",
			Detail:         detail,
		}
	default:
		return rule004Evaluation{
			Pass:   true,
			Detail: detail,
		}
	}
}

func activeRuleBlockersTx(ctx context.Context, tx *sql.Tx, assignmentID string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT arc.rule_id, arc.result
		FROM assignment_rule_checks arc
		JOIN agent_rules ar ON ar.rule_id = arc.rule_id
		WHERE arc.assignment_id = ?
		  AND ar.active = 1
		  AND arc.result <> 'pass'
		ORDER BY arc.rule_id ASC`,
		assignmentID,
	)
	if err != nil {
		return nil, fmt.Errorf("list assignment rule blockers: %w", err)
	}
	defer rows.Close()

	blockers := map[string]string{}
	for rows.Next() {
		var ruleID, result string
		if err := rows.Scan(&ruleID, &result); err != nil {
			return nil, fmt.Errorf("list assignment rule blockers: scan: %w", err)
		}
		blockers[ruleID] = result
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assignment rule blockers: rows: %w", err)
	}
	return blockers, nil
}

func assignmentCompletionError(blockers map[string]string) error {
	if len(blockers) == 1 && blockers[RuleIDGateMustPassBeforeMerge] == "fail" {
		return ErrAssignmentGateNotPassed
	}
	if len(blockers) > 0 {
		return ErrAssignmentComplianceNotPassed
	}
	return nil
}
