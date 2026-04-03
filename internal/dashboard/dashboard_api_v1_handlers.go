package dashboard

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codero/codero/internal/config"
	loglib "github.com/codero/codero/internal/log"
	"github.com/codero/codero/internal/session"
	"github.com/codero/codero/internal/state"
)

// ─── §3 Session endpoints ────────────────────────────────────────────────

// SessionListResponse is the response for GET /api/v1/dashboard/sessions.
type SessionListResponse struct {
	Sessions      []SessionRow `json:"sessions"`
	Total         int          `json:"total"`
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
}

// SessionRow is a single session in the sessions list.
type SessionRow struct {
	SessionID       string     `json:"session_id"`
	AgentID         string     `json:"agent_id"`
	Mode            string     `json:"mode"`
	Status          string     `json:"status"`
	TmuxSessionName string     `json:"tmux_session_name,omitempty"`
	Checkpoint      string     `json:"checkpoint,omitempty"`
	InferredStatus  string     `json:"inferred_status,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	EndReason       string     `json:"end_reason,omitempty"`
}

// SessionDetailResponse is the response for GET /api/v1/dashboard/sessions/{id}.
type SessionDetailResponse struct {
	SessionID       string              `json:"session_id"`
	AgentID         string              `json:"agent_id"`
	Mode            string              `json:"mode"`
	Status          string              `json:"status"`
	TmuxSessionName string              `json:"tmux_session_name,omitempty"`
	Checkpoint      string              `json:"checkpoint,omitempty"`
	StartedAt       time.Time           `json:"started_at"`
	LastSeenAt      time.Time           `json:"last_seen_at"`
	EndedAt         *time.Time          `json:"ended_at,omitempty"`
	EndReason       string              `json:"end_reason,omitempty"`
	Assignments     []AssignmentSummary `json:"assignments"`
	SchemaVersion   string              `json:"schema_version"`
	GeneratedAt     time.Time           `json:"generated_at"`
}

func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	status := r.URL.Query().Get("status")
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	sessions, total, err := querySessions(r.Context(), h.db, status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sessions query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, SessionListResponse{
		Sessions:      sessions,
		Total:         total,
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	sessionID := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/sessions/")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id required", "missing_id")
		return
	}

	session, err := querySessionByID(r.Context(), h.db, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "session not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "session query failed", "db_error")
		return
	}

	assignments, err := queryAssignmentsBySession(r.Context(), h.db, sessionID)
	if err != nil {
		assignments = []AssignmentSummary{}
	}

	writeJSON(w, http.StatusOK, SessionDetailResponse{
		SessionID:       session.SessionID,
		AgentID:         session.AgentID,
		Mode:            session.Mode,
		Status:          session.Status,
		TmuxSessionName: session.TmuxSessionName,
		Checkpoint:      session.Checkpoint,
		StartedAt:       session.StartedAt,
		LastSeenAt:      session.LastSeenAt,
		EndedAt:         session.EndedAt,
		EndReason:       session.EndReason,
		Assignments:     assignments,
		SchemaVersion:   SchemaVersionV1,
		GeneratedAt:     time.Now().UTC(),
	})
}

// ─── §4 Assignment detail ────────────────────────────────────────────────

// AssignmentDetailResponse is the response for GET /api/v1/dashboard/assignments/{id}.
type AssignmentDetailResponse struct {
	AssignmentSummary
	RuleChecks    []AssignmentRuleCheckRow `json:"rule_checks"`
	SchemaVersion string                   `json:"schema_version"`
	GeneratedAt   time.Time                `json:"generated_at"`
}

func (h *Handler) handleAssignmentDetail(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)

	// Strip prefix and split: "/{id}" or "/{id}/{action}"
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/assignments/")
	parts := strings.SplitN(path, "/", 2)
	assignmentID := parts[0]
	if assignmentID == "" {
		writeError(w, http.StatusBadRequest, "assignment_id required", "missing_id")
		return
	}

	// Sub-action route
	if len(parts) == 2 && parts[1] != "" {
		h.handleAssignmentAction(w, r, assignmentID, parts[1])
		return
	}

	// Default: GET detail
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	assignment, err := queryAssignmentByID(r.Context(), h.db, assignmentID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "assignment not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "assignment query failed", "db_error")
		return
	}

	checks, err := queryRuleChecksByAssignment(r.Context(), h.db, assignmentID)
	if err != nil {
		checks = []AssignmentRuleCheckRow{}
	}

	writeJSON(w, http.StatusOK, AssignmentDetailResponse{
		AssignmentSummary: *assignment,
		RuleChecks:        checks,
		SchemaVersion:     SchemaVersionV1,
		GeneratedAt:       time.Now().UTC(),
	})
}

// AssignmentActionResponse is returned for POST /assignments/{id}/{action}.
type AssignmentActionResponse struct {
	AssignmentID  string    `json:"assignment_id"`
	Action        string    `json:"action"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
}

