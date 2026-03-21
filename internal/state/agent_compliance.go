package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type defaultAgentRule struct {
	RuleID          string
	RuleName        string
	RuleKind        string
	Description     string
	Enforcement     string
	ViolationAction []string
	RoutingTarget   string
	RuleVersion     int
}

type AssignmentRuleCheck struct {
	CheckID         string
	AssignmentID    string
	SessionID       string
	RuleID          string
	Result          string
	ViolationRaised bool
	Detail          string
}

var baselineAgentRules = []defaultAgentRule{
	{
		RuleID:          "RULE-001",
		RuleName:        "Gate must pass before merge",
		RuleKind:        "gate",
		Description:     "All CI/CD gates must pass before an agent may merge a pull request.",
		Enforcement:     "hard",
		ViolationAction: []string{"block", "notify"},
		RoutingTarget:   "routing_team",
		RuleVersion:     1,
	},
	{
		RuleID:          "RULE-002",
		RuleName:        "No silent failure",
		RuleKind:        "report",
		Description:     "An agent may not transition to blocked or any terminal state without setting a substatus.",
		Enforcement:     "hard",
		ViolationAction: []string{"block", "fail"},
		RoutingTarget:   "routing_team",
		RuleVersion:     1,
	},
	{
		RuleID:          "RULE-003",
		RuleName:        "Branch hold TTL",
		RuleKind:        "hold",
		Description:     "An agent must not hold a branch beyond the defined TTL.",
		Enforcement:     "hard",
		ViolationAction: []string{"block", "notify"},
		RoutingTarget:   "tech_lead",
		RuleVersion:     1,
	},
	{
		RuleID:          "RULE-004",
		RuleName:        "Heartbeat and progress protocol",
		RuleKind:        "protocol",
		Description:     "Agent must send heartbeats within TTL and update progress_at while active work continues.",
		Enforcement:     "hard",
		ViolationAction: []string{"block", "log", "notify"},
		RoutingTarget:   "infra",
		RuleVersion:     1,
	},
}

