package dashboard

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codero/codero/internal/config"
)

// trackingConfigMu serializes read-modify-write on ~/.codero/config.yaml
// to prevent lost updates from concurrent PUT requests.
var trackingConfigMu sync.Mutex

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
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	assignmentID := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/assignments/")
	if assignmentID == "" {
		writeError(w, http.StatusBadRequest, "assignment_id required", "missing_id")
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
		uc, err := config.LoadUserConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "load config: "+err.Error(), "config_error")
			return
		}
		agents, err := config.DiscoverAgents(uc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "discover agents: "+err.Error(), "config_error")
			return
		}
		disabled := uc.DisabledAgents
		if disabled == nil {
			disabled = []string{}
		}
		if agents == nil {
			agents = []config.AgentInfo{}
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
			AgentID  string `json:"agent_id"`
			Disabled bool   `json:"disabled"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "parse body: "+err.Error(), "bad_request")
			return
		}
		if req.AgentID == "" {
			writeError(w, http.StatusBadRequest, "agent_id is required", "bad_request")
			return
		}
		// Serialize load→modify→save to prevent lost updates.
		trackingConfigMu.Lock()
		uc, err := config.LoadUserConfig()
		if err != nil {
			trackingConfigMu.Unlock()
			writeError(w, http.StatusInternalServerError, "load config: "+err.Error(), "config_error")
			return
		}
		uc.SetTrackingDisabled(req.AgentID, req.Disabled)
		if err := uc.Save(); err != nil {
			trackingConfigMu.Unlock()
			writeError(w, http.StatusInternalServerError, "save config: "+err.Error(), "config_error")
			return
		}
		trackingConfigMu.Unlock()
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