var validAssignmentActions = map[string]bool{
	"pause": true, "resume": true, "abandon": true, "close": true,
	"replay": true, "release": true, "release-slot": true,
}

func (h *Handler) handleAssignmentAction(w http.ResponseWriter, r *http.Request, assignmentID, action string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required", "")
		return
	}
	if !validAssignmentActions[action] {
		writeError(w, http.StatusNotFound, "unknown action: "+action, "not_found")
		return
	}

	// Verify assignment exists
	_, err := queryAssignmentByID(r.Context(), h.db, assignmentID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "assignment not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "assignment query failed", "db_error")
		return
	}

	// For actions not yet supported by state engine, return not_implemented
	if action == "replay" || action == "release" || action == "release-slot" {
		writeJSON(w, http.StatusNotImplemented, AssignmentActionResponse{
			AssignmentID:  assignmentID,
			Action:        action,
			Status:        "not_implemented",
			Message:       action + " action not yet supported in this version",
			SchemaVersion: SchemaVersionV1,
			GeneratedAt:   time.Now().UTC(),
		})
		return
	}

	if err := state.PerformAssignmentAction(r.Context(), state.NewDB(h.db), assignmentID, action); err != nil {
		// Map state errors to appropriate HTTP status codes
		switch {
		case errors.Is(err, state.ErrAgentAssignmentNotFound):
			writeError(w, http.StatusNotFound, "assignment not found", "not_found")
		case errors.Is(err, state.ErrAssignmentEnded):
			writeError(w, http.StatusConflict, "assignment already ended", "already_ended")
		case errors.Is(err, state.ErrInvalidTransition):
			writeError(w, http.StatusConflict, "invalid state transition", "invalid_transition")
		case strings.Contains(err.Error(), "unknown action"):
			writeError(w, http.StatusBadRequest, "unknown action: "+action, "unknown_action")
		default:
			loglib.Error("dashboard: assignment action failed", "assignment_id", assignmentID, "action", action, "error", err)
			writeError(w, http.StatusInternalServerError, "action failed: "+err.Error(), "action_error")
		}
		return
	}

	writeJSON(w, http.StatusOK, AssignmentActionResponse{
		AssignmentID:  assignmentID,
		Action:        action,
		Status:        "accepted",
		Message:       fmt.Sprintf("Assignment %s %s action accepted", assignmentID, action),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

// ─── §5 Feedback endpoints ──────────────────────────────────────────────

// FeedbackItem is one finding returned in feedback responses.
type FeedbackItem struct {
	ID       string    `json:"id"`
	RunID    string    `json:"run_id"`
	Repo     string    `json:"repo"`
	Branch   string    `json:"branch"`
	Severity string    `json:"severity"`
	Category string    `json:"category"`
	File     string    `json:"file"`
	Line     int       `json:"line"`
	Message  string    `json:"message"`
	Source   string    `json:"source"`
	RuleID   string    `json:"rule_id"`
	Ts       time.Time `json:"ts"`
}

// FeedbackResponse is the response for GET /api/v1/dashboard/feedback/{task_id}.
type FeedbackResponse struct {
	TaskID        string         `json:"task_id"`
	Items         []FeedbackItem `json:"items"`
	Total         int            `json:"total"`
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
}

// FeedbackHistoryResponse is the response for GET /api/v1/dashboard/feedback/history.
type FeedbackHistoryResponse struct {
	Items         []FeedbackItem `json:"items"`
	Total         int            `json:"total"`
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
}

// ScorecardResponse is the response for GET /api/v1/dashboard/scorecard.
type ScorecardResponse struct {
	GatePassRate    string    `json:"gatePassRate"`
	AvgCycleTime    string    `json:"avgCycleTime"`
	MergeRate       string    `json:"mergeRate"`
	ComplianceScore string    `json:"complianceScore"`
	Summary         string    `json:"summary"`
	SchemaVersion   string    `json:"schema_version"`
	GeneratedAt     time.Time `json:"generated_at"`
}

func (h *Handler) handleFeedback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	taskID := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/feedback/")
	if taskID == "" || taskID == "history" {
		writeError(w, http.StatusBadRequest, "task_id required", "missing_id")
		return
	}

	items, err := queryFeedbackByTask(r.Context(), h.db, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "feedback query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, FeedbackResponse{
		TaskID:        taskID,
		Items:         items,
		Total:         len(items),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleFeedbackHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	limit := queryInt(r, "limit", 100)
	items, err := queryFeedbackHistory(r.Context(), h.db, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "feedback history query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, FeedbackHistoryResponse{
		Items:         items,
		Total:         len(items),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

// ─── §6 Gate endpoints ──────────────────────────────────────────────────

// GateLiveResponse is the response for GET /api/v1/dashboard/gate/live/{session_id}.
type GateLiveResponse struct {
	SessionID     string    `json:"session_id"`
	Status        string    `json:"status"`
	Provider      string    `json:"provider,omitempty"`
	Progress      string    `json:"progress,omitempty"`
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
}

// GateResultsResponse is the response for GET /api/v1/dashboard/gate/results/{session_id}.
type GateResultsResponse struct {
	SessionID     string    `json:"session_id"`
	Results       []RunRow  `json:"results"`
	Total         int       `json:"total"`
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
}

// GateFindingsResponse is the response for GET /api/v1/dashboard/gate/findings/{task_id}.
type GateFindingsResponse struct {
	TaskID        string         `json:"task_id"`
	Findings      []FeedbackItem `json:"findings"`
	Total         int            `json:"total"`
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
}

func (h *Handler) handleGateRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/gate/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] == "" {
		writeError(w, http.StatusBadRequest, "gate sub-path and ID required", "missing_path")
		return
	}

	switch parts[0] {
	case "live":
		h.handleGateLive(w, r, parts[1])
	case "results":
		h.handleGateResults(w, r, parts[1])
	case "findings":
		h.handleGateFindings(w, r, parts[1])
	default:
		writeError(w, http.StatusNotFound, "unknown gate endpoint", "not_found")
	}
}

func (h *Handler) handleGateLive(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	status, provider, progress := queryGateLive(r.Context(), h.db, sessionID)
	writeJSON(w, http.StatusOK, GateLiveResponse{
		SessionID:     sessionID,
		Status:        status,
		Provider:      provider,
		Progress:      progress,
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleGateResults(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	results, err := queryGateResultsBySession(r.Context(), h.db, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gate results query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, GateResultsResponse{
		SessionID:     sessionID,
		Results:       results,
		Total:         len(results),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleGateFindings(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	findings, err := queryFeedbackByTask(r.Context(), h.db, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gate findings query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, GateFindingsResponse{
		TaskID:        taskID,
		Findings:      findings,
		Total:         len(findings),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

// ─── §7 Merge endpoints ─────────────────────────────────────────────────

// MergeStatusResponse is the response for GET /api/v1/dashboard/merge/status/{assignment_id}.
type MergeStatusResponse struct {
	AssignmentID  string    `json:"assignment_id"`
	MergeEligible bool      `json:"merge_eligible"`
	GateStatus    string    `json:"gate_status"`
	Reason        string    `json:"reason,omitempty"`
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
}

// MergeActionResponse is the response for POST merge approve/reject/force.
type MergeActionResponse struct {
	AssignmentID  string    `json:"assignment_id"`
	Action        string    `json:"action"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
}

func (h *Handler) handleMergeRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/merge/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] == "" {
		writeError(w, http.StatusBadRequest, "merge sub-path and ID required", "missing_path")
		return
	}

	switch parts[0] {
	case "status":
		h.handleMergeStatus(w, r, parts[1])
	case "approve":
		h.handleMergeApprove(w, r, parts[1])
	case "reject":
		h.handleMergeReject(w, r, parts[1])
	case "force":
		h.handleMergeForce(w, r, parts[1])
	default:
		writeError(w, http.StatusNotFound, "unknown merge endpoint", "not_found")
	}
}

func (h *Handler) handleMergeStatus(w http.ResponseWriter, r *http.Request, assignmentID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	eligible, gateStatus, reason := queryMergeEligibility(r.Context(), h.db, assignmentID)
	writeJSON(w, http.StatusOK, MergeStatusResponse{
		AssignmentID:  assignmentID,
		MergeEligible: eligible,
		GateStatus:    gateStatus,
		Reason:        reason,
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleMergeApprove(w http.ResponseWriter, r *http.Request, assignmentID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	writeJSON(w, http.StatusOK, MergeActionResponse{
		AssignmentID:  assignmentID,
		Action:        "approve",
		Status:        "accepted",
		Message:       "merge approved",
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleMergeReject(w http.ResponseWriter, r *http.Request, assignmentID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	writeJSON(w, http.StatusOK, MergeActionResponse{
		AssignmentID:  assignmentID,
		Action:        "reject",
		Status:        "accepted",
		Message:       "merge rejected",
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

// handleMergeForce implements DA-4 (RULE-001): force-merge is blocked when gate has failed.
func (h *Handler) handleMergeForce(w http.ResponseWriter, r *http.Request, assignmentID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	_, gateStatus, _ := queryMergeEligibility(r.Context(), h.db, assignmentID)
	if gateStatus == "failed" {
		writeError(w, http.StatusForbidden,
			"RULE-001: force-merge blocked because gate check failed",
			"da4_gate_failed")
		return
	}

	writeJSON(w, http.StatusOK, MergeActionResponse{
		AssignmentID:  assignmentID,
		Action:        "force",
		Status:        "accepted",
		Message:       "force-merge accepted",
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

// ─── §8 Repo config endpoints ───────────────────────────────────────────

// RepoConfigResponse is the response for GET/PUT /api/v1/dashboard/settings/repo-config/{repo}.
type RepoConfigResponse struct {
	Repo          string            `json:"repo"`
	Config        map[string]string `json:"config"`
	SchemaVersion string            `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
}

func (h *Handler) handleRepoConfig(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/settings/repo-config/")
	if path == "" {
		writeError(w, http.StatusBadRequest, "repo name required", "missing_repo")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getRepoConfig(w, r, path)
	case http.MethodPut:
		h.putRepoConfig(w, r, path)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (h *Handler) getRepoConfig(w http.ResponseWriter, _ *http.Request, repo string) {
	config := queryRepoConfig(h.db, repo)
	writeJSON(w, http.StatusOK, RepoConfigResponse{
		Repo:          repo,
		Config:        config,
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) putRepoConfig(w http.ResponseWriter, r *http.Request, repo string) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body", "read_error")
		return
	}

	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), "parse_error")
		return
	}

	config := queryRepoConfig(h.db, repo)
	for k, v := range payload {
		config[k] = v
	}

	writeJSON(w, http.StatusOK, RepoConfigResponse{
		Repo:          repo,
		Config:        config,
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

// ─── §9 Compliance sub-endpoints ────────────────────────────────────────

// ComplianceRulesResponse is the response for GET /api/v1/dashboard/compliance/rules.
type ComplianceRulesResponse struct {
	Rules         []AgentRuleRow `json:"rules"`
	Total         int            `json:"total"`
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
}

// ComplianceChecksResponse is the response for GET /api/v1/dashboard/compliance/checks/{assignment_id}.
type ComplianceChecksResponse struct {
	AssignmentID  string                   `json:"assignment_id"`
	Checks        []AssignmentRuleCheckRow `json:"checks"`
	Total         int                      `json:"total"`
	SchemaVersion string                   `json:"schema_version"`
	GeneratedAt   time.Time                `json:"generated_at"`
}

// ComplianceViolationsResponse is the response for GET /api/v1/dashboard/compliance/violations.
type ComplianceViolationsResponse struct {
	Violations    []AssignmentRuleCheckRow `json:"violations"`
	Total         int                      `json:"total"`
	SchemaVersion string                   `json:"schema_version"`
	GeneratedAt   time.Time                `json:"generated_at"`
}

func (h *Handler) handleComplianceRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	rules, _, err := queryCompliance(r.Context(), h.db, 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "compliance rules query failed", "db_error")
		return
	}
	if rules == nil {
		rules = []AgentRuleRow{}
	}
	writeJSON(w, http.StatusOK, ComplianceRulesResponse{
		Rules:         rules,
		Total:         len(rules),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleComplianceChecks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	assignmentID := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/compliance/checks/")
	if assignmentID == "" {
		writeError(w, http.StatusBadRequest, "assignment_id required", "missing_id")
		return
	}

	checks, err := queryRuleChecksByAssignment(r.Context(), h.db, assignmentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "compliance checks query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, ComplianceChecksResponse{
		AssignmentID:  assignmentID,
		Checks:        checks,
		Total:         len(checks),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleComplianceViolations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	violations, err := queryComplianceViolations(r.Context(), h.db, 500)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "compliance violations query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, ComplianceViolationsResponse{
		Violations:    violations,
		Total:         len(violations),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

// ─── §10 Queue endpoints ────────────────────────────────────────────────

// QueueItem is a single item in the merge queue.
type QueueItem struct {
	ID             string    `json:"id"`
	Repo           string    `json:"repo"`
	Branch         string    `json:"branch"`
	State          string    `json:"state"`
	Priority       int       `json:"priority"`
	OwnerSessionID string    `json:"owner_session_id"`
	SubmissionTime time.Time `json:"submission_time"`
}

// QueueResponse is the response for GET /api/v1/dashboard/queue.
type QueueResponse struct {
	Items         []QueueItem `json:"items"`
	Total         int         `json:"total"`
	SchemaVersion string      `json:"schema_version"`
	GeneratedAt   time.Time   `json:"generated_at"`
}

// QueueStatsResponse is the response for GET /api/v1/dashboard/queue/stats.
type QueueStatsResponse struct {
	Pending       int       `json:"pending"`
	Active        int       `json:"active"`
	Blocked       int       `json:"blocked"`
	Total         int       `json:"total"`
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
}

func (h *Handler) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	items, err := queryQueue(r.Context(), h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "queue query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, QueueResponse{
		Items:         items,
		Total:         len(items),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

func (h *Handler) handleQueueStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	pending, active, blocked, total, err := queryQueueStats(r.Context(), h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "queue stats query failed", "db_error")
		return
	}
	writeJSON(w, http.StatusOK, QueueStatsResponse{
		Pending:       pending,
		Active:        active,
		Blocked:       blocked,
		Total:         total,
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────

func queryInt(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return defaultVal
	}
	return n
}

// queryRepoConfig returns a default repo configuration map.
// In the current implementation, repo config is in-memory with defaults.
func queryRepoConfig(_ *sql.DB, _ string) map[string]string {
	return map[string]string{
		"pr_auto_create":    "true",
		"coderabbit_opt_in": "false",
		"branch_cleanup":    "true",
	}
}

// handleArchives serves GET /api/v1/dashboard/archives (§2.8).
func (h *Handler) handleArchives(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	archives, err := querySessionArchives(r.Context(), h.db, queryIntParam(r, "limit", 50))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query archives: "+err.Error(), "archives_query_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"archives":       archives,
		"total":          len(archives),
		"schema_version": SchemaVersionV1,
		"generated_at":   time.Now().UTC(),
	})
}

// handleTrackingConfig serves GET/PUT /api/v1/dashboard/tracking-config.
// GET returns the list of agents with tracking disabled.
// PUT accepts {"agent_id": "...", "disabled": true/false} to toggle.
func (h *Handler) handleTrackingConfig(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	switch r.Method {
	case http.MethodGet:
		// Intentionally force a fresh scan here so deleted local profiles disappear
		// from the operator UI immediately after they are removed from disk.
		uc, agents, err := config.LoadUserConfigWithFreshRegistry()
		if err != nil {
			loglib.Warn("dashboard: tracking-config: fresh registry load failed",
				loglib.FieldComponent, "dashboard", "error", err)
			uc, err = config.LoadUserConfig()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "load config: "+err.Error(), "config_error")
				return
			}
			agents = uc.RegisteredAgents()
		}
		disabled := uc.DisabledAgents
		if disabled == nil {
			disabled = []string{}
		}
		if agents == nil {
			agents = []config.RegisteredAgent{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"disabled_agents": disabled,
			"agents":          agents,
			"generated_at":    time.Now().UTC(),
		})
	case http.MethodPut:
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			writeError(w, http.StatusBadRequest, "read body: "+err.Error(), "bad_request")
			return
		}
		var req struct {
			AgentID  string            `json:"agent_id"`
			Disabled *bool             `json:"disabled,omitempty"`
			EnvVars  map[string]string `json:"env_vars,omitempty"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "parse body: "+err.Error(), "bad_request")
			return
		}
		if req.AgentID == "" {
			writeError(w, http.StatusBadRequest, "agent_id is required", "bad_request")
			return
		}
		// Validate env_vars: reject empty keys, keys containing '=' or NUL, values with NUL
		for k, v := range req.EnvVars {
			if k == "" {
				writeError(w, http.StatusBadRequest, "env_vars key cannot be empty", "bad_request")
				return
			}
			if strings.Contains(k, "=") {
				writeError(w, http.StatusBadRequest, "env_vars key cannot contain '='", "bad_request")
				return
			}
			if strings.ContainsRune(k, '\x00') || strings.ContainsRune(v, '\x00') {
				writeError(w, http.StatusBadRequest, "env_vars cannot contain NUL bytes", "bad_request")
				return
			}
		}
		// Serialize load→modify→save to prevent lost updates.
		config.ConfigMu.Lock()
		uc, err := config.LoadUserConfig()
		if err != nil {
			config.ConfigMu.Unlock()
			writeError(w, http.StatusInternalServerError, "load config: "+err.Error(), "config_error")
			return
		}
		if req.Disabled != nil {
			uc.SetTrackingDisabled(req.AgentID, *req.Disabled)
		}
		if req.EnvVars != nil {
			if uc.Wrappers == nil {
				uc.Wrappers = make(map[string]config.WrapperConfig)
			}
			wConf := uc.Wrappers[req.AgentID]
			wConf.EnvVars = req.EnvVars
			uc.Wrappers[req.AgentID] = wConf
		}
		if err := uc.Save(); err != nil {
			config.ConfigMu.Unlock()
			writeError(w, http.StatusInternalServerError, "save config: "+err.Error(), "config_error")
			return
		}
		config.ConfigMu.Unlock()
		agents := uc.DisabledAgents
		if agents == nil {
			agents = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"disabled_agents": agents,
			"generated_at":    time.Now().UTC(),
		})
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// handleAgents serves GET /api/v1/dashboard/agents.
// Returns per-agent stats aggregated over the last 30 days, merged with
// tracking config (installed/disabled status from discovered shims).
func (h *Handler) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	roster, err := queryAgentRoster(r.Context(), h.db)
	if err != nil {
		loglib.Error("dashboard: agents query failed",
			loglib.FieldComponent, "dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "agents query failed", "db_error")
		return
	}

	// Intentionally force a fresh scan here so deleted local profiles disappear
	// from the operator UI immediately after they are removed from disk.
	uc, registryAgents, ucErr := config.LoadUserConfigWithFreshRegistry()
	if ucErr != nil {
		loglib.Warn("dashboard: agents: failed to load user config",
			loglib.FieldComponent, "dashboard", "error", ucErr)
		uc, ucErr = config.LoadUserConfig()
		if ucErr != nil {
			loglib.Warn("dashboard: agents: failed to load cached user config",
				loglib.FieldComponent, "dashboard", "error", ucErr)
		} else {
			registryAgents = uc.RegisteredAgents()
		}
	}
	registryByID := map[string]config.RegisteredAgent{}
	for _, agent := range registryAgents {
		registryByID[agent.AgentID] = agent
	}
	disabledByID := map[string]bool{}
	if uc != nil {
		for _, agentID := range uc.DisabledAgents {
			disabledByID[agentID] = true
		}
	}

	// Override status for disabled agents; add shims not yet in DB roster.
	seenIDs := make(map[string]bool, len(roster))
	for i := range roster {
		seenIDs[roster[i].AgentID] = true
		if info, ok := registryByID[roster[i].AgentID]; ok {
			roster[i].Status = agentStatus(roster[i], info.Disabled)
		} else if disabledByID[roster[i].AgentID] {
			roster[i].Status = agentStatus(roster[i], true)
		}
	}
	// Append shim-only agents (discovered but no 30-day history).
	for _, info := range registryByID {
		if !seenIDs[info.AgentID] {
			status := "idle"
			if info.Disabled {
				status = "disabled"
			}
			roster = append(roster, AgentRosterRow{
				AgentID: info.AgentID,
				Status:  status,
			})
		}
	}
	if roster == nil {
		roster = []AgentRosterRow{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"agents":       roster,
		"generated_at": time.Now().UTC(),
	})
}

// handleAgentSessions serves GET /api/v1/dashboard/agents/{agentId}/sessions.
// Returns the last 5 sessions for the named agent with repo/branch context.
func (h *Handler) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	const prefix = "/api/v1/dashboard/agents/"
	const suffix = "/sessions"
	path := r.URL.Path
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		writeError(w, http.StatusNotFound, "not found", "")
		return
	}
	agentID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if agentID == "" || strings.Contains(agentID, "/") {
		writeError(w, http.StatusBadRequest, "invalid agent ID", "")
		return
	}

	sessions, err := queryAgentRecentSessions(r.Context(), h.db, agentID)
	if err != nil {
		loglib.Error("dashboard: agent sessions query failed",
			loglib.FieldComponent, "dashboard", "agent_id", agentID, "error", err)
		writeError(w, http.StatusInternalServerError, "query failed", "db_error")
		return
	}
	if sessions == nil {
		sessions = []AgentRecentSessionRow{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

func (h *Handler) handleSessionTailRouter(w http.ResponseWriter, r *http.Request) {
	// URL format: /api/v1/dashboard/sessions/{id} or /api/v1/dashboard/sessions/{id}/tail
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/sessions/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		h.handleSessionDetail(w, r)
		return
	}
	sessionID := parts[0]

	// No action or empty action -> session detail
	if len(parts) == 1 || parts[1] == "" {
		// Restore the original URL path for handleSessionDetail
		r.URL.Path = "/api/v1/dashboard/sessions/" + sessionID
		h.handleSessionDetail(w, r)
		return
	}

	action := parts[1]
	if action == "tail" {
		h.handleSessionTail(w, r, sessionID)
		return
	}

	// Unknown action: return 404 instead of falling through with malformed sessionID
	http.NotFound(w, r)
}

func (h *Handler) handleSessionTail(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	lineCount := queryInt(r, "lines", 50)
	if lineCount > 500 {
		lineCount = 500
	}

	path, err := session.TailPath(sessionID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session ID: "+err.Error(), "invalid_input")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{
				"session_id":   sessionID,
				"lines":        []string{},
				"error":        "tail file not found",
				"generated_at": time.Now().UTC(),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "open tail: "+err.Error(), "fs_error")
		return
	}
	defer f.Close()

	// Simple tail: read last 32KB
	const bufSize = 32 * 1024
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stat tail: "+err.Error(), "fs_error")
		return
	}

	offset := info.Size() - bufSize
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, 0); err != nil {
		writeError(w, http.StatusInternalServerError, "seek tail: "+err.Error(), "fs_error")
		return
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	// Only skip the first scanned line if we actually landed mid-line
	// (i.e., offset > 0 and the byte before offset is not a newline)
	skipFirst := false
	if offset > 0 {
		// Read one byte before offset to check if we're at a line boundary
		if _, err := f.Seek(offset-1, 0); err == nil {
			var prevByte [1]byte
			if _, err := f.Read(prevByte[:]); err == nil && prevByte[0] != '\n' {
				skipFirst = true
			}
		}
		// Seek back to offset
		if _, err := f.Seek(offset, 0); err != nil {
			writeError(w, http.StatusInternalServerError, "seek tail: "+err.Error(), "fs_error")
			return
		}
	}
	for scanner.Scan() {
		if skipFirst {
			skipFirst = false
			continue
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "scan tail: "+err.Error(), "fs_error")
		return
	}

	if len(lines) > lineCount {
		lines = lines[len(lines)-lineCount:]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":   sessionID,
		"lines":        lines,
		"generated_at": time.Now().UTC(),
	})
}

// agentStatus derives the display status for an agent roster row.
// disabled overrides everything; otherwise: active > offline > idle.
func agentStatus(r AgentRosterRow, disabled bool) string {
	if disabled {
		return "disabled"
	}
	return r.Status // already set by queryAgentRoster (active/offline/idle)
}

// handleSessionMetrics serves GET /api/v1/dashboard/sessions/metrics/{session_id}.
// Returns token usage and context pressure summary for a session.
func (h *Handler) handleSessionMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	// URL: /api/v1/dashboard/sessions/metrics/{session_id}
	prefix := "/api/v1/dashboard/sessions/metrics/"
	sessionID := strings.TrimPrefix(r.URL.Path, prefix)
	sessionID = strings.TrimSuffix(sessionID, "/")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id required", "missing_id")
		return
	}

	rows, err := h.db.QueryContext(r.Context(), `
		SELECT model, prompt_tokens, completion_tokens,
		       cumulative_prompt_tokens, cumulative_completion_tokens, request_time
		FROM session_token_metrics
		WHERE session_id = ?
		ORDER BY request_time ASC`, sessionID)
	if err != nil {
		loglib.Error("dashboard: session metrics query failed",
			loglib.FieldComponent, "dashboard",
			"session_id", sessionID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "metrics query failed", "db_error")
		return
	}
	defer rows.Close()

	type requestEntry struct {
		Model                      string    `json:"model"`
		PromptTokens               int64     `json:"prompt_tokens"`
		CompletionTokens           int64     `json:"completion_tokens"`
		CumulativePromptTokens     int64     `json:"cumulative_prompt_tokens"`
		CumulativeCompletionTokens int64     `json:"cumulative_completion_tokens"`
		RequestTime                time.Time `json:"request_time"`
	}

	var requests []requestEntry
	var totalPrompt, totalCompletion int64
	seenModels := map[string]bool{}
	var models []string

	for rows.Next() {
		var e requestEntry
		var reqTimeStr string
		if err := rows.Scan(&e.Model, &e.PromptTokens, &e.CompletionTokens,
			&e.CumulativePromptTokens, &e.CumulativeCompletionTokens, &reqTimeStr); err != nil {
			writeError(w, http.StatusInternalServerError, "metrics scan failed", "db_error")
			return
		}
		if t, err2 := time.Parse(time.RFC3339, reqTimeStr); err2 == nil {
			e.RequestTime = t
		}
		requests = append(requests, e)
		totalPrompt += e.PromptTokens
		totalCompletion += e.CompletionTokens
		if !seenModels[e.Model] {
			seenModels[e.Model] = true
			models = append(models, e.Model)
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "metrics rows error", "db_error")
		return
	}

	// Fetch pressure/compact fields from the session row.
	pressure := "normal"
	var compactCount int
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(context_pressure, 'normal'), COALESCE(compact_count, 0)
		 FROM agent_sessions WHERE session_id = ?`, sessionID).
		Scan(&pressure, &compactCount); err != nil && err != sql.ErrNoRows {
		loglib.Warn("dashboard: session pressure query failed",
			loglib.FieldComponent, "dashboard",
			"session_id", sessionID,
			"error", err,
		)
	}

	// Fetch output activity samples for sparkline (last 30 minutes).
	activitySamples, err := state.GetActivitySamples(r.Context(), state.NewDB(h.db), sessionID, 30)
	if err != nil {
		loglib.Warn("dashboard: activity samples query failed",
			loglib.FieldComponent, "dashboard",
			"session_id", sessionID,
			"error", err,
		)
	}

	// Compute output deltas from cumulative samples for the sparkline.
	var activityDeltas []map[string]interface{}
	for i, s := range activitySamples {
		delta := s.OutputBytes
		if i > 0 {
			delta = s.OutputBytes - activitySamples[i-1].OutputBytes
		}
		if delta < 0 {
			delta = 0
		}
		activityDeltas = append(activityDeltas, map[string]interface{}{
			"bucket":      s.Bucket,
			"delta_bytes": delta,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id":       sessionID,
		"total_requests":   len(requests),
		"total_prompt":     totalPrompt,
		"total_completion": totalCompletion,
		"models":           models,
		"context_pressure": pressure,
		"compact_count":    compactCount,
		"requests":         requests,
		"activity":         activityDeltas,
		"generated_at":     time.Now().UTC(),
	})
}

func (h *Handler) handleScorecard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	// Fetch Phase 1F proving period metrics
	card, err := state.ComputeProvingScorecard(r.Context(), state.NewDB(h.db))
	if err != nil {
		loglib.Error("dashboard: scorecard compute failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to compute scorecard", "db_error")
		return
	}

	// Map proving metrics to dashboard scorecard fields
	gatePassRate := "—"
	if card.PrecommitReviews7Days > 0 {
		gatePassRate = fmt.Sprintf("%d runs", card.PrecommitReviews7Days)
	}

	avgCycleTime := "—"
	mergeRate := "—"
	if card.BranchesReviewed7Days > 0 {
		mergeRate = fmt.Sprintf("%d/wk", card.BranchesReviewed7Days)
	}

	complianceScore := "100%"
	if card.ManualDBRepairs > 0 || card.MissedFeedbackDeliveries > 0 {
		complianceScore = "needs attention"
	}

	summary := fmt.Sprintf("Phase 1F Proving in progress. %d branches reviewed in last 7 days. %d pre-commit runs. %d stale detections recorded.",
		card.BranchesReviewed7Days, card.PrecommitReviews7Days, card.StaleDetections30Days)

	writeJSON(w, http.StatusOK, ScorecardResponse{
		GatePassRate:    gatePassRate,
		AvgCycleTime:    avgCycleTime,
		MergeRate:       mergeRate,
		ComplianceScore: complianceScore,
		Summary:         summary,
		SchemaVersion:   SchemaVersionV1,
		GeneratedAt:     time.Now().UTC(),
	})
}

// handleTasks serves GET /api/v1/dashboard/tasks.
// Returns the current task list (stub: empty until task tracking is wired).
func (h *Handler) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	tasks := []TaskRow{}
	writeJSON(w, http.StatusOK, TasksResponse{
		Tasks:         tasks,
		Total:         len(tasks),
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}