func ensureBaselineAgentRulesTx(ctx context.Context, tx *sql.Tx) error {
	for _, rule := range baselineAgentRules {
		violationAction, err := json.Marshal(rule.ViolationAction)
		if err != nil {
			return fmt.Errorf("ensure baseline agent rules: marshal %s actions: %w", rule.RuleID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO agent_rules (
				rule_id, rule_name, rule_kind, description, enforcement,
				violation_action, routing_target, rule_version, active
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(rule_id) DO UPDATE SET
				rule_name = excluded.rule_name,
				rule_kind = excluded.rule_kind,
				description = excluded.description,
				enforcement = excluded.enforcement,
				violation_action = excluded.violation_action,
				routing_target = excluded.routing_target,
				rule_version = excluded.rule_version,
				active = 1`,
			rule.RuleID, rule.RuleName, rule.RuleKind, rule.Description, rule.Enforcement,
			string(violationAction), rule.RoutingTarget, rule.RuleVersion,
		); err != nil {
			return fmt.Errorf("ensure baseline agent rules: upsert %s: %w", rule.RuleID, err)
		}
	}
	return nil
}

func createInitialAssignmentRuleChecksTx(ctx context.Context, tx *sql.Tx, assignment *AgentAssignment) error {
	checks := []struct {
		CheckID              string
		RuleID               string
		RuleVersion          int
		Result               string
		ViolationRaised      int
		ViolationActionTaken []string
		Detail               string
	}{
		{
			CheckID:     assignment.ID + ":RULE-001",
			RuleID:      "RULE-001",
			RuleVersion: 1,
			Result:      "pending",
			Detail:      `{"source":"assignment_attach"}`,
		},
		{
			CheckID:     assignment.ID + ":RULE-002",
			RuleID:      "RULE-002",
			RuleVersion: 1,
			Result:      "pass",
			Detail:      `{"source":"assignment_attach"}`,
		},
		{
			CheckID:     assignment.ID + ":RULE-003",
			RuleID:      "RULE-003",
			RuleVersion: 1,
			Result:      "pass",
			Detail:      `{"source":"assignment_attach","branch_hold":"fresh"}`,
		},
		{
			CheckID:     assignment.ID + ":RULE-004",
			RuleID:      "RULE-004",
			RuleVersion: 1,
			Result:      "pass",
			Detail:      `{"source":"assignment_attach","progress":"fresh"}`,
		},
	}

	for _, check := range checks {
		violationActionTaken, err := json.Marshal(check.ViolationActionTaken)
		if err != nil {
			return fmt.Errorf("create assignment rule checks: marshal %s actions: %w", check.RuleID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO assignment_rule_checks (
				check_id, assignment_id, session_id, rule_id, rule_version,
				result, violation_raised, violation_action_taken, detail
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			check.CheckID, assignment.ID, assignment.SessionID, check.RuleID, check.RuleVersion,
			check.Result, check.ViolationRaised, string(violationActionTaken), check.Detail,
		); err != nil {
			return fmt.Errorf("create assignment rule checks: insert %s: %w", check.RuleID, err)
		}
	}

	return nil
}

func UpdateRule004Check(ctx context.Context, db *DB, assignment *AgentAssignment, result string, violationRaised bool, detail string, resolved bool) error {
	if assignment == nil {
		return fmt.Errorf("update rule-004 check: nil assignment")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update rule-004 check: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := updateRule004CheckTx(ctx, tx, assignment, result, violationRaised, detail, resolved); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update rule-004 check: commit: %w", err)
	}
	return nil
}

func GetAssignmentRuleCheck(ctx context.Context, db *DB, assignmentID, ruleID string) (*AssignmentRuleCheck, error) {
	row := db.sql.QueryRowContext(ctx, `
		SELECT check_id, assignment_id, session_id, rule_id, result, violation_raised, detail
		FROM assignment_rule_checks
		WHERE assignment_id = ? AND rule_id = ?`,
		assignmentID, ruleID,
	)

	var check AssignmentRuleCheck
	var violationRaised int
	if err := row.Scan(
		&check.CheckID,
		&check.AssignmentID,
		&check.SessionID,
		&check.RuleID,
		&check.Result,
		&violationRaised,
		&check.Detail,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrAgentAssignmentNotFound
		}
		return nil, fmt.Errorf("get assignment rule check: %w", err)
	}
	check.ViolationRaised = violationRaised != 0
	return &check, nil
}

func UpdateRule003Check(ctx context.Context, db *DB, assignment *AgentAssignment, result string, violationRaised bool, detail string) error {
	if assignment == nil {
		return fmt.Errorf("update rule-003 check: nil assignment")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("update rule-003 check: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	actions := []string{}
	if violationRaised {
		actions = []string{"block", "notify"}
	}
	if err := updateRuleCheckTx(ctx, tx, assignment.ID, "RULE-003", result, violationRaised, actions, detail, false); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update rule-003 check: commit: %w", err)
	}
	return nil
}

func WarnAssignmentHoldTTL(ctx context.Context, db *DB, assignment *AgentAssignment, detail string) error {
	if assignment == nil {
		return fmt.Errorf("warn assignment hold ttl: nil assignment")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("warn assignment hold ttl: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := updateRuleCheckTx(ctx, tx, assignment.ID, "RULE-003", "warn", false, []string{"notify"}, detail, false); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{
		"assignment_id": assignment.ID,
		"repo":          assignment.Repo,
		"branch":        assignment.Branch,
		"detail":        detail,
	})
	if err != nil {
		return fmt.Errorf("warn assignment hold ttl: marshal event payload: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		VALUES (?, ?, ?, ?)`,
		assignment.SessionID, assignment.AgentID, "assignment_hold_warning", string(payload),
	); err != nil {
		return fmt.Errorf("warn assignment hold ttl: append event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("warn assignment hold ttl: commit: %w", err)
	}
	return nil
}

func ReleaseAssignmentForHoldTTL(ctx context.Context, db *DB, assignment *AgentAssignment, detail string) error {
	if assignment == nil {
		return fmt.Errorf("release assignment hold ttl: nil assignment")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("release assignment hold ttl: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_assignments
		SET ended_at = datetime('now'), end_reason = 'hold_ttl_exceeded'
		WHERE assignment_id = ? AND ended_at IS NULL`,
		assignment.ID,
	); err != nil {
		return fmt.Errorf("release assignment hold ttl: end assignment: %w", err)
	}
	if err := updateRuleCheckTx(ctx, tx, assignment.ID, "RULE-003", "fail", true, []string{"block", "notify"}, detail, false); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE branch_states
		SET owner_session_id = '',
		    owner_session_last_seen = NULL,
		    owner_agent = '',
		    updated_at = datetime('now')
		WHERE repo = ? AND branch = ?`,
		assignment.Repo, assignment.Branch,
	); err != nil {
		return fmt.Errorf("release assignment hold ttl: clear branch ownership: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"assignment_id": assignment.ID,
		"repo":          assignment.Repo,
		"branch":        assignment.Branch,
		"detail":        detail,
	})
	if err != nil {
		return fmt.Errorf("release assignment hold ttl: marshal event payload: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO agent_events (session_id, agent_id, event_type, payload)
		VALUES (?, ?, ?, ?)`,
		assignment.SessionID, assignment.AgentID, "assignment_hold_released", string(payload),
	); err != nil {
		return fmt.Errorf("release assignment hold ttl: append event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("release assignment hold ttl: commit: %w", err)
	}
	return nil
}

func updateRule004CheckTx(ctx context.Context, tx *sql.Tx, assignment *AgentAssignment, result string, violationRaised bool, detail string, resolved bool) error {
	actions := []string{}
	if violationRaised {
		actions = []string{"block", "log", "notify"}
	}
	return updateRuleCheckTx(ctx, tx, assignment.ID, "RULE-004", result, violationRaised, actions, detail, resolved)
}

func updateRuleCheckTx(ctx context.Context, tx *sql.Tx, assignmentID, ruleID, result string, violationRaised bool, actions []string, detail string, resolved bool) error {
	resolvedBy := ""
	var resolvedAt any = nil
	if resolved {
		resolvedBy = "codero"
		resolvedAt = time.Now().UTC()
	}

	violationActionTaken, err := json.Marshal(actions)
	if err != nil {
		return fmt.Errorf("update %s check: marshal actions: %w", ruleID, err)
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE assignment_rule_checks
		SET checked_at = datetime('now'),
		    result = ?,
		    violation_raised = ?,
		    violation_action_taken = ?,
		    detail = ?,
		    resolved_at = ?,
		    resolved_by = ?
		WHERE assignment_id = ? AND rule_id = ?`,
		result, boolToInt(violationRaised), string(violationActionTaken), detail, resolvedAt, resolvedBy, assignmentID, ruleID,
	)
	if err != nil {
		return fmt.Errorf("update %s check: update: %w", ruleID, err)
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
